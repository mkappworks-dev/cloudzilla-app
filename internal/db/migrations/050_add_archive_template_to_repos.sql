ALTER TABLE repositories
    ADD COLUMN IF NOT EXISTS is_archived  BOOLEAN    NOT NULL DEFAULT FALSE,
    ADD COLUMN IF NOT EXISTS archived_at  TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS is_template  BOOLEAN    NOT NULL DEFAULT FALSE;

CREATE INDEX IF NOT EXISTS idx_repos_template ON repositories(is_template) WHERE is_template = TRUE;
