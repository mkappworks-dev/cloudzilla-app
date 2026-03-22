CREATE TABLE pull_line_comments (
    id          BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    pull_id     BIGINT NOT NULL REFERENCES pull_requests(id) ON DELETE CASCADE,
    repo_id     BIGINT NOT NULL REFERENCES repositories(id)  ON DELETE CASCADE,
    author_id   BIGINT NOT NULL REFERENCES users(id),
    author_name TEXT   NOT NULL DEFAULT '',
    path        TEXT   NOT NULL,
    diff_side   TEXT   NOT NULL DEFAULT 'right' CHECK(diff_side IN ('left','right')),
    line        INTEGER NOT NULL,
    body        TEXT   NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX idx_pull_line_comments_pull      ON pull_line_comments(pull_id);
CREATE INDEX idx_pull_line_comments_pull_path ON pull_line_comments(pull_id, path);
