CREATE TABLE IF NOT EXISTS repositories (
    id             INTEGER PRIMARY KEY AUTOINCREMENT,
    owner_id       INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    name           TEXT    NOT NULL,
    description    TEXT    NOT NULL DEFAULT '',
    private        BOOLEAN NOT NULL DEFAULT 0,
    default_branch TEXT    NOT NULL DEFAULT 'main',
    created_at     DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at     DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE(owner_id, name)
);

CREATE INDEX IF NOT EXISTS idx_repos_owner ON repositories(owner_id);
