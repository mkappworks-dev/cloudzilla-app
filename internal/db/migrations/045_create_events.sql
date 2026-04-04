CREATE TABLE events (
    id         BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    actor_id   BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    actor_name TEXT NOT NULL DEFAULT '',
    repo_id    BIGINT REFERENCES repositories(id) ON DELETE CASCADE,
    repo_name  TEXT NOT NULL DEFAULT '',
    owner_name TEXT NOT NULL DEFAULT '',
    event_type TEXT NOT NULL,
    payload    JSONB NOT NULL DEFAULT '{}',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX idx_events_actor ON events(actor_id, created_at DESC);
CREATE INDEX idx_events_repo  ON events(repo_id,  created_at DESC);
