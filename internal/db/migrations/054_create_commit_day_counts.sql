-- 054_create_commit_day_counts.sql
-- Per-(repo, user, day) commit-count aggregate. Backs the home and profile
-- contribution heatmaps. Populated on each push (via RepoService.OnPostReceive
-- in Phase 1 Task 4) and via a background backfill on first server start
-- after this migration runs.

CREATE TABLE IF NOT EXISTS commit_day_counts (
    id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    repo_id BIGINT NOT NULL REFERENCES repositories(id) ON DELETE CASCADE,
    user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    day DATE NOT NULL,
    commit_count INT NOT NULL DEFAULT 0,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (repo_id, user_id, day)
);

CREATE INDEX IF NOT EXISTS idx_commit_day_counts_user_day
    ON commit_day_counts (user_id, day DESC);

CREATE INDEX IF NOT EXISTS idx_commit_day_counts_repo_day
    ON commit_day_counts (repo_id, day DESC);
