-- An organization webhook has org_id and no repo_id; it fires for every repository the organization owns.
ALTER TABLE webhooks ALTER COLUMN repo_id DROP NOT NULL;
ALTER TABLE webhooks ADD COLUMN org_id BIGINT REFERENCES organizations(id) ON DELETE CASCADE;
ALTER TABLE webhooks ADD CONSTRAINT webhooks_one_owner CHECK ((repo_id IS NULL) <> (org_id IS NULL));
CREATE INDEX idx_webhooks_org ON webhooks (org_id) WHERE org_id IS NOT NULL;
