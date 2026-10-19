ALTER TABLE webhook_events
    ADD COLUMN IF NOT EXISTS identity_key TEXT;

UPDATE webhook_events
SET identity_key = 'legacy:' || id::text
WHERE identity_key IS NULL;

ALTER TABLE webhook_events
    ALTER COLUMN identity_key SET NOT NULL;

CREATE UNIQUE INDEX IF NOT EXISTS idx_webhook_events_workspace_identity
    ON webhook_events (workspace_id, identity_key);
