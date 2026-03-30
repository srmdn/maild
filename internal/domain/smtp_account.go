package domain

import "time"

type SMTPAccount struct {
	WorkspaceID int64  `json:"workspace_id"`
	Name        string `json:"name"`
	Host        string `json:"host"`
	Port        int    `json:"port"`
	Username    string `json:"username"`
	Password    string `json:"password"`
	FromEmail   string `json:"from_email"`
}

type SMTPAccountSummary struct {
	WorkspaceID int64     `json:"workspace_id"`
	Name        string    `json:"name"`
	Active      bool      `json:"active"`
	UpdatedAt   time.Time `json:"updated_at"`
}
