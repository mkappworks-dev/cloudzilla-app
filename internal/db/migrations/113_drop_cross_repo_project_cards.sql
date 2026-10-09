-- Deleted, not turned into a note: a note would have to copy the foreign title, and showing that title is the leak.
DELETE FROM project_cards c
USING project_columns col, projects p
WHERE col.id = c.column_id
  AND p.id = col.project_id
  AND (
    (c.issue_id IS NOT NULL AND NOT EXISTS (SELECT 1 FROM issues i WHERE i.id = c.issue_id AND i.repo_id = p.repo_id))
    OR (c.pull_id IS NOT NULL AND NOT EXISTS (SELECT 1 FROM pull_requests pr WHERE pr.id = c.pull_id AND pr.repo_id = p.repo_id))
  );
