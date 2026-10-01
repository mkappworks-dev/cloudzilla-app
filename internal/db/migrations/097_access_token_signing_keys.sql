-- A token bound to a signing key works only for requests signed with its
-- private key, so a copy of the token string alone is useless.
ALTER TABLE access_tokens ADD COLUMN IF NOT EXISTS signing_key TEXT NOT NULL DEFAULT '';

-- Nonces of signed requests, kept past the clock-skew window so none is replayed.
CREATE TABLE IF NOT EXISTS access_token_nonces (
    token_id   BIGINT      NOT NULL REFERENCES access_tokens(id) ON DELETE CASCADE,
    nonce      TEXT        NOT NULL,
    expires_at TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (token_id, nonce)
);
