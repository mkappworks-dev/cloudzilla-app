ALTER TABLE repositories
    ADD COLUMN IF NOT EXISTS deleted_at  TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS deleted_by  BIGINT REFERENCES users(id);

CREATE INDEX IF NOT EXISTS idx_repos_deleted ON repositories(deleted_at) WHERE deleted_at IS NOT NULL;
