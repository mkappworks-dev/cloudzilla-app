-- The repo issue and PR lists filter and page in SQL; these back the state tabs, the label/assignee/milestone filters
-- and the default sorts.
CREATE INDEX IF NOT EXISTS idx_issues_repo_state_created ON issues (repo_id, state, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_prs_repo_state_number ON pull_requests (repo_id, state, number DESC);
CREATE INDEX IF NOT EXISTS idx_issue_labels_label ON issue_labels (label_id);
CREATE INDEX IF NOT EXISTS idx_pull_labels_label ON pull_labels (label_id);
CREATE INDEX IF NOT EXISTS idx_issue_assignees_user ON issue_assignees (user_id);
CREATE INDEX IF NOT EXISTS idx_pull_assignees_user ON pull_assignees (user_id);
CREATE INDEX IF NOT EXISTS idx_issues_milestone ON issues (milestone_id);
CREATE INDEX IF NOT EXISTS idx_prs_milestone ON pull_requests (milestone_id);
