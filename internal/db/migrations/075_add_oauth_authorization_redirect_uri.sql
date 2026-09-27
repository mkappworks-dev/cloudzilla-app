ALTER TABLE oauth_authorizations ADD COLUMN IF NOT EXISTS redirect_uri TEXT NOT NULL DEFAULT '';
