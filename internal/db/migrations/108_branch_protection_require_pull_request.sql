ALTER TABLE branch_protections
    ADD COLUMN require_pull_request BOOLEAN NOT NULL DEFAULT FALSE;
