ALTER TABLE pull_requests
    ADD COLUMN merged_base_sha TEXT,
    ADD COLUMN merged_head_sha TEXT;
