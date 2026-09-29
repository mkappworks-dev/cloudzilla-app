-- RepoStore.NameHeld compares owner and repo names case-insensitively, which
-- idx_repos_owner_name cannot serve.
CREATE INDEX IF NOT EXISTS idx_repos_owner_name_lower ON repositories (lower(owner_name), lower(name));
