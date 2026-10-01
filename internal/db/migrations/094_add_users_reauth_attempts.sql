-- Failed password and code checks, counted per user in the database so every
-- instance shares one budget; the window starts at the first attempt in it.
ALTER TABLE users ADD COLUMN IF NOT EXISTS reauth_failures INTEGER NOT NULL DEFAULT 0;
ALTER TABLE users ADD COLUMN IF NOT EXISTS reauth_window_start TIMESTAMPTZ NULL;
