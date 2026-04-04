CREATE TABLE discussion_categories (
    id      BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    repo_id BIGINT NOT NULL REFERENCES repositories(id) ON DELETE CASCADE,
    name    TEXT NOT NULL,
    emoji   TEXT NOT NULL DEFAULT '',
    UNIQUE(repo_id, name)
);

CREATE TABLE discussions (
    id           BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    repo_id      BIGINT NOT NULL REFERENCES repositories(id) ON DELETE CASCADE,
    category_id  BIGINT NOT NULL REFERENCES discussion_categories(id) ON DELETE CASCADE,
    number       INTEGER NOT NULL,
    title        TEXT NOT NULL,
    body         TEXT NOT NULL DEFAULT '',
    author_id    BIGINT NOT NULL REFERENCES users(id),
    author_name  TEXT NOT NULL DEFAULT '',
    is_locked    BOOLEAN NOT NULL DEFAULT FALSE,
    is_answered  BOOLEAN NOT NULL DEFAULT FALSE,
    answer_id    BIGINT,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE(repo_id, number)
);

CREATE TABLE discussion_replies (
    id            BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    discussion_id BIGINT NOT NULL REFERENCES discussions(id) ON DELETE CASCADE,
    parent_id     BIGINT REFERENCES discussion_replies(id) ON DELETE CASCADE,
    author_id     BIGINT NOT NULL REFERENCES users(id),
    author_name   TEXT NOT NULL DEFAULT '',
    body          TEXT NOT NULL,
    is_answer     BOOLEAN NOT NULL DEFAULT FALSE,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

ALTER TABLE discussions
    ADD CONSTRAINT fk_answer
    FOREIGN KEY (answer_id) REFERENCES discussion_replies(id)
    ON DELETE SET NULL DEFERRABLE INITIALLY DEFERRED;

CREATE INDEX idx_discussions_repo ON discussions(repo_id, created_at DESC);
CREATE INDEX idx_discussion_replies_discussion ON discussion_replies(discussion_id);
