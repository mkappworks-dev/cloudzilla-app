-- migrations/033_create_reactions.sql
CREATE TABLE reactions (
    id         BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    user_id    BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    comment_id BIGINT NOT NULL REFERENCES comments(id) ON DELETE CASCADE,
    emoji      TEXT NOT NULL CHECK(emoji IN ('+1','-1','laugh','hooray','confused','heart','rocket','eyes')),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE(user_id, comment_id, emoji)
);
CREATE INDEX idx_reactions_comment ON reactions(comment_id);
