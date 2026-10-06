-- One row per account, so a new link (or the passwordless note, with no token) replaces the last.
-- emailed_at survives links an admin issues, so they don't reset the email cooldown. Only a
-- SHA-256 of each token is stored, so a database read cannot reset anyone's password.
CREATE TABLE IF NOT EXISTS password_reset_tokens (
    user_id         BIGINT      PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
    email           TEXT        NOT NULL,
    token_hash      TEXT        UNIQUE,
    session_version INTEGER     NOT NULL,
    issued_by       TEXT        NOT NULL CHECK (issued_by IN ('email', 'admin', 'cli')),
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    emailed_at      TIMESTAMPTZ,
    expires_at      TIMESTAMPTZ NOT NULL,
    used_at         TIMESTAMPTZ
);
