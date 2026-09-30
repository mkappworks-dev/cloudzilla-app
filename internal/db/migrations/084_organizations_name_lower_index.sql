-- The owner-name guard (ownerNameTakenCond) looks orgs up by lower(name).
CREATE INDEX IF NOT EXISTS idx_organizations_name_lower ON organizations (lower(name));
