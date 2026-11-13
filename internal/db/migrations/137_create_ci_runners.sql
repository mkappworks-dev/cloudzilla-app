CREATE TABLE ci_runners (
    id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    scope TEXT NOT NULL CHECK (scope IN ('instance', 'repository')),
    repo_id BIGINT REFERENCES repositories(id) ON DELETE CASCADE,
    name TEXT NOT NULL,
    labels JSONB NOT NULL DEFAULT '[]',
    token_hash TEXT NOT NULL UNIQUE,
    version TEXT NOT NULL DEFAULT '',
    last_contact_at TIMESTAMPTZ,
    revoked_at TIMESTAMPTZ,
    created_by BIGINT REFERENCES users(id) ON DELETE SET NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CHECK ((scope = 'instance') = (repo_id IS NULL))
);
CREATE INDEX idx_ci_runners_repo ON ci_runners(repo_id) WHERE repo_id IS NOT NULL;

CREATE TABLE ci_runner_registration_tokens (
    id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    scope TEXT NOT NULL CHECK (scope IN ('instance', 'repository')),
    repo_id BIGINT REFERENCES repositories(id) ON DELETE CASCADE,
    token_hash TEXT NOT NULL UNIQUE,
    created_by BIGINT REFERENCES users(id) ON DELETE SET NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    expires_at TIMESTAMPTZ NOT NULL,
    used_at TIMESTAMPTZ,
    CHECK ((scope = 'instance') = (repo_id IS NULL))
);

ALTER TABLE workflow_jobs
    ADD CONSTRAINT workflow_jobs_runner_fk FOREIGN KEY (runner_id) REFERENCES ci_runners(id) ON DELETE SET NULL;
-- Only the hash of the per-job token is kept; it stops matching once the Job leaves 'running'.
ALTER TABLE workflow_jobs ADD COLUMN token_hash TEXT NOT NULL DEFAULT '';
CREATE INDEX idx_workflow_jobs_token ON workflow_jobs(token_hash) WHERE token_hash <> '';
CREATE INDEX idx_workflow_jobs_lease ON workflow_jobs(lease_until) WHERE status = 'running';
