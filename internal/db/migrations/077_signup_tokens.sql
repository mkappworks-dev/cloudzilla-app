-- Links for email-first signup. Only each token's SHA-256 is stored; the raw
-- token exists only in the email. One row per address: a new request replaces
-- it, which also throttles mail to that inbox.
CREATE TABLE IF NOT EXISTS signup_tokens (
    id          BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    token_hash  TEXT        NOT NULL UNIQUE,
    email       TEXT        NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    expires_at  TIMESTAMPTZ NOT NULL,
    used_at     TIMESTAMPTZ
);

CREATE UNIQUE INDEX IF NOT EXISTS signup_tokens_email_lower_key ON signup_tokens (lower(email));
