ALTER TABLE users
    ADD COLUMN IF NOT EXISTS email_notifications BOOLEAN NOT NULL DEFAULT TRUE,
    ADD COLUMN IF NOT EXISTS email_digest        TEXT NOT NULL DEFAULT 'immediate'
        CHECK(email_digest IN ('immediate','daily','weekly','never'));
