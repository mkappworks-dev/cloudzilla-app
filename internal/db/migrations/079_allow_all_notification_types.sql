ALTER TABLE notifications
    DROP CONSTRAINT IF EXISTS notifications_type_check;

ALTER TABLE notifications
    ADD CONSTRAINT notifications_type_check CHECK (type IN (
        'issue_comment','pr_comment',
        'issue_closed','issue_reopened',
        'pr_merged','pr_closed','pr_opened',
        'pr_review','mention','discussion_reply'
    ));
