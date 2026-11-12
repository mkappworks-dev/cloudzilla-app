-- Extra addresses credit commits only: sign-in, notifications and password reset use users.email.
-- Only a SHA-256 of each link token is stored, as in email_verification_tokens.
CREATE TABLE IF NOT EXISTS user_emails (
    id          BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    user_id     BIGINT      NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    email       TEXT        NOT NULL,
    verified_at TIMESTAMPTZ,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE UNIQUE INDEX IF NOT EXISTS user_emails_email_lower_key ON user_emails (lower(email));
CREATE INDEX IF NOT EXISTS idx_user_emails_user ON user_emails (user_id, created_at);

CREATE TABLE IF NOT EXISTS user_email_tokens (
    id            BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    user_email_id BIGINT      NOT NULL REFERENCES user_emails(id) ON DELETE CASCADE,
    token_hash    TEXT        NOT NULL UNIQUE,
    expires_at    TIMESTAMPTZ NOT NULL,
    used_at       TIMESTAMPTZ,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_user_email_tokens_email ON user_email_tokens (user_email_id);

-- users.email and user_emails.email share one name space. Both triggers take the same advisory lock so a
-- concurrent insert on each side waits instead of missing the other's uncommitted row. An unverified claim
-- older than the link's 24 hours is dropped first, so nobody can hold an address they never proved.
CREATE OR REPLACE FUNCTION email_claim_free(addr TEXT, other_table TEXT) RETURNS void AS $$
BEGIN
  PERFORM pg_advisory_xact_lock(hashtext('email_claim'), hashtext(lower(addr)));
  DELETE FROM user_emails
   WHERE lower(email) = lower(addr) AND verified_at IS NULL AND created_at < NOW() - INTERVAL '24 hours';
  IF other_table = 'user_emails' AND EXISTS (SELECT 1 FROM user_emails WHERE lower(email) = lower(addr)) THEN
    RAISE EXCEPTION 'email "%" is an additional address', addr
      USING ERRCODE = 'unique_violation', CONSTRAINT = 'users_email_lower_key';
  END IF;
  IF other_table = 'users' AND EXISTS (SELECT 1 FROM users WHERE lower(email) = lower(addr)) THEN
    RAISE EXCEPTION 'email "%" is an account address', addr
      USING ERRCODE = 'unique_violation', CONSTRAINT = 'users_email_lower_key';
  END IF;
END $$ LANGUAGE plpgsql;

CREATE OR REPLACE FUNCTION users_email_not_additional() RETURNS trigger AS $$
BEGIN
  PERFORM email_claim_free(NEW.email, 'user_emails');
  RETURN NEW;
END $$ LANGUAGE plpgsql;
CREATE OR REPLACE TRIGGER users_email_not_additional
  BEFORE INSERT OR UPDATE OF email ON users
  FOR EACH ROW EXECUTE FUNCTION users_email_not_additional();

CREATE OR REPLACE FUNCTION user_emails_not_primary() RETURNS trigger AS $$
BEGIN
  PERFORM email_claim_free(NEW.email, 'users');
  RETURN NEW;
END $$ LANGUAGE plpgsql;
CREATE OR REPLACE TRIGGER user_emails_not_primary
  BEFORE INSERT OR UPDATE OF email ON user_emails
  FOR EACH ROW EXECUTE FUNCTION user_emails_not_primary();
