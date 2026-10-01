-- An 'owner' row granted read only (CanRead counts any row), so 'reader' keeps
-- that access. A current owner's row is dropped instead: it adds nothing now
-- and would outlive a transfer as a read grant.
DELETE FROM permissions p
 USING repositories r
 WHERE p.repo_id = r.id
   AND p.role = 'owner'
   AND (r.owner_id = p.user_id
        OR EXISTS (SELECT 1 FROM org_members om
                    WHERE om.org_id = r.org_id AND om.user_id = p.user_id AND om.role = 'owner'));

UPDATE permissions SET role = 'reader' WHERE role = 'owner';

ALTER TABLE permissions DROP CONSTRAINT IF EXISTS permissions_role_check;
ALTER TABLE permissions ADD CONSTRAINT permissions_role_check
  CHECK (role IN ('admin','writer','reader'));
