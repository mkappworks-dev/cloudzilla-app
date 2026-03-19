CREATE TABLE IF NOT EXISTS issues (
    id         BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    repo_id    BIGINT  NOT NULL REFERENCES repositories(id) ON DELETE CASCADE,
    number     INTEGER NOT NULL,
    author_id  BIGINT  NOT NULL REFERENCES users(id),
    title      TEXT    NOT NULL,
    body       TEXT    NOT NULL DEFAULT '',
    state      TEXT    NOT NULL DEFAULT 'open' CHECK(state IN ('open','closed')),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    closed_at  TIMESTAMPTZ,
    UNIQUE(repo_id, number)
);

CREATE INDEX IF NOT EXISTS idx_issues_repo   ON issues(repo_id);
CREATE INDEX IF NOT EXISTS idx_issues_author ON issues(author_id);
CREATE INDEX IF NOT EXISTS idx_issues_state  ON issues(state);
