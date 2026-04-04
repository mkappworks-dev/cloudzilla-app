CREATE TABLE oauth_apps (
    id            BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    owner_id      BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    name          TEXT NOT NULL,
    client_id     TEXT NOT NULL UNIQUE,
    client_secret TEXT NOT NULL,
    redirect_uris TEXT NOT NULL DEFAULT '',
    homepage_url  TEXT NOT NULL DEFAULT '',
    description   TEXT NOT NULL DEFAULT '',
    created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE oauth_authorizations (
    id              BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    app_id          BIGINT NOT NULL REFERENCES oauth_apps(id) ON DELETE CASCADE,
    user_id         BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    code            TEXT UNIQUE,
    token_hash      TEXT UNIQUE,
    scopes          TEXT NOT NULL DEFAULT '',
    code_expires_at TIMESTAMPTZ,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE(app_id, user_id)
);
