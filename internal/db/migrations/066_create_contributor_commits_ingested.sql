CREATE TABLE IF NOT EXISTS contributor_commits_ingested (
    repo_id BIGINT NOT NULL REFERENCES repositories(id) ON DELETE CASCADE,
    sha TEXT NOT NULL,
    user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    week DATE NOT NULL,
    additions INT NOT NULL DEFAULT 0,
    deletions INT NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (repo_id, sha)
);

CREATE INDEX IF NOT EXISTS idx_contributor_commits_repo_user_week
    ON contributor_commits_ingested (repo_id, user_id, week);
