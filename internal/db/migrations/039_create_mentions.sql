CREATE TABLE mentions (
    id         BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    comment_id BIGINT NOT NULL REFERENCES comments(id) ON DELETE CASCADE,
    user_id    BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE(comment_id, user_id)
);

CREATE INDEX idx_mentions_user ON mentions(user_id);
