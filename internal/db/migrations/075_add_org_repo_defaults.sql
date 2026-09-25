ALTER TABLE organizations
    ADD COLUMN IF NOT EXISTS default_repo_visibility TEXT NOT NULL DEFAULT 'private',
    ADD COLUMN IF NOT EXISTS default_branch_name     TEXT NOT NULL DEFAULT 'main';

ALTER TABLE organizations
    DROP CONSTRAINT IF EXISTS organizations_default_repo_visibility_check;

ALTER TABLE organizations
    ADD CONSTRAINT organizations_default_repo_visibility_check
        CHECK (default_repo_visibility IN ('public', 'private'));
