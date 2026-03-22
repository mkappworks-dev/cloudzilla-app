-- Repositories full-text search
ALTER TABLE repositories ADD COLUMN IF NOT EXISTS search_vector tsvector;
UPDATE repositories SET search_vector = to_tsvector('english', coalesce(name,'') || ' ' || coalesce(description,''));
CREATE INDEX IF NOT EXISTS idx_repositories_search ON repositories USING GIN(search_vector);
CREATE OR REPLACE FUNCTION repositories_search_trigger() RETURNS trigger AS $$
BEGIN
  NEW.search_vector := to_tsvector('english', coalesce(NEW.name,'') || ' ' || coalesce(NEW.description,''));
  RETURN NEW;
END $$ LANGUAGE plpgsql;
CREATE OR REPLACE TRIGGER repositories_search_update
  BEFORE INSERT OR UPDATE ON repositories
  FOR EACH ROW EXECUTE FUNCTION repositories_search_trigger();

-- Issues full-text search
ALTER TABLE issues ADD COLUMN IF NOT EXISTS search_vector tsvector;
UPDATE issues SET search_vector = to_tsvector('english', coalesce(title,'') || ' ' || coalesce(body,''));
CREATE INDEX IF NOT EXISTS idx_issues_search ON issues USING GIN(search_vector);
CREATE OR REPLACE FUNCTION issues_search_trigger() RETURNS trigger AS $$
BEGIN
  NEW.search_vector := to_tsvector('english', coalesce(NEW.title,'') || ' ' || coalesce(NEW.body,''));
  RETURN NEW;
END $$ LANGUAGE plpgsql;
CREATE OR REPLACE TRIGGER issues_search_update
  BEFORE INSERT OR UPDATE ON issues
  FOR EACH ROW EXECUTE FUNCTION issues_search_trigger();

-- Pull requests full-text search
ALTER TABLE pull_requests ADD COLUMN IF NOT EXISTS search_vector tsvector;
UPDATE pull_requests SET search_vector = to_tsvector('english', coalesce(title,'') || ' ' || coalesce(body,''));
CREATE INDEX IF NOT EXISTS idx_pull_requests_search ON pull_requests USING GIN(search_vector);
CREATE OR REPLACE FUNCTION pull_requests_search_trigger() RETURNS trigger AS $$
BEGIN
  NEW.search_vector := to_tsvector('english', coalesce(NEW.title,'') || ' ' || coalesce(NEW.body,''));
  RETURN NEW;
END $$ LANGUAGE plpgsql;
CREATE OR REPLACE TRIGGER pull_requests_search_update
  BEFORE INSERT OR UPDATE ON pull_requests
  FOR EACH ROW EXECUTE FUNCTION pull_requests_search_trigger();

-- User username search (simple lower-case prefix)
CREATE INDEX IF NOT EXISTS idx_users_username_lower ON users(lower(username));
