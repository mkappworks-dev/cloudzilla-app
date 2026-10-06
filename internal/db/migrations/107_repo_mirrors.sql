-- A pull mirror: the repo's branches and tags track remote_url and are
-- read-only locally. lease_until keeps a claimed sync from running twice
-- across instances and expires if the claiming instance dies. A request
-- (Sync now, new settings) after claimed_at outlives that sync's result.
CREATE TABLE IF NOT EXISTS repo_mirrors (
    repo_id              BIGINT      PRIMARY KEY REFERENCES repositories(id) ON DELETE CASCADE,
    remote_url           TEXT        NOT NULL,
    auth_username        TEXT        NOT NULL DEFAULT '',
    auth_token_enc       BYTEA,
    interval_seconds     INTEGER     NOT NULL CHECK (interval_seconds > 0),
    next_sync_at         TIMESTAMPTZ NOT NULL,
    lease_until          TIMESTAMPTZ,
    claimed_at           TIMESTAMPTZ,
    requested_at         TIMESTAMPTZ,
    last_sync_at         TIMESTAMPTZ,
    last_success_at      TIMESTAMPTZ,
    last_error           TEXT        NOT NULL DEFAULT '',
    consecutive_failures INTEGER     NOT NULL DEFAULT 0,
    created_by           BIGINT      REFERENCES users(id) ON DELETE SET NULL,
    created_at           TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at           TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_repo_mirrors_next_sync_at ON repo_mirrors(next_sync_at);
