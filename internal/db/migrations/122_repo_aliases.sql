-- A path a repo used to have. Rows point at a repo, never at another alias, so a chain of renames stays flat.
CREATE TABLE repo_aliases (
    id          BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    repo_id     BIGINT NOT NULL REFERENCES repositories(id) ON DELETE CASCADE,
    owner_name  TEXT   NOT NULL,
    name        TEXT   NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Case-insensitive like RepoStore.NameHeld: a path is a live repo, an alias, or free.
CREATE UNIQUE INDEX idx_repo_aliases_path ON repo_aliases (lower(owner_name), lower(name));
CREATE INDEX idx_repo_aliases_repo ON repo_aliases (repo_id);
