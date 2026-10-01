-- Sessions are stateless JWTs; each carries the version current when it was
-- issued, and bumping the column ends every session issued before.
ALTER TABLE users ADD COLUMN IF NOT EXISTS session_version INTEGER NOT NULL DEFAULT 0;
