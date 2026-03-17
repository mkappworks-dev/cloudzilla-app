CREATE TABLE IF NOT EXISTS invitations (
    id            INTEGER PRIMARY KEY AUTOINCREMENT,
    token         TEXT    NOT NULL UNIQUE,
    email         TEXT    NOT NULL,
    invited_by_id INTEGER NOT NULL REFERENCES users(id),
    expires_at    DATETIME NOT NULL,
    accepted_at   DATETIME,
    created_at    DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

ALTER TABLE users ADD COLUMN is_invited BOOLEAN NOT NULL DEFAULT FALSE;
