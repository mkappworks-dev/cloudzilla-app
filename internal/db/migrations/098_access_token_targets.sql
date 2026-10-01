-- The repositories ("owner/repo") and organizations ("org") a repo:admin token
-- is limited to, comma-separated; empty for other tokens.
ALTER TABLE access_tokens ADD COLUMN IF NOT EXISTS targets TEXT NOT NULL DEFAULT '';
