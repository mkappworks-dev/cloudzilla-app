CREATE TABLE watches (
    id         BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    user_id    BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    repo_id    BIGINT NOT NULL REFERENCES repositories(id) ON DELETE CASCADE,
    level      TEXT NOT NULL DEFAULT 'watching' CHECK(level IN ('watching','releases_only','ignoring')),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE(user_id, repo_id)
);
CREATE INDEX idx_watches_repo ON watches(repo_id);
CREATE INDEX idx_watches_user ON watches(user_id);
