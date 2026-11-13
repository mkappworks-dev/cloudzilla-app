-- Counters of what the log chunks hold, so the per-Job cap is checked without summing chunks and survives their expiry.
ALTER TABLE workflow_jobs
    ADD COLUMN log_bytes BIGINT NOT NULL DEFAULT 0,
    ADD COLUMN log_lines INTEGER NOT NULL DEFAULT 0,
    ADD COLUMN log_truncated BOOLEAN NOT NULL DEFAULT FALSE;

-- job_id and repo_id have no foreign keys: a repo, user or org delete cascades away the Job rows, and the sweep needs
-- these rows to find the objects it left behind.
CREATE TABLE workflow_log_chunks (
    id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    job_id BIGINT NOT NULL,
    repo_id BIGINT NOT NULL,
    -- Chosen by the runner so a retried POST is recognised instead of stored twice.
    seq INTEGER NOT NULL,
    first_line INTEGER NOT NULL,
    line_count INTEGER NOT NULL,
    size_bytes INTEGER NOT NULL,
    storage_key TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (job_id, seq),
    UNIQUE (job_id, first_line)
);
CREATE INDEX idx_workflow_log_chunks_created ON workflow_log_chunks(created_at);
CREATE INDEX idx_workflow_log_chunks_repo ON workflow_log_chunks(repo_id);

CREATE TABLE workflow_artifacts (
    id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    run_id BIGINT NOT NULL,
    job_id BIGINT NOT NULL,
    repo_id BIGINT NOT NULL,
    name TEXT NOT NULL,
    size BIGINT NOT NULL,
    sha256 TEXT NOT NULL,
    storage_key TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    expires_at TIMESTAMPTZ NOT NULL,
    UNIQUE (job_id, name)
);
CREATE INDEX idx_workflow_artifacts_run ON workflow_artifacts(run_id);
CREATE INDEX idx_workflow_artifacts_expires ON workflow_artifacts(expires_at);
CREATE INDEX idx_workflow_artifacts_repo ON workflow_artifacts(repo_id);
