-- One emailed code at a time, for accounts with no password or 2FA to confirm
-- sensitive actions with. Only its bcrypt hash is kept.
ALTER TABLE users ADD COLUMN IF NOT EXISTS reauth_code_hash TEXT NOT NULL DEFAULT '';
ALTER TABLE users ADD COLUMN IF NOT EXISTS reauth_code_expires_at TIMESTAMPTZ NULL;
ALTER TABLE users ADD COLUMN IF NOT EXISTS reauth_code_sent_at TIMESTAMPTZ NULL;
