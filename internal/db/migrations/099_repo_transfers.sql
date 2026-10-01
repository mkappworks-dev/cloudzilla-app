-- A transfer to another user waits here until they accept it; nothing moves
-- before then. A repository has at most one pending transfer.
CREATE TABLE IF NOT EXISTS repo_transfers (
    id           BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    repo_id      BIGINT      NOT NULL UNIQUE REFERENCES repositories(id) ON DELETE CASCADE,
    requester_id BIGINT      NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    recipient_id BIGINT      NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    expires_at   TIMESTAMPTZ NOT NULL,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_repo_transfers_recipient ON repo_transfers(recipient_id);

ALTER TABLE notifications
    DROP CONSTRAINT IF EXISTS notifications_type_check;

ALTER TABLE notifications
    ADD CONSTRAINT notifications_type_check CHECK (type IN (
        'issue_comment','pr_comment',
        'issue_closed','issue_reopened',
        'pr_merged','pr_closed','pr_opened',
        'pr_review','mention','discussion_reply',
        'repo_transfer'
    ));
