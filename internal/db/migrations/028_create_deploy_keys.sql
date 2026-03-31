CREATE TABLE deploy_keys (
    id           BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    repo_id      BIGINT NOT NULL REFERENCES repositories(id) ON DELETE CASCADE,
    title        TEXT NOT NULL,
    fingerprint  TEXT NOT NULL,
    public_key   TEXT NOT NULL,
    read_only    BOOLEAN NOT NULL DEFAULT TRUE,
    last_used_at TIMESTAMPTZ,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE(repo_id, fingerprint)
);
CREATE INDEX idx_deploy_keys_fingerprint ON deploy_keys(fingerprint);
