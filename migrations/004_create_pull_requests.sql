CREATE TABLE IF NOT EXISTS pull_requests (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    repo_id     INTEGER NOT NULL REFERENCES repositories(id) ON DELETE CASCADE,
    number      INTEGER NOT NULL,
    author_id   INTEGER NOT NULL REFERENCES users(id),
    title       TEXT    NOT NULL,
    body        TEXT    NOT NULL DEFAULT '',
    state       TEXT    NOT NULL DEFAULT 'open' CHECK(state IN ('open','closed','merged')),
    head_branch TEXT    NOT NULL,
    base_branch TEXT    NOT NULL DEFAULT 'main',
    created_at  DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at  DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    merged_at   DATETIME,
    closed_at   DATETIME,
    UNIQUE(repo_id, number)
);

CREATE INDEX IF NOT EXISTS idx_prs_repo   ON pull_requests(repo_id);
CREATE INDEX IF NOT EXISTS idx_prs_author ON pull_requests(author_id);
CREATE INDEX IF NOT EXISTS idx_prs_state  ON pull_requests(state);
