CREATE TABLE IF NOT EXISTS webhooks (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    repo_id    INTEGER NOT NULL REFERENCES repositories(id) ON DELETE CASCADE,
    url        TEXT    NOT NULL,
    secret     TEXT    NOT NULL DEFAULT '',
    events     TEXT    NOT NULL DEFAULT 'push,issues,pull_request',
    active     BOOLEAN NOT NULL DEFAULT 1,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX IF NOT EXISTS idx_webhooks_repo ON webhooks(repo_id);

CREATE TABLE IF NOT EXISTS webhook_deliveries (
    id             INTEGER PRIMARY KEY AUTOINCREMENT,
    webhook_id     INTEGER NOT NULL REFERENCES webhooks(id) ON DELETE CASCADE,
    event          TEXT    NOT NULL,
    payload        TEXT    NOT NULL,
    response_code  INTEGER,
    response_body  TEXT    NOT NULL DEFAULT '',
    error          TEXT    NOT NULL DEFAULT '',
    delivered_at   DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX IF NOT EXISTS idx_webhook_deliveries_wh ON webhook_deliveries(webhook_id);
