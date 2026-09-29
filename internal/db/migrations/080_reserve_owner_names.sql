-- Users and organizations share /{owner} and <repos_root>/<owner>/, so a name
-- may belong to only one of them. The advisory lock makes a concurrent user
-- and org insert of one name wait for each other; a plain EXISTS check would
-- miss the other's uncommitted row.
CREATE OR REPLACE FUNCTION users_owner_name_free() RETURNS trigger AS $$
BEGIN
  PERFORM pg_advisory_xact_lock(hashtext('owner_name'), hashtext(NEW.username));
  IF EXISTS (SELECT 1 FROM organizations WHERE name = NEW.username) THEN
    RAISE EXCEPTION 'name "%" is taken by an organization', NEW.username
      USING ERRCODE = 'unique_violation', CONSTRAINT = 'owner_name_taken';
  END IF;
  RETURN NEW;
END $$ LANGUAGE plpgsql;
CREATE OR REPLACE TRIGGER users_owner_name_free
  BEFORE INSERT OR UPDATE OF username ON users
  FOR EACH ROW EXECUTE FUNCTION users_owner_name_free();

CREATE OR REPLACE FUNCTION organizations_owner_name_free() RETURNS trigger AS $$
BEGIN
  PERFORM pg_advisory_xact_lock(hashtext('owner_name'), hashtext(NEW.name));
  IF EXISTS (SELECT 1 FROM users WHERE username = NEW.name) THEN
    RAISE EXCEPTION 'name "%" is taken by a user', NEW.name
      USING ERRCODE = 'unique_violation', CONSTRAINT = 'owner_name_taken';
  END IF;
  RETURN NEW;
END $$ LANGUAGE plpgsql;
CREATE OR REPLACE TRIGGER organizations_owner_name_free
  BEFORE INSERT OR UPDATE OF name ON organizations
  FOR EACH ROW EXECUTE FUNCTION organizations_owner_name_free();
