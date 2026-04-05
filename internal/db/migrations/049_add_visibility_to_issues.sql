ALTER TABLE issues
    ADD COLUMN IF NOT EXISTS visibility TEXT NOT NULL DEFAULT 'public'
        CHECK(visibility IN ('public','private'));
CREATE INDEX IF NOT EXISTS idx_issues_visibility ON issues(repo_id, visibility);
