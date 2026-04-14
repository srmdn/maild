ALTER TABLE user_workspaces ADD COLUMN IF NOT EXISTS onboarding_seen_at TIMESTAMPTZ;
