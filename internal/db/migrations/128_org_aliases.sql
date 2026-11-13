-- A name an organization used to have. Rows point at an org, never at another alias, so a chain of renames stays flat.
CREATE TABLE org_aliases (
    id          BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    org_id      BIGINT NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    name        TEXT   NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Case-insensitive like the owner name space: a name is a user, an org, an alias, or free.
CREATE UNIQUE INDEX idx_org_aliases_name ON org_aliases (lower(name));
CREATE INDEX idx_org_aliases_org ON org_aliases (org_id);

-- Whoever takes a name wins it, by whichever path creates the account, so the claim lives next to the existing
-- owner-name triggers (080). Repo aliases under the name go too, unless they belong to a repo the claimant owns: an org
-- renamed back to a name it had keeps the redirects of repos renamed while it held that name.
CREATE OR REPLACE FUNCTION users_owner_name_free() RETURNS trigger AS $$
BEGIN
  PERFORM pg_advisory_xact_lock(hashtext('owner_name'), hashtext(NEW.username));
  IF EXISTS (SELECT 1 FROM organizations WHERE name = NEW.username) THEN
    RAISE EXCEPTION 'name "%" is taken by an organization', NEW.username
      USING ERRCODE = 'unique_violation', CONSTRAINT = 'owner_name_taken';
  END IF;
  DELETE FROM org_aliases WHERE lower(name) = lower(NEW.username);
  DELETE FROM repo_aliases WHERE lower(owner_name) = lower(NEW.username)
    AND repo_id NOT IN (SELECT id FROM repositories WHERE owner_id = NEW.id);
  RETURN NEW;
END $$ LANGUAGE plpgsql;

CREATE OR REPLACE FUNCTION organizations_owner_name_free() RETURNS trigger AS $$
BEGIN
  PERFORM pg_advisory_xact_lock(hashtext('owner_name'), hashtext(NEW.name));
  IF EXISTS (SELECT 1 FROM users WHERE username = NEW.name) THEN
    RAISE EXCEPTION 'name "%" is taken by a user', NEW.name
      USING ERRCODE = 'unique_violation', CONSTRAINT = 'owner_name_taken';
  END IF;
  DELETE FROM org_aliases WHERE lower(name) = lower(NEW.name);
  DELETE FROM repo_aliases WHERE lower(owner_name) = lower(NEW.name)
    AND repo_id NOT IN (SELECT id FROM repositories WHERE org_id = NEW.id);
  RETURN NEW;
END $$ LANGUAGE plpgsql;
