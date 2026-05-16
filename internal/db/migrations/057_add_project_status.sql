ALTER TABLE projects ADD COLUMN IF NOT EXISTS closed_at TIMESTAMPTZ;

CREATE INDEX IF NOT EXISTS idx_projects_repo_status ON projects (repo_id, closed_at);
