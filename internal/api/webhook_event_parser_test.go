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
	if events[0].Type != "bounce_unclassified" {
		t.Fatalf("type = %q, want bounce_unclassified", events[0].Type)
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
	  "bounce":{"bounceType":"Permanent","bounceSubType":"General","bouncedRecipients":[{"emailAddress":"hard@example.com"},{"emailAddress":"another@example.com","diagnosticCode":"smtp; 550 5.1.1 no such user"}]}
	}`)
	events, rejected, err := parseWebhookEvents(body)
	if err != nil {
		t.Fatalf("parseWebhookEvents error = %v", err)
	}
	if rejected != 0 {
		t.Fatalf("rejected = %d, want 0", rejected)
	}
	if len(events) != 2 {
		t.Fatalf("len(events) = %d, want 2", len(events))
	}
	if events[0].Type != "bounce_permanent" {
		t.Fatalf("type = %q, want bounce_permanent", events[0].Type)
	}
	if events[0].Email != "hard@example.com" || events[1].Email != "another@example.com" {
		t.Fatalf("recipients = %q, %q; want bounced recipient addresses", events[0].Email, events[1].Email)
	}
	if events[0].Reason != "Permanent/General" {
		t.Fatalf("reason = %q, want Permanent/General", events[0].Reason)
	}
}

func TestParseWebhookEventsProviderBounceClassification(t *testing.T) {
	tests := []struct {
		name string
		body string
		want string
	}{
		{name: "ses transient", body: `{"eventType":"Bounce","mail":{"destination":["a@example.com"]},"bounce":{"bounceType":"Transient","bouncedRecipients":[{"emailAddress":"a@example.com"}]}}`, want: "bounce_temporary"},
		{name: "mailgun temporary", body: `{"event-data":{"event":"failed","severity":"temporary","recipient":"a@example.com","reason":"mailbox full"}}`, want: "bounce_temporary"},
		{name: "mailgun permanent", body: `{"event-data":{"event":"failed","severity":"permanent","recipient":"a@example.com"}}`, want: "bounce_permanent"},
		{name: "postmark hard", body: `{"RecordType":"Bounce","Type":"HardBounce","TypeCode":1,"Email":"a@example.com"}`, want: "bounce_permanent"},
		{name: "postmark soft", body: `{"RecordType":"Bounce","Type":"SoftBounce","TypeCode":4096,"Email":"a@example.com"}`, want: "bounce_temporary"},
		{name: "enhanced smtp temporary", body: `{"event":"bounce","email":"a@example.com","status":"4.2.2"}`, want: "bounce_temporary"},
		{name: "enhanced smtp permanent", body: `{"event":"bounce","email":"a@example.com","status":"5.1.1"}`, want: "bounce_permanent"},
		{name: "generic bounce", body: `{"event":"bounce","email":"a@example.com"}`, want: "bounce_unclassified"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			events, _, err := parseWebhookEvents([]byte(tt.body))
			if err != nil {
				t.Fatalf("parseWebhookEvents error = %v", err)
			}
			if len(events) != 1 || events[0].Type != tt.want {
				t.Fatalf("events = %#v, want one %q event", events, tt.want)
			}
		})
	}
}

func TestParseWebhookEventsRejectInvalidPayload(t *testing.T) {
	body := []byte(`[{"event":"processed","email":"nope@example.com"}]`)
	_, _, err := parseWebhookEvents(body)
	if err == nil {
		t.Fatalf("expected error for payload with no actionable events")
	}
}
