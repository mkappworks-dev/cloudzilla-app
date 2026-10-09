-- Images pasted into markdown. repo_id has no foreign key on purpose: a repo
-- removed by any path (purge, org or user delete) must leave these rows behind
-- so the sweep can still find and delete their objects.
CREATE TABLE IF NOT EXISTS attachments (
    token        TEXT        PRIMARY KEY,
    repo_id      BIGINT      NOT NULL,
    uploader_id  BIGINT      REFERENCES users(id) ON DELETE SET NULL,
    storage_key  TEXT        NOT NULL,
    content_type TEXT        NOT NULL,
    size_bytes   BIGINT      NOT NULL,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_attachments_repo_id ON attachments(repo_id);
CREATE INDEX IF NOT EXISTS idx_attachments_created_at ON attachments(created_at);
