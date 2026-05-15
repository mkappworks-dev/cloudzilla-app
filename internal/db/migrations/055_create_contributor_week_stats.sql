-- 055_create_contributor_week_stats.sql
-- Per-(repo, user, week) commit/lines aggregate. Backs the per-repo
-- contributors page. Week is the Monday-of-week date (UTC).
-- Populated on each push via RepoService.PostReceive hook.

CREATE TABLE IF NOT EXISTS contributor_week_stats (
    id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    repo_id BIGINT NOT NULL REFERENCES repositories(id) ON DELETE CASCADE,
    user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    week DATE NOT NULL,
    commits INT NOT NULL DEFAULT 0,
    additions INT NOT NULL DEFAULT 0,
    deletions INT NOT NULL DEFAULT 0,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (repo_id, user_id, week)
);

CREATE INDEX IF NOT EXISTS idx_contributor_week_stats_repo_user
    ON contributor_week_stats (repo_id, user_id, week DESC);
