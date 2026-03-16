CREATE TABLE IF NOT EXISTS ssh_keys (
    id          INTEGER  PRIMARY KEY AUTOINCREMENT,
    user_id     INTEGER  NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    title       TEXT     NOT NULL,
    public_key  TEXT     NOT NULL,
    fingerprint TEXT     NOT NULL UNIQUE,
    created_at  DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_ssh_keys_user        ON ssh_keys(user_id);
CREATE INDEX IF NOT EXISTS idx_ssh_keys_fingerprint ON ssh_keys(fingerprint);
