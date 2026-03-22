CREATE TABLE pull_reviews (
    id          BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    pull_id     BIGINT NOT NULL REFERENCES pull_requests(id) ON DELETE CASCADE,
    repo_id     BIGINT NOT NULL REFERENCES repositories(id)  ON DELETE CASCADE,
    author_id   BIGINT NOT NULL REFERENCES users(id),
    author_name TEXT   NOT NULL DEFAULT '',
    state       TEXT   NOT NULL CHECK(state IN ('approved','changes_requested','commented','pending')),
    body        TEXT   NOT NULL DEFAULT '',
    submitted_at TIMESTAMPTZ,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE(pull_id, author_id)
);
CREATE INDEX idx_pull_reviews_pull ON pull_reviews(pull_id);
