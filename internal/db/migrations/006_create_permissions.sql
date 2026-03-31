CREATE TABLE IF NOT EXISTS permissions (
    id         BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    user_id    BIGINT  NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    repo_id    BIGINT  NOT NULL REFERENCES repositories(id) ON DELETE CASCADE,
    role       TEXT    NOT NULL CHECK(role IN ('owner','admin','writer','reader')),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE(user_id, repo_id)
);

CREATE INDEX IF NOT EXISTS idx_permissions_user ON permissions(user_id);
CREATE INDEX IF NOT EXISTS idx_permissions_repo ON permissions(repo_id);
