DROP INDEX idx_user_api_keys_hash_unique;
ALTER TABLE user_api_keys DROP COLUMN workspace_id, DROP COLUMN key_prefix;
