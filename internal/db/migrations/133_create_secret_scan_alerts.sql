CREATE TABLE secret_scan_alerts (
    id             BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    repo_id        BIGINT NOT NULL REFERENCES repositories(id) ON DELETE CASCADE,
    number         INTEGER NOT NULL,
    secret_type    TEXT NOT NULL,
    secret_hash    TEXT NOT NULL,
    secret_hint    TEXT NOT NULL,
    state          TEXT NOT NULL DEFAULT 'open' CHECK (state IN ('open','closed')),
    resolution     TEXT CHECK (resolution IN ('revoked','false_positive','used_in_tests')),
    resolved_by    BIGINT REFERENCES users(id) ON DELETE SET NULL,
    resolved_at    TIMESTAMPTZ,
    auto_revoked   BOOLEAN NOT NULL DEFAULT FALSE,
    incomplete     BOOLEAN NOT NULL DEFAULT FALSE,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CHECK ((state = 'closed') = (resolution IS NOT NULL)),
    UNIQUE (repo_id, number),
    UNIQUE (repo_id, secret_hash)
);
CREATE INDEX idx_secret_scan_alerts_repo_state ON secret_scan_alerts(repo_id, state);

CREATE TABLE secret_scan_locations (
    id          BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    alert_id    BIGINT NOT NULL REFERENCES secret_scan_alerts(id) ON DELETE CASCADE,
    commit_sha  TEXT NOT NULL,
    file_path   TEXT NOT NULL,
    line_number INTEGER NOT NULL,
    reached_ref BOOLEAN NOT NULL,
    UNIQUE (alert_id, commit_sha, file_path, line_number)
);
CREATE INDEX idx_secret_scan_locations_commit ON secret_scan_locations(commit_sha);

CREATE TABLE secret_scan_events (
    id         BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    alert_id   BIGINT NOT NULL REFERENCES secret_scan_alerts(id) ON DELETE CASCADE,
    kind       TEXT NOT NULL CHECK (kind IN ('found','blocked','closed','reopened','allowed','auto_revoked')),
    actor_id   BIGINT REFERENCES users(id) ON DELETE SET NULL,
    ref        TEXT NOT NULL DEFAULT '',
    commit_sha TEXT NOT NULL DEFAULT '',
    detail     TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX idx_secret_scan_events_alert ON secret_scan_events(alert_id, id);
