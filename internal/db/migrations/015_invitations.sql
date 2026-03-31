CREATE TABLE IF NOT EXISTS invitations (
    id            BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    token         TEXT    NOT NULL UNIQUE,
    email         TEXT    NOT NULL,
    invited_by_id BIGINT  NOT NULL REFERENCES users(id),
    expires_at    TIMESTAMPTZ NOT NULL,
    accepted_at   TIMESTAMPTZ,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

ALTER TABLE users ADD COLUMN IF NOT EXISTS is_invited BOOLEAN NOT NULL DEFAULT FALSE;
