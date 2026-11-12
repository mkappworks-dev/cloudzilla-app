-- repo_id is denormalised so a repo purge can find its objects without joining releases, which the cascade has already emptied.
CREATE TABLE IF NOT EXISTS release_assets (
    id          BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    release_id  BIGINT      NOT NULL REFERENCES releases(id) ON DELETE CASCADE,
    repo_id     BIGINT      NOT NULL,
    name        TEXT        NOT NULL,
    size        BIGINT      NOT NULL,
    sha256      TEXT        NOT NULL,
    storage_key TEXT        NOT NULL,
    uploader_id BIGINT      REFERENCES users(id) ON DELETE SET NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (release_id, name)
);
CREATE INDEX IF NOT EXISTS idx_release_assets_repo_id ON release_assets(repo_id);
