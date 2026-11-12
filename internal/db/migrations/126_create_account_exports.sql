-- started_at is the worker's lease: a running row older than the service's stale window is claimed again.
CREATE TABLE IF NOT EXISTS account_exports (
    id           BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    user_id      BIGINT      NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    status       TEXT        NOT NULL DEFAULT 'requested' CHECK (status IN ('requested', 'running', 'ready', 'failed')),
    storage_key  TEXT        NOT NULL DEFAULT '',
    error        TEXT        NOT NULL DEFAULT '',
    requested_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    started_at   TIMESTAMPTZ,
    ready_at     TIMESTAMPTZ,
    expires_at   TIMESTAMPTZ
);

-- One export in flight per user, enforced here so two instances cannot both accept a request.
CREATE UNIQUE INDEX IF NOT EXISTS account_exports_active_key ON account_exports (user_id) WHERE status IN ('requested', 'running');
CREATE INDEX IF NOT EXISTS idx_account_exports_user ON account_exports (user_id, requested_at DESC);
CREATE INDEX IF NOT EXISTS idx_account_exports_expires ON account_exports (expires_at) WHERE expires_at IS NOT NULL;
