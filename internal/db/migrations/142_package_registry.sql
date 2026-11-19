-- A package lives at {path owner}/{name}: exactly one of owner_user_id and owner_org_id names the path owner.
-- repo_id is NULL while the package is pending, between the first blob upload and the first manifest push.
CREATE TABLE IF NOT EXISTS packages (
    id                 BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    owner_user_id      BIGINT REFERENCES users(id) ON DELETE CASCADE,
    owner_org_id       BIGINT REFERENCES organizations(id) ON DELETE CASCADE,
    type               TEXT NOT NULL CHECK (type IN ('container', 'generic')),
    name               TEXT NOT NULL,
    repo_id            BIGINT REFERENCES repositories(id) ON DELETE CASCADE,
    creator_id         BIGINT REFERENCES users(id) ON DELETE SET NULL,
    pending_expires_at TIMESTAMPTZ,
    created_at         TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at         TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT packages_one_path_owner CHECK ((owner_user_id IS NULL) <> (owner_org_id IS NULL)),
    CONSTRAINT packages_pending_expires CHECK (repo_id IS NOT NULL OR pending_expires_at IS NOT NULL)
);

CREATE UNIQUE INDEX IF NOT EXISTS packages_user_linked_name
    ON packages (owner_user_id, type, name) WHERE repo_id IS NOT NULL AND owner_user_id IS NOT NULL;
CREATE UNIQUE INDEX IF NOT EXISTS packages_org_linked_name
    ON packages (owner_org_id, type, name) WHERE repo_id IS NOT NULL AND owner_org_id IS NOT NULL;
CREATE UNIQUE INDEX IF NOT EXISTS packages_user_pending_name
    ON packages (owner_user_id, type, name, creator_id) WHERE repo_id IS NULL AND owner_user_id IS NOT NULL;
CREATE UNIQUE INDEX IF NOT EXISTS packages_org_pending_name
    ON packages (owner_org_id, type, name, creator_id) WHERE repo_id IS NULL AND owner_org_id IS NOT NULL;
CREATE INDEX IF NOT EXISTS packages_repo ON packages (repo_id) WHERE repo_id IS NOT NULL;
CREATE INDEX IF NOT EXISTS packages_pending_expiry ON packages (pending_expires_at) WHERE repo_id IS NULL;
CREATE INDEX IF NOT EXISTS packages_creator_pending ON packages (creator_id) WHERE repo_id IS NULL;

-- One object per digest, shared by every package. unlinked_at is when the last link went; the sweep deletes the
-- object after the grace period, so a blob row is never removed while a link row references it.
CREATE TABLE IF NOT EXISTS registry_blobs (
    digest      TEXT PRIMARY KEY,
    size        BIGINT NOT NULL CHECK (size >= 0),
    unlinked_at TIMESTAMPTZ,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS registry_blobs_unlinked ON registry_blobs (unlinked_at) WHERE unlinked_at IS NOT NULL;

-- A link row is what lets a package read a blob; knowing a digest grants nothing.
CREATE TABLE IF NOT EXISTS package_blobs (
    package_id BIGINT NOT NULL REFERENCES packages(id) ON DELETE CASCADE,
    digest     TEXT   NOT NULL REFERENCES registry_blobs(digest),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (package_id, digest)
);
CREATE INDEX IF NOT EXISTS package_blobs_digest ON package_blobs (digest);
CREATE INDEX IF NOT EXISTS package_blobs_created ON package_blobs (created_at);

-- content holds the exact bytes pushed, because the digest is over them.
CREATE TABLE IF NOT EXISTS package_manifests (
    id              BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    package_id      BIGINT NOT NULL REFERENCES packages(id) ON DELETE CASCADE,
    digest          TEXT   NOT NULL,
    media_type      TEXT   NOT NULL,
    size            BIGINT NOT NULL,
    content         BYTEA  NOT NULL,
    subject_digest  TEXT,
    artifact_type   TEXT,
    platforms       TEXT   NOT NULL DEFAULT '',
    pull_count      BIGINT NOT NULL DEFAULT 0,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (package_id, digest)
);
CREATE INDEX IF NOT EXISTS package_manifests_subject ON package_manifests (package_id, subject_digest)
    WHERE subject_digest IS NOT NULL;

-- Which blobs and child manifests a manifest names, so deleting and the sweep need no JSON parsing.
CREATE TABLE IF NOT EXISTS package_manifest_blobs (
    manifest_id BIGINT NOT NULL REFERENCES package_manifests(id) ON DELETE CASCADE,
    digest      TEXT   NOT NULL,
    PRIMARY KEY (manifest_id, digest)
);
CREATE INDEX IF NOT EXISTS package_manifest_blobs_digest ON package_manifest_blobs (digest);

CREATE TABLE IF NOT EXISTS package_manifest_children (
    manifest_id  BIGINT NOT NULL REFERENCES package_manifests(id) ON DELETE CASCADE,
    child_digest TEXT   NOT NULL,
    PRIMARY KEY (manifest_id, child_digest)
);
CREATE INDEX IF NOT EXISTS package_manifest_children_digest ON package_manifest_children (child_digest);

-- Deleting a manifest deletes its tags; deleting a tag leaves the manifest untagged.
CREATE TABLE IF NOT EXISTS package_tags (
    id          BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    package_id  BIGINT NOT NULL REFERENCES packages(id) ON DELETE CASCADE,
    name        TEXT   NOT NULL,
    manifest_id BIGINT NOT NULL REFERENCES package_manifests(id) ON DELETE CASCADE,
    pusher_id   BIGINT REFERENCES users(id) ON DELETE SET NULL,
    pull_count  BIGINT NOT NULL DEFAULT 0,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (package_id, name)
);
CREATE INDEX IF NOT EXISTS package_tags_manifest ON package_tags (manifest_id);

-- temp_path is a local spool file, so a session does not survive a restart.
CREATE TABLE IF NOT EXISTS package_upload_sessions (
    id         UUID PRIMARY KEY,
    package_id BIGINT NOT NULL REFERENCES packages(id) ON DELETE CASCADE,
    user_id    BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    bytes      BIGINT NOT NULL DEFAULT 0 CHECK (bytes >= 0),
    temp_path  TEXT   NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS package_upload_sessions_user ON package_upload_sessions (user_id);
CREATE INDEX IF NOT EXISTS package_upload_sessions_package ON package_upload_sessions (package_id);

CREATE TABLE IF NOT EXISTS package_versions (
    id         BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    package_id BIGINT NOT NULL REFERENCES packages(id) ON DELETE CASCADE,
    version    TEXT   NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (package_id, version)
);

CREATE TABLE IF NOT EXISTS package_files (
    id          BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    version_id  BIGINT NOT NULL REFERENCES package_versions(id) ON DELETE CASCADE,
    filename    TEXT   NOT NULL,
    size        BIGINT NOT NULL CHECK (size >= 0),
    sha256      TEXT   NOT NULL,
    storage_key TEXT   NOT NULL,
    uploader_id BIGINT REFERENCES users(id) ON DELETE SET NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (version_id, filename)
);
