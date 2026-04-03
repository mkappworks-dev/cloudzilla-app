CREATE TABLE sso_configs (
    id         BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    provider   TEXT NOT NULL UNIQUE CHECK(provider IN ('ldap','saml')),
    config     JSONB NOT NULL DEFAULT '{}',
    enabled    BOOLEAN NOT NULL DEFAULT FALSE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

ALTER TABLE users
    ADD COLUMN IF NOT EXISTS sso_provider TEXT,
    ADD COLUMN IF NOT EXISTS sso_id       TEXT;

-- Partial unique index: only enforce uniqueness where both columns are non-NULL.
CREATE UNIQUE INDEX IF NOT EXISTS uq_users_sso
    ON users (sso_provider, sso_id)
    WHERE sso_provider IS NOT NULL AND sso_id IS NOT NULL;
