ALTER TABLE pull_requests
    ADD COLUMN IF NOT EXISTS head_repo_id    BIGINT REFERENCES repositories(id) ON DELETE SET NULL,
    ADD COLUMN IF NOT EXISTS head_repo_label TEXT   NOT NULL DEFAULT '';

UPDATE pull_requests SET head_repo_id = repo_id WHERE head_repo_id IS NULL;

CREATE INDEX IF NOT EXISTS idx_prs_head_repo_branch ON pull_requests(head_repo_id, head_branch) WHERE state = 'open';
