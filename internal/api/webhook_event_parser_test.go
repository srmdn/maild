package api

import "testing"

func TestParseWebhookEventsCanonicalObject(t *testing.T) {
	body := []byte(`{"workspace_id":2,"type":"bounce","email":"USER@example.com","reason":"hard_bounce"}`)
	events, rejected, err := parseWebhookEvents(body)
	if err != nil {
		t.Fatalf("parseWebhookEvents error = %v", err)
	}
	if rejected != 0 {
		t.Fatalf("rejected = %d, want 0", rejected)
	}
	if len(events) != 1 {
		t.Fatalf("len(events) = %d, want 1", len(events))
	}
	if events[0].WorkspaceID != 2 {
		t.Fatalf("workspace_id = %d, want 2", events[0].WorkspaceID)
	}
	if events[0].Type != "bounce" {
		t.Fatalf("type = %q, want bounce", events[0].Type)
	}
	if events[0].Email != "user@example.com" {
		t.Fatalf("email = %q, want user@example.com", events[0].Email)
	}
}

func TestParseWebhookEventsSendgridStyleBatch(t *testing.T) {
	body := []byte(`[
	  {"event":"processed","email":"ignored@example.com"},
	  {"event":"spamreport","email":"abuse@example.com"},
	  {"event":"group_unsubscribe","email":"list@example.com"}
	]`)
	events, rejected, err := parseWebhookEvents(body)
	if err != nil {
		t.Fatalf("parseWebhookEvents error = %v", err)
	}
	if len(events) != 2 {
		t.Fatalf("len(events) = %d, want 2", len(events))
	}
	if rejected != 1 {
		t.Fatalf("rejected = %d, want 1", rejected)
	}
	if events[0].Type != "complaint" || events[0].Email != "abuse@example.com" {
		t.Fatalf("first event = %#v", events[0])
	}
	if events[1].Type != "unsubscribe" || events[1].Email != "list@example.com" {
		t.Fatalf("second event = %#v", events[1])
	}
}

func TestParseWebhookEventsSESBounceShape(t *testing.T) {
	body := []byte(`{
	  "eventType":"Bounce",
	  "mail":{"destination":["first@example.com","second@example.com"]},
	  "bounce":{"bounceType":"Permanent","bounceSubType":"General","bouncedRecipients":[{"emailAddress":"hard@example.com"}]}
	}`)
	events, rejected, err := parseWebhookEvents(body)
	if err != nil {
		t.Fatalf("parseWebhookEvents error = %v", err)
	}
	if rejected != 0 {
		t.Fatalf("rejected = %d, want 0", rejected)
	}
	if len(events) != 1 {
		t.Fatalf("len(events) = %d, want 1", len(events))
	}
	if events[0].Type != "bounce" {
		t.Fatalf("type = %q, want bounce", events[0].Type)
	}
	if events[0].Email != "first@example.com" {
		t.Fatalf("email = %q, want first@example.com", events[0].Email)
	}
	if events[0].Reason != "Permanent/General" {
		t.Fatalf("reason = %q, want Permanent/General", events[0].Reason)
	}
}

func TestParseWebhookEventsRejectInvalidPayload(t *testing.T) {
	body := []byte(`[{"event":"processed","email":"nope@example.com"}]`)
	_, _, err := parseWebhookEvents(body)
	if err == nil {
		t.Fatalf("expected error for payload with no actionable events")
	}
}
