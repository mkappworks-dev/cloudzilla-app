-- Only a SHA-256 of each token is stored, so a database read cannot verify anyone's address.
-- Spent and revoked rows stay until the user's next link replaces them, because the resend
-- cooldown counts them; a deleted user's rows keep counting for the address (user_id NULL).
CREATE TABLE IF NOT EXISTS email_verification_tokens (
    id         BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    user_id    BIGINT      REFERENCES users(id) ON DELETE SET NULL,
    email      TEXT        NOT NULL,
    token_hash TEXT        NOT NULL UNIQUE,
    expires_at TIMESTAMPTZ NOT NULL,
    used_at    TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_email_verification_tokens_user ON email_verification_tokens (user_id, created_at);
CREATE INDEX IF NOT EXISTS idx_email_verification_tokens_email ON email_verification_tokens (lower(email), created_at);
