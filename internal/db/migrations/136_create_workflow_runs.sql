ALTER TABLE repositories ADD COLUMN ci_enabled BOOLEAN NOT NULL DEFAULT TRUE;

-- A counter row rather than MAX(number)+1, so two runs created at once cannot take the same number.
CREATE TABLE workflow_run_counters (
    repo_id BIGINT PRIMARY KEY REFERENCES repositories(id) ON DELETE CASCADE,
    last_number INTEGER NOT NULL
);

CREATE TABLE workflow_runs (
    id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    repo_id BIGINT NOT NULL REFERENCES repositories(id) ON DELETE CASCADE,
    number INTEGER NOT NULL,
    workflow_path TEXT NOT NULL,
    workflow_name TEXT NOT NULL,
    -- A re-run at the same SHA must see the file the first attempt saw, even if the branch has moved on.
    workflow_content TEXT NOT NULL,
    event TEXT NOT NULL CHECK (event IN ('push', 'pull_request', 'workflow_dispatch')),
    ref TEXT NOT NULL,
    sha TEXT NOT NULL,
    pull_request_id BIGINT REFERENCES pull_requests(id) ON DELETE SET NULL,
    attempt INTEGER NOT NULL DEFAULT 1,
    status TEXT NOT NULL CHECK (status IN ('waiting', 'queued', 'running', 'success', 'failure', 'cancelled')),
    no_secrets BOOLEAN NOT NULL DEFAULT FALSE,
    concurrency_group TEXT NOT NULL DEFAULT '',
    cancel_in_progress BOOLEAN NOT NULL DEFAULT FALSE,
    trigger_user_id BIGINT REFERENCES users(id) ON DELETE SET NULL,
    cancelled_by BIGINT REFERENCES users(id) ON DELETE SET NULL,
    error TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    started_at TIMESTAMPTZ,
    finished_at TIMESTAMPTZ,
    UNIQUE (repo_id, number)
);
CREATE INDEX idx_workflow_runs_repo_status ON workflow_runs(repo_id, status);
CREATE INDEX idx_workflow_runs_repo_sha ON workflow_runs(repo_id, sha);

-- A re-run adds rows with a higher attempt; the current row of a job is the highest attempt of its name.
CREATE TABLE workflow_jobs (
    id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    run_id BIGINT NOT NULL REFERENCES workflow_runs(id) ON DELETE CASCADE,
    attempt INTEGER NOT NULL DEFAULT 1,
    job_key TEXT NOT NULL,
    name TEXT NOT NULL,
    status_context TEXT NOT NULL,
    matrix JSONB NOT NULL DEFAULT '{}',
    needs JSONB NOT NULL DEFAULT '[]',
    if_expr TEXT NOT NULL DEFAULT '',
    runs_on JSONB NOT NULL DEFAULT '[]',
    container TEXT NOT NULL DEFAULT '',
    env JSONB NOT NULL DEFAULT '{}',
    artifacts JSONB NOT NULL DEFAULT '[]',
    timeout_minutes INTEGER NOT NULL,
    status TEXT NOT NULL CHECK (status IN ('waiting', 'queued', 'running', 'success', 'failure', 'cancelled', 'skipped')),
    message TEXT NOT NULL DEFAULT '',
    -- No foreign key until the runners table exists.
    runner_id BIGINT,
    lease_until TIMESTAMPTZ,
    heartbeat_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    queued_at TIMESTAMPTZ,
    started_at TIMESTAMPTZ,
    finished_at TIMESTAMPTZ,
    UNIQUE (run_id, attempt, name)
);
CREATE INDEX idx_workflow_jobs_run ON workflow_jobs(run_id);
CREATE INDEX idx_workflow_jobs_queued ON workflow_jobs(queued_at) WHERE status = 'queued';

CREATE TABLE workflow_steps (
    id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    job_id BIGINT NOT NULL REFERENCES workflow_jobs(id) ON DELETE CASCADE,
    position INTEGER NOT NULL,
    name TEXT NOT NULL,
    run TEXT NOT NULL,
    shell TEXT NOT NULL DEFAULT '',
    working_directory TEXT NOT NULL DEFAULT '',
    env JSONB NOT NULL DEFAULT '{}',
    if_expr TEXT NOT NULL DEFAULT '',
    timeout_minutes INTEGER NOT NULL DEFAULT 0,
    status TEXT NOT NULL DEFAULT 'queued' CHECK (status IN ('queued', 'running', 'success', 'failure', 'cancelled', 'skipped')),
    started_at TIMESTAMPTZ,
    finished_at TIMESTAMPTZ,
    log_first_line INTEGER NOT NULL DEFAULT 0,
    log_last_line INTEGER NOT NULL DEFAULT 0,
    UNIQUE (job_id, position)
);

ALTER TABLE commit_statuses ADD COLUMN run_id BIGINT REFERENCES workflow_runs(id) ON DELETE SET NULL;
CREATE INDEX idx_commit_statuses_run ON commit_statuses(run_id) WHERE run_id IS NOT NULL;
