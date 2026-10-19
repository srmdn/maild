package api

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

var errInvalidWebhookPayload = errors.New("invalid webhook payload")

func parseWebhookEvents(body []byte) ([]webhookEventRequest, int, error) {
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.UseNumber()

	var payload any
	if err := decoder.Decode(&payload); err != nil {
		return nil, 0, fmt.Errorf("%w: malformed JSON", errInvalidWebhookPayload)
	}

	events := make([]webhookEventRequest, 0)
	rejected := 0

	switch v := payload.(type) {
	case map[string]any:
		parsed, ok := parseWebhookEventObject(v)
		if !ok {
			return nil, 0, fmt.Errorf("%w: object missing required fields", errInvalidWebhookPayload)
		}
		events = append(events, parsed...)
	case []any:
		for _, raw := range v {
			obj, ok := raw.(map[string]any)
			if !ok {
				rejected++
				continue
			}
			parsed, ok := parseWebhookEventObject(obj)
			if !ok {
				rejected++
				continue
			}
			events = append(events, parsed...)
		}
	default:
		return nil, 0, fmt.Errorf("%w: expected object or array", errInvalidWebhookPayload)
	}

	if len(events) == 0 {
		return nil, rejected, fmt.Errorf("%w: no valid events", errInvalidWebhookPayload)
	}

	return events, rejected, nil
}

func parseWebhookEventObject(obj map[string]any) ([]webhookEventRequest, bool) {
	workspaceID := int64FromAny(firstAny(obj, "workspace_id", "workspaceId", "workspace"))

	rawType := stringFromAny(firstAny(obj, "type", "event", "event_type", "eventType", "RecordType"))
	eventType := normalizeWebhookEventType(rawType)
	if eventType == "" {
		if nested, ok := mapFromAny(obj["event-data"]); ok {
			rawType = stringFromAny(firstAny(nested, "event", "event_type", "eventType"))
			eventType = normalizeWebhookEventType(rawType)
		}
	}
	eventType = classifyProviderBounce(obj, rawType, eventType)

	reason := strings.TrimSpace(stringFromAny(
		firstAny(obj, "reason", "description", "response", "diagnostic_code", "details", "message"),
	))
	if reason == "" {
		reason = reasonFromNestedObjects(obj)
	}

	if recipients, ok := sesBouncedRecipients(obj); ok {
		events := make([]webhookEventRequest, 0, len(recipients))
		for _, recipient := range recipients {
			recipientObj, ok := mapFromAny(recipient)
			if !ok {
				continue
			}
			email := strings.TrimSpace(stringFromAny(firstAny(recipientObj, "emailAddress", "email")))
			if email == "" {
				continue
			}
			recipientReason := reason
			if diagnostic := strings.TrimSpace(stringFromAny(firstAny(recipientObj, "diagnosticCode", "status", "action"))); diagnostic != "" && !strings.Contains(recipientReason, diagnostic) {
				if recipientReason != "" {
					recipientReason += ": "
				}
				recipientReason += diagnostic
			}
			events = append(events, webhookEventRequest{WorkspaceID: workspaceID, Type: eventType, Email: strings.ToLower(email), Reason: recipientReason})
		}
		return events, len(events) > 0
	}

	email := strings.TrimSpace(stringFromAny(
		firstAny(obj, "email", "recipient", "to", "rcpt_to", "Email"),
	))
	if email == "" {
		email = emailFromNestedObjects(obj)
	}
	if eventType == "" || email == "" {
		return nil, false
	}

	return []webhookEventRequest{{
		WorkspaceID: workspaceID,
		Type:        eventType,
		Email:       strings.ToLower(email),
		Reason:      reason,
	}}, true
}

func classifyProviderBounce(obj map[string]any, rawType, eventType string) string {
	if eventType == "complaint" || eventType == "unsubscribe" {
		return eventType
	}
	if eventType == "bounce_permanent" || eventType == "bounce_temporary" {
		return eventType
	}
	if eventType != "bounce_unclassified" {
		return eventType
	}

	if bounce, ok := mapFromAny(obj["bounce"]); ok {
		switch strings.ToLower(strings.TrimSpace(stringFromAny(firstAny(bounce, "bounceType", "type")))) {
		case "permanent", "hard":
			return "bounce_permanent"
		case "transient", "temporary", "soft":
			return "bounce_temporary"
		}
	}

	if data, ok := mapFromAny(obj["event-data"]); ok {
		switch strings.ToLower(strings.TrimSpace(stringFromAny(firstAny(data, "severity")))) {
		case "permanent":
			return "bounce_permanent"
		case "temporary":
			return "bounce_temporary"
		}
	}

	if class := classifyEnhancedSMTPStatus(stringFromAny(firstAny(obj, "status", "Status", "smtp_status"))); class != "" {
		return class
	}

	postmarkType := strings.ToLower(strings.TrimSpace(stringFromAny(firstAny(obj, "Type", "BounceType"))))
	code := int64FromAny(firstAny(obj, "TypeCode", "type_code"))
	if strings.EqualFold(strings.TrimSpace(rawType), "bounce") || strings.EqualFold(strings.TrimSpace(rawType), "failed") {
		switch {
		case strings.Contains(postmarkType, "hard") || code == 1:
			return "bounce_permanent"
		case strings.Contains(postmarkType, "soft") || strings.Contains(postmarkType, "transient") || code == 2 || code == 4096:
			return "bounce_temporary"
		}
	}
	return eventType
}

func classifyEnhancedSMTPStatus(status string) string {
	status = strings.TrimSpace(status)
	if strings.HasPrefix(status, "4.") || strings.HasPrefix(status, "4 ") {
		return "bounce_temporary"
	}
	if strings.HasPrefix(status, "5.") || strings.HasPrefix(status, "5 ") {
		return "bounce_permanent"
	}
	return ""
}

func sesBouncedRecipients(obj map[string]any) ([]any, bool) {
	bounce, ok := mapFromAny(obj["bounce"])
	if !ok {
		return nil, false
	}
	recipients, ok := sliceFromAny(bounce["bouncedRecipients"])
	return recipients, ok && len(recipients) > 0
}

/*
The canonical event names intentionally retain classification in webhook_events,
so replay cannot turn a temporary or unclassified bounce into a permanent one.
*/
func normalizeWebhookEventType(raw string) string {
	event := strings.ToLower(strings.TrimSpace(raw))
	switch event {
	case "hard_bounce", "hardbounce":
		return "bounce_permanent"
	case "soft_bounce", "softbounce", "temporary_bounce", "transient_bounce":
		return "bounce_temporary"
	case "bounce", "bounced", "failed", "dropped", "blocked", "reject", "rejected":
		return "bounce_unclassified"
	case "complaint", "complained", "spam", "spamreport", "spam_report", "abuse":
		return "complaint"
	case "unsubscribe", "unsubscribed", "group_unsubscribe", "list_unsubscribe", "user_unsubscribed":
		return "unsubscribe"
	default:
		return ""
	}
}

func emailFromNestedObjects(obj map[string]any) string {
	if recipients, ok := sesBouncedRecipients(obj); ok && len(recipients) > 0 {
		if recObj, ok := mapFromAny(recipients[0]); ok {
			return strings.TrimSpace(stringFromAny(firstAny(recObj, "emailAddress", "email")))
		}
	}
	if nested, ok := mapFromAny(obj["mail"]); ok {
		if destinations, ok := sliceFromAny(nested["destination"]); ok {
			if first := firstString(destinations); first != "" {
				return strings.TrimSpace(first)
			}
		}
	}

	if eventData, ok := mapFromAny(obj["event-data"]); ok {
		return strings.TrimSpace(stringFromAny(firstAny(eventData, "recipient", "email", "rcpt_to")))
	}

	return ""
}

func reasonFromNestedObjects(obj map[string]any) string {
	if bounce, ok := mapFromAny(obj["bounce"]); ok {
		kind := strings.TrimSpace(stringFromAny(firstAny(bounce, "bounceType", "type")))
		subtype := strings.TrimSpace(stringFromAny(firstAny(bounce, "bounceSubType", "subType", "subtype")))
		if kind != "" && subtype != "" {
			return kind + "/" + subtype
		}
		if kind != "" {
			return kind
		}
	}

	if eventData, ok := mapFromAny(obj["event-data"]); ok {
		reason := strings.TrimSpace(stringFromAny(firstAny(eventData, "reason", "delivery-status")))
		if reason != "" {
			return reason
		}
	}

	return ""
}

func firstAny(obj map[string]any, keys ...string) any {
	for _, key := range keys {
		if value, ok := obj[key]; ok {
			return value
		}
	}
	return nil
}

func int64FromAny(v any) int64 {
	switch value := v.(type) {
	case json.Number:
		i, err := value.Int64()
		if err == nil {
			return i
		}
	case float64:
		return int64(value)
	case int64:
		return value
	case int:
		return int64(value)
	case string:
		var n json.Number = json.Number(strings.TrimSpace(value))
		i, err := n.Int64()
		if err == nil {
			return i
		}
	}
	return 0
}

func stringFromAny(v any) string {
	switch value := v.(type) {
	case string:
		return value
	case json.Number:
		return value.String()
	default:
		return ""
	}
}

func mapFromAny(v any) (map[string]any, bool) {
	m, ok := v.(map[string]any)
	return m, ok
}

func sliceFromAny(v any) ([]any, bool) {
	s, ok := v.([]any)
	return s, ok
}

func firstString(values []any) string {
	for _, v := range values {
		if s, ok := v.(string); ok {
			trimmed := strings.TrimSpace(s)
			if trimmed != "" {
				return trimmed
			}
		}
	}
	return ""
}
