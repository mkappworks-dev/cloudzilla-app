CREATE TABLE instance_signing_keys (
    id             BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    public_key     TEXT NOT NULL,
    fingerprint    TEXT NOT NULL UNIQUE,
    sealed_private BYTEA NOT NULL,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    retired_at     TIMESTAMPTZ
);

-- The constant expression makes every row with retired_at NULL collide, so at most one key is active.
CREATE UNIQUE INDEX idx_instance_signing_keys_active ON instance_signing_keys ((TRUE)) WHERE retired_at IS NULL;
