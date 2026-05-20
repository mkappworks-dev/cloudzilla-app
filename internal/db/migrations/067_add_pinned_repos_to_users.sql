-- 067_add_pinned_repos_to_users.sql
-- Pinned repository IDs are stored as a BIGINT[] column on users so the
-- profile page reads them in a single query alongside the user. Ordering
-- in the array is preserved as the pin order.

ALTER TABLE users
    ADD COLUMN IF NOT EXISTS pinned_repo_ids BIGINT[] NOT NULL DEFAULT '{}';
