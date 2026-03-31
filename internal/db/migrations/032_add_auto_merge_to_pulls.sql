ALTER TABLE pull_requests
    ADD COLUMN IF NOT EXISTS auto_merge_enabled  BOOLEAN NOT NULL DEFAULT FALSE,
    ADD COLUMN IF NOT EXISTS auto_merge_strategy TEXT CHECK (auto_merge_strategy IN ('ff', 'merge', 'squash'));
