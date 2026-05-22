-- Cached head commit SHA per PR, so CI-check counts on list pages avoid a
-- git ref resolution per row.
ALTER TABLE pull_requests ADD COLUMN IF NOT EXISTS head_sha TEXT;
