ALTER TABLE user_api_keys
    ADD COLUMN workspace_id BIGINT REFERENCES workspaces(id) ON DELETE CASCADE,
    ADD COLUMN key_prefix TEXT NOT NULL DEFAULT '';

-- Legacy keys belong to the same deterministic default workspace as the session.
-- Keys without a current membership remain unusable; their hashes are retained.
UPDATE user_api_keys k SET workspace_id = (
    SELECT uw.workspace_id FROM user_workspaces uw
    WHERE uw.user_id = k.user_id
    ORDER BY uw.created_at, uw.workspace_id LIMIT 1
);

CREATE UNIQUE INDEX idx_user_api_keys_hash_unique ON user_api_keys (key_hash);
