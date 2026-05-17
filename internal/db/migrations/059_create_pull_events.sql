CREATE TABLE IF NOT EXISTS pull_events (
    id         BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    pull_id    BIGINT NOT NULL REFERENCES pull_requests(id) ON DELETE CASCADE,
    actor_id   BIGINT NOT NULL REFERENCES users(id),
    actor_name TEXT NOT NULL,
    event_type TEXT NOT NULL,
    detail     TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_pull_events_pull ON pull_events (pull_id, created_at);
