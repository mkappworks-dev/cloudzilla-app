-- NULL for repo_transfer, which has no thread, and for mentions whose URL names none.
ALTER TABLE notifications ADD COLUMN IF NOT EXISTS subject_kind TEXT
    CHECK (subject_kind IN ('issue', 'pull', 'discussion'));

-- Mention URLs are matched whole: a repo named "issues" must not read as an issue URL.
UPDATE notifications SET subject_kind = CASE
    WHEN type IN ('issue_comment', 'issue_closed', 'issue_reopened') THEN 'issue'
    WHEN type IN ('pr_comment', 'pr_merged', 'pr_closed', 'pr_opened', 'pr_review') THEN 'pull'
    WHEN type = 'discussion_reply' THEN 'discussion'
    WHEN type = 'mention' AND subject_url ~ '^/[^/]+/[^/]+/issues/[0-9]+$' THEN 'issue'
    WHEN type = 'mention' AND subject_url ~ '^/[^/]+/[^/]+/pulls/[0-9]+$' THEN 'pull'
    WHEN type = 'mention' AND subject_url ~ '^/[^/]+/[^/]+/discussions/[0-9]+$' THEN 'discussion'
END
WHERE subject_kind IS NULL;
