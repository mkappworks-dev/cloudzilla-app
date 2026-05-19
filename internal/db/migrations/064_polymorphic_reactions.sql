-- Reactions become polymorphic: each row targets exactly one of a comment,
-- a discussion post, or a discussion reply. Existing rows keep comment_id set.
ALTER TABLE reactions ALTER COLUMN comment_id DROP NOT NULL;
ALTER TABLE reactions ADD COLUMN discussion_id BIGINT REFERENCES discussions(id) ON DELETE CASCADE;
ALTER TABLE reactions ADD COLUMN discussion_reply_id BIGINT REFERENCES discussion_replies(id) ON DELETE CASCADE;

ALTER TABLE reactions ADD CONSTRAINT reactions_one_target CHECK (
    (comment_id IS NOT NULL)::int
  + (discussion_id IS NOT NULL)::int
  + (discussion_reply_id IS NOT NULL)::int = 1
);

-- Replace the comment-only uniqueness with one partial index per target type.
ALTER TABLE reactions DROP CONSTRAINT reactions_user_id_comment_id_emoji_key;
CREATE UNIQUE INDEX reactions_uq_comment ON reactions (user_id, comment_id, emoji) WHERE comment_id IS NOT NULL;
CREATE UNIQUE INDEX reactions_uq_discussion ON reactions (user_id, discussion_id, emoji) WHERE discussion_id IS NOT NULL;
CREATE UNIQUE INDEX reactions_uq_reply ON reactions (user_id, discussion_reply_id, emoji) WHERE discussion_reply_id IS NOT NULL;

CREATE INDEX idx_reactions_discussion ON reactions(discussion_id);
CREATE INDEX idx_reactions_reply ON reactions(discussion_reply_id);
