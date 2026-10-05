-- No CHECK constraint: the theme catalog lives in internal/highlight and
-- unknown values fall back to the defaults when read.
ALTER TABLE users
  ADD COLUMN code_theme_light TEXT NOT NULL DEFAULT 'github',
  ADD COLUMN code_theme_dark  TEXT NOT NULL DEFAULT 'github-dark';
