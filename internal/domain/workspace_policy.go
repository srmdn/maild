package domain

type WorkspacePolicy struct {
	WorkspaceID               int64    `json:"workspace_id"`
	RateLimitWorkspacePerHour int      `json:"rate_limit_workspace_per_hour"`
	RateLimitDomainPerHour    int      `json:"rate_limit_domain_per_hour"`
	BlockedRecipientDomains   []string `json:"blocked_recipient_domains"`
}
