CREATE TABLE IF NOT EXISTS comments (
    id         BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    repo_id    BIGINT  NOT NULL REFERENCES repositories(id) ON DELETE CASCADE,
    issue_id   BIGINT  REFERENCES issues(id) ON DELETE CASCADE,
    pull_id    BIGINT  REFERENCES pull_requests(id) ON DELETE CASCADE,
    author_id  BIGINT  NOT NULL REFERENCES users(id),
    body       TEXT    NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CHECK (
        (issue_id IS NOT NULL AND pull_id IS NULL) OR
        (issue_id IS NULL AND pull_id IS NOT NULL)
    )
);

CREATE INDEX IF NOT EXISTS idx_comments_issue ON comments(issue_id);
CREATE INDEX IF NOT EXISTS idx_comments_pull  ON comments(pull_id);
