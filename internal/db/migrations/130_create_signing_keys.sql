CREATE TABLE gpg_keys (
    id          BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    user_id     BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    key_id      TEXT NOT NULL,
    fingerprint TEXT NOT NULL UNIQUE,
    public_key  TEXT NOT NULL,
    expires_at  TIMESTAMPTZ,
    revoked_at  TIMESTAMPTZ,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX idx_gpg_keys_user ON gpg_keys(user_id);
CREATE INDEX idx_gpg_keys_key_id ON gpg_keys(key_id);

CREATE TABLE gpg_subkeys (
    id          BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    gpg_key_id  BIGINT NOT NULL REFERENCES gpg_keys(id) ON DELETE CASCADE,
    key_id      TEXT NOT NULL UNIQUE,
    fingerprint TEXT NOT NULL UNIQUE,
    can_sign    BOOLEAN NOT NULL,
    expires_at  TIMESTAMPTZ,
    revoked_at  TIMESTAMPTZ
);
CREATE INDEX idx_gpg_subkeys_key ON gpg_subkeys(gpg_key_id);

CREATE TABLE gpg_key_emails (
    gpg_key_id BIGINT NOT NULL REFERENCES gpg_keys(id) ON DELETE CASCADE,
    email      TEXT NOT NULL,
    PRIMARY KEY (gpg_key_id, email)
);

CREATE TABLE ssh_signing_keys (
    id          BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    user_id     BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    title       TEXT NOT NULL,
    public_key  TEXT NOT NULL,
    fingerprint TEXT NOT NULL UNIQUE,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX idx_ssh_signing_keys_user ON ssh_signing_keys(user_id);
