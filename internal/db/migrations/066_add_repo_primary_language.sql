-- Cached primary language per repo, for the language-filter chips on /stars
-- and the user repositories tab.

ALTER TABLE repositories ADD COLUMN IF NOT EXISTS primary_language TEXT;
CREATE INDEX IF NOT EXISTS idx_repositories_primary_language ON repositories(primary_language);
