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
