-- Emailed confirmation codes are capped per hour as well as per minute.
ALTER TABLE users ADD COLUMN IF NOT EXISTS reauth_codes_sent INTEGER NOT NULL DEFAULT 0;
ALTER TABLE users ADD COLUMN IF NOT EXISTS reauth_codes_window_start TIMESTAMPTZ NULL;

-- A state can also start a fresh sign-in with the account's provider, which
-- confirms a sensitive action for an account with no password or 2FA.
ALTER TABLE oauth_states DROP CONSTRAINT IF EXISTS oauth_states_purpose_check;
ALTER TABLE oauth_states ADD CONSTRAINT oauth_states_purpose_check CHECK (purpose IN ('link', 'reauth'));
