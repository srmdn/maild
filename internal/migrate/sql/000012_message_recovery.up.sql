CREATE INDEX IF NOT EXISTS idx_messages_queued_recovery
    ON messages (updated_at, id)
    WHERE status = 'queued';

CREATE INDEX IF NOT EXISTS idx_messages_sending_recovery
    ON messages (updated_at)
    WHERE status = 'sending';
