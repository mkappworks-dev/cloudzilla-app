CREATE TABLE device_grants (
    id               BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    device_code_hash TEXT NOT NULL UNIQUE,
    user_code        TEXT NOT NULL UNIQUE,
    scopes           TEXT NOT NULL,
    device_name      TEXT NOT NULL DEFAULT '',
    status           TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'approved', 'denied', 'consumed')),
    user_id          BIGINT REFERENCES users(id) ON DELETE CASCADE,
    requester_ip     TEXT NOT NULL,
    interval_secs    INT NOT NULL DEFAULT 5,
    last_polled_at   TIMESTAMPTZ,
    expires_at       TIMESTAMPTZ NOT NULL,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX idx_device_grants_live_ip ON device_grants(requester_ip) WHERE status IN ('pending', 'approved');
CREATE INDEX idx_device_grants_created ON device_grants(created_at);
