CREATE TABLE IF NOT EXISTS webhook_events (
    id BIGSERIAL PRIMARY KEY,
    workspace_id BIGINT NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    event_type TEXT NOT NULL,
    email TEXT,
    reason TEXT,
    status TEXT NOT NULL,
    attempt_count INT NOT NULL DEFAULT 0,
    last_error TEXT,
    raw_payload TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_webhook_events_workspace_created_at
    ON webhook_events (workspace_id, created_at DESC);

CREATE INDEX IF NOT EXISTS idx_webhook_events_status_created_at
    ON webhook_events (status, created_at DESC);
