package domain

import "time"

type MeteringEvent struct {
	ID          int64     `json:"id"`
	WorkspaceID int64     `json:"workspace_id"`
	MessageID   int64     `json:"message_id,omitempty"`
	EventType   string    `json:"event_type"`
	Quantity    int       `json:"quantity"`
	Metadata    string    `json:"metadata,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
}

type MeteringSummaryItem struct {
	EventType string `json:"event_type"`
	Total     int64  `json:"total"`
}
