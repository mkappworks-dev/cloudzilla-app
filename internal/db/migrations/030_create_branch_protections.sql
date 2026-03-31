CREATE TABLE branch_protections (
    id                    BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    repo_id               BIGINT NOT NULL REFERENCES repositories(id) ON DELETE CASCADE,
    pattern               TEXT NOT NULL,
    require_review_count  INTEGER NOT NULL DEFAULT 0,
    require_status_checks TEXT[] NOT NULL DEFAULT '{}',
    block_force_push      BOOLEAN NOT NULL DEFAULT TRUE,
    created_at            TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at            TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE(repo_id, pattern)
);
