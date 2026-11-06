-- One row per browser a superadmin entered admin mode in. Only the token's SHA-256 is kept.
-- session_version is the user's at entry: any bump (revoke, password change, suspension) leaves the row dead.
CREATE TABLE admin_modes (
    id              BIGSERIAL   PRIMARY KEY,
    user_id         BIGINT      NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    token_hash      TEXT        NOT NULL UNIQUE,
    session_version INTEGER     NOT NULL,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    expires_at      TIMESTAMPTZ NOT NULL,
    ended_at        TIMESTAMPTZ
);

CREATE INDEX admin_modes_user_id_idx ON admin_modes (user_id);
