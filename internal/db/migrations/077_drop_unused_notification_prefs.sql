ALTER TABLE users
    DROP COLUMN IF EXISTS notify_issue_assigned,
    DROP COLUMN IF EXISTS notify_watched,
    DROP COLUMN IF EXISTS notify_weekly_digest;
