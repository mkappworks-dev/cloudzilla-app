ALTER TABLE repositories ADD COLUMN owner_name TEXT NOT NULL DEFAULT '';
ALTER TABLE repositories ADD COLUMN org_id INTEGER REFERENCES organizations(id) ON DELETE CASCADE;

UPDATE repositories
SET owner_name = (SELECT username FROM users WHERE users.id = repositories.owner_id);

CREATE INDEX IF NOT EXISTS idx_repos_owner_name ON repositories(owner_name);
CREATE INDEX IF NOT EXISTS idx_repos_org ON repositories(org_id);
