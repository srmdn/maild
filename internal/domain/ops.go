package domain

import "time"

type OnboardingChecklistItem struct {
	ID          string `json:"id"`
	Title       string `json:"title"`
	Description string `json:"description"`
	Done        bool   `json:"done"`
	Evidence    string `json:"evidence,omitempty"`
	Action      string `json:"action,omitempty"`
}

type OnboardingChecklist struct {
	WorkspaceID int64                     `json:"workspace_id"`
	GeneratedAt time.Time                 `json:"generated_at"`
	Completed   int                       `json:"completed"`
	Total       int                       `json:"total"`
	Items       []OnboardingChecklistItem `json:"items"`
}

type IncidentBundleSummary struct {
	AttemptedSends         int `json:"attempted_sends"`
	FailedAttempts         int `json:"failed_attempts"`
	RelatedWebhookOutcomes int `json:"related_webhook_outcomes"`
	DeadLetterWebhooks     int `json:"dead_letter_webhooks"`
}

type IncidentBundle struct {
	WorkspaceID       int64                 `json:"workspace_id"`
	MessageID         int64                 `json:"message_id"`
	GeneratedAt       time.Time             `json:"generated_at"`
	WebhookWindowFrom time.Time             `json:"webhook_window_from"`
	WebhookWindowTo   time.Time             `json:"webhook_window_to"`
	Message           Message               `json:"message"`
	Attempts          []MessageAttempt      `json:"attempts"`
	WebhookOutcomes   []WebhookEvent        `json:"webhook_outcomes"`
	Summary           IncidentBundleSummary `json:"summary"`
}
