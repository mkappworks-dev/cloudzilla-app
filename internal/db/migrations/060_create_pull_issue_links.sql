-- Explicit links between pull requests and issues, set from the PR sidebar
-- picker. Distinct from #N references parsed out of the PR body.
CREATE TABLE IF NOT EXISTS pull_issue_links (
    pull_id  BIGINT REFERENCES pull_requests(id) ON DELETE CASCADE,
    issue_id BIGINT REFERENCES issues(id) ON DELETE CASCADE,
    PRIMARY KEY (pull_id, issue_id)
);
