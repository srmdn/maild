CREATE TABLE IF NOT EXISTS workspace_policies (
    workspace_id BIGINT PRIMARY KEY REFERENCES workspaces(id) ON DELETE CASCADE,
    rate_limit_workspace_per_hour INT NOT NULL,
    rate_limit_domain_per_hour INT NOT NULL,
    blocked_recipient_domains TEXT NOT NULL DEFAULT '',
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
