-- The storage key of the uploaded avatar, or '' for none. avatar_url keeps
-- what OAuth sign-up stored, for API compatibility.
ALTER TABLE users ADD COLUMN avatar_key TEXT NOT NULL DEFAULT '';
ALTER TABLE organizations ADD COLUMN avatar_key TEXT NOT NULL DEFAULT '';
