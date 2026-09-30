-- An org repo belongs to its org: owner_id is NULL, and created_by records
-- who created a repo without granting access.
DO $$
BEGIN
  IF NOT EXISTS (SELECT 1 FROM information_schema.columns
                 WHERE table_schema = current_schema() AND table_name = 'repositories' AND column_name = 'created_by') THEN
    ALTER TABLE repositories ADD COLUMN created_by BIGINT REFERENCES users(id) ON DELETE SET NULL;
    UPDATE repositories SET created_by = owner_id;
  END IF;
END $$;

ALTER TABLE repositories ALTER COLUMN owner_id DROP NOT NULL;
UPDATE repositories SET owner_id = NULL WHERE org_id IS NOT NULL AND owner_id IS NOT NULL;

ALTER TABLE repositories DROP CONSTRAINT IF EXISTS repositories_owner_or_org;
ALTER TABLE repositories ADD CONSTRAINT repositories_owner_or_org
  CHECK ((owner_id IS NULL) <> (org_id IS NULL));

-- Before claimRepo, a second org owner could create a live name that already
-- existed; which of those rows holds the data only a person can tell. The
-- error names repo IDs.
DO $$
DECLARE
  dup_ids TEXT;
BEGIN
  SELECT string_agg(ids, '; ')
    INTO dup_ids
    FROM (SELECT string_agg(id::TEXT, ', ' ORDER BY id) AS ids
            FROM repositories
           WHERE org_id IS NOT NULL AND deleted_at IS NULL
           GROUP BY org_id, name
          HAVING COUNT(*) > 1) dups;
  IF dup_ids IS NOT NULL THEN
    RAISE EXCEPTION 'organization repositories share a name (repo IDs: %); rename or delete all but one of each, then restart', dup_ids;
  END IF;
END $$;

-- UNIQUE (owner_id, name) skips NULL owners. Soft-deleted org repos are left
-- out so deleting frees the name; Restore refuses one taken since.
CREATE UNIQUE INDEX IF NOT EXISTS idx_repos_org_name_live
  ON repositories (org_id, name) WHERE org_id IS NOT NULL AND deleted_at IS NULL;
