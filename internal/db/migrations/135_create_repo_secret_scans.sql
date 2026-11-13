CREATE TABLE repo_secret_scans (
    repo_id     BIGINT PRIMARY KEY REFERENCES repositories(id) ON DELETE CASCADE,
    state       TEXT NOT NULL DEFAULT 'pending' CHECK (state IN ('pending','running','done','failed')),
    incomplete  BOOLEAN NOT NULL DEFAULT FALSE,
    lease_until TIMESTAMPTZ,
    scanned_at  TIMESTAMPTZ,
    error       TEXT NOT NULL DEFAULT ''
);

INSERT INTO repo_secret_scans (repo_id) SELECT id FROM repositories;

-- A trigger rather than a call per creation path, so create, fork, import and org creation cannot miss one.
CREATE FUNCTION queue_repo_secret_scan() RETURNS trigger AS $$
BEGIN
    INSERT INTO repo_secret_scans (repo_id) VALUES (NEW.id) ON CONFLICT DO NOTHING;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER repositories_queue_secret_scan AFTER INSERT ON repositories
    FOR EACH ROW EXECUTE FUNCTION queue_repo_secret_scan();
