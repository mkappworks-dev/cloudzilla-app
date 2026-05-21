ALTER TABLE gists ADD COLUMN forked_from_id TEXT NULL REFERENCES gists(id) ON DELETE SET NULL;
CREATE INDEX idx_gists_forked_from ON gists(forked_from_id);

CREATE TABLE gist_stars (
    id         BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    gist_id    TEXT   NOT NULL REFERENCES gists(id) ON DELETE CASCADE,
    user_id    BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE(gist_id, user_id)
);
CREATE INDEX idx_gist_stars_gist ON gist_stars(gist_id);
CREATE INDEX idx_gist_stars_user ON gist_stars(user_id);
