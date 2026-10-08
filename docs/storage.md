# Object storage

Files that live outside git and PostgreSQL go through `internal/storage`. Today that means avatars. Git repos, wikis and gists still live under `git.repos_root`.

## Backends

| `storage.backend` | Where objects go |
| --- | --- |
| `local` (default) | Files under `storage.local.root` (`./storage`; `/data/storage` in the Docker image) |
| `s3` | An S3 bucket on AWS, or on any S3-compatible server such as Cloudflare R2, Backblaze B2, Garage or versitygw |

```go
type Backend interface {
    Put(ctx context.Context, key string, r io.Reader, size int64) error
    Get(ctx context.Context, key string) (io.ReadCloser, error)   // ErrNotFound when missing
    Delete(ctx context.Context, key string) error                 // nil when missing
    Exists(ctx context.Context, key string) (bool, error)
}
```

### Keys

- Keys are `[a-z0-9._-]` segments joined by `/`, with no empty, `.` or `..` segment. `storage.ValidKey` checks this, and both backends refuse anything else.
- The local backend opens its root through `os.Root`, so no key or symlink can reach outside it.
- Each local `Put` writes a temp file and renames it into place.

### The S3 backend

- It uses aws-sdk-go-v2.
- With both `access_key_id` and `secret_access_key` set, those keys are used. Otherwise the SDK's default chain applies: env vars, shared files, then an EC2/ECS/IRSA role.
- Request and response checksums are sent only when an operation requires them. Since early 2025 the SDK adds CRC32 headers by default, and several S3-compatible servers reject them.
- `storage.s3.prefix` goes in front of every key, so one bucket can serve several instances.

Startup stops on a configuration error: an unknown backend, `s3` without a bucket, only one of the two keys, or an invalid prefix. An unreachable bucket does not stop startup. Each request that needs storage fails and is logged instead.

### Examples

AWS, with an instance role:

```yaml
storage:
  backend: s3
  s3:
    region: eu-west-1
    bucket: cloudzilla-prod
```

Cloudflare R2:

```yaml
storage:
  backend: s3
  s3:
    endpoint: https://<account id>.r2.cloudflarestorage.com
    region: auto
    bucket: cloudzilla
    access_key_id: <R2 token key id>
    secret_access_key: <R2 token secret>
```

A local S3 endpoint for development is `docker compose --profile s3 up versitygw`, which serves the bucket `cloudzilla` on port 7070:

```
CZ_STORAGE_BACKEND=s3 CZ_STORAGE_S3_ENDPOINT=http://localhost:7070 CZ_STORAGE_S3_BUCKET=cloudzilla \
CZ_STORAGE_S3_PATH_STYLE=true CZ_STORAGE_S3_ACCESS_KEY_ID=cloudzilla-dev \
CZ_STORAGE_S3_SECRET_ACCESS_KEY=cloudzilla-dev-secret make dev
```

The storage tests also run against a real server when these are set:

```
TEST_S3_ENDPOINT=http://localhost:7070 TEST_S3_BUCKET=cloudzilla \
TEST_S3_ACCESS_KEY_ID=cloudzilla-dev TEST_S3_SECRET_ACCESS_KEY=cloudzilla-dev-secret go test ./internal/storage/
```

## Object layout

```
avatars/user/<user id>/<sha256 of the stored bytes>.<png|jpg>
avatars/org/<org id>/<sha256 of the stored bytes>.<png|jpg>
```

- `users.avatar_key` and `organizations.avatar_key` hold the full key, or `''` for none.
- Because the hash is part of the key, an object never changes once written, and `GET /avatars/<key>` can be cached for a year.
- The owner id in the path means no two rows share an object.
- Replacing or removing an avatar, or deleting its user or org, deletes the object after the database change commits.
- Uploads and removals for one user or org run under a Postgres advisory lock (`AvatarStore.WithOwnerLock`). The same image always gives the same key, so without the lock a removal could delete an object that a concurrent re-upload had just pointed the row at again.
- If that delete fails, the failure is logged and the object stays as an orphan. Any object that no `avatar_key` points to is an orphan and is safe to delete.

## Backup and restore

`cloudzilla-cli backup` includes the local storage root in its archive, next to the database; an S3 bucket is not copied (see [Backup and restore](./deployment.md#backup-and-restore)). Back up the bucket and prefix with your provider's versioning or replication.

| Restored | Result |
| --- | --- |
| Both | Everything works. |
| Database only | Avatars whose objects are missing return 404. Each `<img data-avatar>` sits over its initials, and a capture-phase `error` listener in the layout hides a failed image, so pages show initials in their place. |
| Objects only | Objects no row points at are orphans: harmless, and safe to delete. |

## Avatars

Uploads go to `POST /settings/avatar` and `POST /orgs/{org}/settings/avatar` (org owners only), as multipart field `avatar`. The handler is in `internal/handler/avatar_handler.go`.

`avatar.Process` handles each upload:
- **Limits:** the file is capped at 2 MB. Its type comes from its bytes: PNG, JPEG, GIF or WebP; the filename and declared type are ignored.
- **Size check:** an image over 4096 px a side (so at most about 16.8 MP) is rejected from its header, before any pixel is decoded.
- **Output:** the image is centre-cropped and scaled to at most 460 × 460, then re-encoded. It becomes a PNG if any pixel is transparent, otherwise a JPEG, and all metadata is dropped.

`GET /avatars/*` serves objects through the app:
- The key must match `avatars/(user|org)/<id>/<64 hex>.(png|jpg)`, and the user or org must still point at it. That check is one primary-key read, so made-up keys never reach the backend; the route isn't rate-limited.
- `GET` and `HEAD` are both served.
- Response headers: `Cache-Control: public, max-age=31536000, immutable`, `ETag`, `nosniff` and `Content-Security-Policy: default-src 'none'; sandbox`.
- A missing object is a 404 with `no-store`.

`components.Avatar(name, …)` renders the image for a user or org name. It finds the key in a map that the page handler puts on the request context with `withAvatars` (one batched query) or `withKnownAvatars`. With no key, it falls back to initials. Git commit authors use `components.Initials`, because an author name can match an unrelated username.
