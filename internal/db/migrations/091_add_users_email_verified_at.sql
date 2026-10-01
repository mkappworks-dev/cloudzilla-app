-- No address was ever proven before this, so every existing account starts unverified.
ALTER TABLE users ADD COLUMN IF NOT EXISTS email_verified_at TIMESTAMPTZ NULL;
