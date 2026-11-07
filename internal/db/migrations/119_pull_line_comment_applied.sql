ALTER TABLE pull_line_comments
    ADD COLUMN applied_at    TIMESTAMPTZ,
    ADD COLUMN applied_sha   TEXT NOT NULL DEFAULT '',
    ADD COLUMN applied_by_id BIGINT REFERENCES users(id),
    ADD COLUMN applied_by    TEXT NOT NULL DEFAULT '';
