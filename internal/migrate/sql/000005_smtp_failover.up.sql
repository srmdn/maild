ALTER TABLE smtp_accounts
    ADD COLUMN IF NOT EXISTS is_active BOOLEAN NOT NULL DEFAULT FALSE;

DO $$
BEGIN
    IF EXISTS (
        SELECT 1
        FROM pg_constraint
        WHERE conname = 'smtp_accounts_workspace_id_key'
    ) THEN
        ALTER TABLE smtp_accounts DROP CONSTRAINT smtp_accounts_workspace_id_key;
    END IF;
END
$$;

CREATE UNIQUE INDEX IF NOT EXISTS idx_smtp_accounts_workspace_name_unique
    ON smtp_accounts (workspace_id, name);

CREATE UNIQUE INDEX IF NOT EXISTS idx_smtp_accounts_workspace_active_unique
    ON smtp_accounts (workspace_id)
    WHERE is_active;

UPDATE smtp_accounts sa
SET is_active = TRUE
WHERE sa.is_active = FALSE
  AND NOT EXISTS (
    SELECT 1
    FROM smtp_accounts x
    WHERE x.workspace_id = sa.workspace_id
      AND x.is_active = TRUE
  );
