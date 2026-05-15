ALTER TABLE projects ADD COLUMN closed_at TIMESTAMPTZ;

CREATE INDEX idx_projects_repo_status ON projects (repo_id, closed_at);
