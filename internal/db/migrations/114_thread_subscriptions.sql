-- No FK on number: issues, pulls and discussions are separate tables, and rows for deleted threads are left orphaned.
CREATE TABLE thread_subscriptions (
    user_id    BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    repo_id    BIGINT NOT NULL REFERENCES repositories(id) ON DELETE CASCADE,
    kind       TEXT NOT NULL CHECK (kind IN ('issue','pull','discussion')),
    number     BIGINT NOT NULL,
    state      TEXT NOT NULL CHECK (state IN ('subscribed','muted')),
    reason     TEXT NOT NULL CHECK (reason IN ('author','comment','review','mention','assign','manual')),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (user_id, repo_id, kind, number)
);
CREATE INDEX idx_thread_subscriptions_thread ON thread_subscriptions(repo_id, kind, number, state);
