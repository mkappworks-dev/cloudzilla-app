CREATE TABLE IF NOT EXISTS site_settings (
    key   TEXT PRIMARY KEY,
    value TEXT NOT NULL
);

INSERT INTO site_settings (key, value) VALUES ('allow_registration', 'true') ON CONFLICT DO NOTHING;
INSERT INTO site_settings (key, value) VALUES ('allow_login', 'true') ON CONFLICT DO NOTHING;
