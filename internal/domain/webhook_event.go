package domain

import "time"

type WebhookEvent struct {
	ID           int64     `json:"id"`
	WorkspaceID  int64     `json:"workspace_id"`
	EventType    string    `json:"event_type"`
	Email        string    `json:"email,omitempty"`
	Reason       string    `json:"reason,omitempty"`
	Status       string    `json:"status"`
	AttemptCount int       `json:"attempt_count"`
	LastError    string    `json:"last_error,omitempty"`
	RawPayload   string    `json:"raw_payload,omitempty"`
	CreatedAt    time.Time `json:"created_at"`
}

type WebhookReplayOutcome struct {
	EventID      int64  `json:"event_id"`
	Status       string `json:"status"`
	AttemptCount int    `json:"attempt_count"`
	Error        string `json:"error,omitempty"`
}

type WebhookReplayResult struct {
	WorkspaceID  int64                  `json:"workspace_id"`
	Requested    int                    `json:"requested"`
	Replayed     int                    `json:"replayed"`
	Failed       int                    `json:"failed"`
	ReplaySource string                 `json:"replay_source"`
	Outcomes     []WebhookReplayOutcome `json:"outcomes"`
}
