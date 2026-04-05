CREATE TABLE gists (
    id          TEXT PRIMARY KEY,
    owner_id    BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    owner_name  TEXT NOT NULL DEFAULT '',
    description TEXT NOT NULL DEFAULT '',
    public      BOOLEAN NOT NULL DEFAULT TRUE,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE TABLE gist_files (
    id       BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    gist_id  TEXT NOT NULL REFERENCES gists(id) ON DELETE CASCADE,
    filename TEXT NOT NULL,
    content  TEXT NOT NULL DEFAULT '',
    UNIQUE(gist_id, filename)
);
CREATE INDEX idx_gists_owner ON gists(owner_id, created_at DESC);
