CREATE TABLE repo_dependencies (
    id          BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    repo_id     BIGINT NOT NULL REFERENCES repositories(id) ON DELETE CASCADE,
    package_mgr TEXT NOT NULL,
    package     TEXT NOT NULL,
    version     TEXT NOT NULL DEFAULT '',
    is_dev      BOOLEAN NOT NULL DEFAULT FALSE,
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE(repo_id, package_mgr, package)
);
CREATE INDEX idx_repo_deps_repo ON repo_dependencies(repo_id);
