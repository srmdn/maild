package domain

import "time"

type Message struct {
	ID          int64      `json:"id"`
	WorkspaceID int64      `json:"workspace_id"`
	FromEmail   string     `json:"from_email"`
	ToEmail     string     `json:"to_email"`
	Subject     string     `json:"subject"`
	BodyText    string     `json:"body_text"`
	Status      string     `json:"status"`
	ScheduledAt *time.Time `json:"scheduled_at,omitempty"`
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
}

type MessageRetryOutcome struct {
	MessageID int64  `json:"message_id"`
	Status    string `json:"status"`
	Error     string `json:"error,omitempty"`
}

type MessageRetryResult struct {
	WorkspaceID  int64                 `json:"workspace_id"`
	Requested    int                   `json:"requested"`
	Retried      int                   `json:"retried"`
	Skipped      int                   `json:"skipped"`
	Failed       int                   `json:"failed"`
	ReplaySource string                `json:"replay_source"`
	Outcomes     []MessageRetryOutcome `json:"outcomes"`
}
