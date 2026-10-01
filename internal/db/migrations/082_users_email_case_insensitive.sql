-- Emails are unique regardless of case. An instance that already has emails
-- differing only by case must change or merge those accounts first; the error
-- names user IDs so the addresses stay out of logs.
DO $$
DECLARE
    dup_ids TEXT;
BEGIN
    SELECT string_agg(ids, '; ')
      INTO dup_ids
      FROM (SELECT string_agg(id::TEXT, ', ' ORDER BY id) AS ids
              FROM users
             GROUP BY lower(email)
            HAVING COUNT(*) > 1) dups;
    IF dup_ids IS NOT NULL THEN
        RAISE EXCEPTION 'users have emails that differ only by case (user IDs: %); change or merge those accounts, then restart', dup_ids;
    END IF;
END $$;

CREATE UNIQUE INDEX IF NOT EXISTS users_email_lower_key ON users (lower(email));
