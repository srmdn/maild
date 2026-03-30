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
		event, ok := parseWebhookEventObject(v)
		if !ok {
			return nil, 0, fmt.Errorf("%w: object missing required fields", errInvalidWebhookPayload)
		}
		events = append(events, event)
	case []any:
		for _, raw := range v {
			obj, ok := raw.(map[string]any)
			if !ok {
				rejected++
				continue
			}
			event, ok := parseWebhookEventObject(obj)
			if !ok {
				rejected++
				continue
			}
			events = append(events, event)
		}
	default:
		return nil, 0, fmt.Errorf("%w: expected object or array", errInvalidWebhookPayload)
	}

	if len(events) == 0 {
		return nil, rejected, fmt.Errorf("%w: no valid events", errInvalidWebhookPayload)
	}

	return events, rejected, nil
}

func parseWebhookEventObject(obj map[string]any) (webhookEventRequest, bool) {
	workspaceID := int64FromAny(firstAny(obj, "workspace_id", "workspaceId", "workspace"))

	eventType := normalizeWebhookEventType(stringFromAny(
		firstAny(obj, "type", "event", "event_type", "eventType", "RecordType"),
	))
	if eventType == "" {
		if nested, ok := mapFromAny(obj["event-data"]); ok {
			eventType = normalizeWebhookEventType(stringFromAny(firstAny(nested, "event", "event_type", "eventType")))
		}
	}

	email := strings.TrimSpace(stringFromAny(
		firstAny(obj, "email", "recipient", "to", "rcpt_to", "Email"),
	))
	if email == "" {
		email = emailFromNestedObjects(obj)
	}

	reason := strings.TrimSpace(stringFromAny(
		firstAny(obj, "reason", "description", "response", "diagnostic_code", "details", "message"),
	))
	if reason == "" {
		reason = reasonFromNestedObjects(obj)
	}

	if eventType == "" || email == "" {
		return webhookEventRequest{}, false
	}

	return webhookEventRequest{
		WorkspaceID: workspaceID,
		Type:        eventType,
		Email:       strings.ToLower(email),
		Reason:      reason,
	}, true
}

func normalizeWebhookEventType(raw string) string {
	event := strings.ToLower(strings.TrimSpace(raw))
	switch event {
	case "bounce", "bounced", "hard_bounce", "soft_bounce", "failed", "dropped", "blocked", "reject", "rejected":
		return "bounce"
	case "complaint", "complained", "spam", "spamreport", "spam_report", "abuse":
		return "complaint"
	case "unsubscribe", "unsubscribed", "group_unsubscribe", "list_unsubscribe", "user_unsubscribed":
		return "unsubscribe"
	default:
		return ""
	}
}

func emailFromNestedObjects(obj map[string]any) string {
	if nested, ok := mapFromAny(obj["mail"]); ok {
		if destinations, ok := sliceFromAny(nested["destination"]); ok {
			if first := firstString(destinations); first != "" {
				return strings.TrimSpace(first)
			}
		}
	}

	if bounce, ok := mapFromAny(obj["bounce"]); ok {
		if recipients, ok := sliceFromAny(bounce["bouncedRecipients"]); ok {
			for _, rec := range recipients {
				if recObj, ok := mapFromAny(rec); ok {
					email := strings.TrimSpace(stringFromAny(firstAny(recObj, "emailAddress", "email")))
					if email != "" {
						return email
					}
				}
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
