CREATE TABLE commit_statuses (
    id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    repo_id BIGINT NOT NULL REFERENCES repositories(id) ON DELETE CASCADE,
    sha TEXT NOT NULL,
    context TEXT NOT NULL DEFAULT 'default',
    state TEXT NOT NULL CHECK(state IN ('pending','success','failure','error')),
    target_url TEXT NOT NULL DEFAULT '',
    description TEXT NOT NULL DEFAULT '',
    creator_id BIGINT NOT NULL REFERENCES users(id),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE(repo_id, sha, context)
);
CREATE INDEX idx_commit_statuses_repo_sha ON commit_statuses(repo_id, sha);
