package domain

import "time"

type MessageAttempt struct {
	ID           int64     `json:"id"`
	MessageID    int64     `json:"message_id"`
	AttemptNo    int       `json:"attempt_no"`
	SMTPProvider string    `json:"smtp_provider"`
	SMTPResponse string    `json:"smtp_response"`
	Success      bool      `json:"success"`
	CreatedAt    time.Time `json:"created_at"`
}
