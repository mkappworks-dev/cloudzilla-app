ALTER TABLE gpg_keys ADD COLUMN removed_at TIMESTAMPTZ;
ALTER TABLE gpg_subkeys ADD COLUMN removed_at TIMESTAMPTZ;
ALTER TABLE ssh_signing_keys ADD COLUMN removed_at TIMESTAMPTZ;
ALTER TABLE ssh_signing_keys ADD COLUMN revoked_at TIMESTAMPTZ;
