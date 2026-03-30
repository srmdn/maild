CREATE TABLE IF NOT EXISTS metering_events (
    id BIGSERIAL PRIMARY KEY,
    workspace_id BIGINT NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    message_id BIGINT REFERENCES messages(id) ON DELETE SET NULL,
    event_type TEXT NOT NULL,
    quantity INT NOT NULL DEFAULT 1,
    metadata JSONB,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_metering_events_workspace_created_at
    ON metering_events (workspace_id, created_at DESC);

CREATE INDEX IF NOT EXISTS idx_metering_events_type_created_at
    ON metering_events (event_type, created_at DESC);
