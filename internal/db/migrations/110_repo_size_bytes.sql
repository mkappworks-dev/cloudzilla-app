-- On-disk size of the repo's git and wiki dirs, kept for quotas. NULL means not measured yet and counts as 0.
ALTER TABLE repositories ADD COLUMN IF NOT EXISTS size_bytes BIGINT CHECK (size_bytes >= 0);
