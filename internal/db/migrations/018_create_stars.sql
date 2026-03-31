CREATE TABLE stars (
    user_id    BIGINT REFERENCES users(id) ON DELETE CASCADE,
    repo_id    BIGINT REFERENCES repositories(id) ON DELETE CASCADE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY(user_id, repo_id)
);

CREATE INDEX idx_stars_repo ON stars(repo_id);
CREATE INDEX idx_stars_user ON stars(user_id);
