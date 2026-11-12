ALTER TABLE pull_reviews DROP CONSTRAINT pull_reviews_state_check;
ALTER TABLE pull_reviews ADD CONSTRAINT pull_reviews_state_check
    CHECK (state IN ('approved', 'changes_requested', 'commented', 'pending', 'dismissed'));

-- Names, not user ids: the entry keeps reading right after the dismisser's account is deleted.
ALTER TABLE pull_reviews
    ADD COLUMN dismissed_by_name TEXT NOT NULL DEFAULT '',
    ADD COLUMN dismissed_reason  TEXT NOT NULL DEFAULT '',
    ADD COLUMN dismissed_at      TIMESTAMPTZ;
