CREATE TABLE saml_used_assertions (
    id           BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    assertion_id TEXT        NOT NULL UNIQUE,
    expires_at   TIMESTAMPTZ NOT NULL,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_saml_used_assertions_expires
    ON saml_used_assertions (expires_at);
