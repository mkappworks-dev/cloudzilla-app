# Object storage

Files that live outside git and PostgreSQL go through `internal/storage`. Today that means avatars, markdown image
attachments and release assets. Git repos, wikis and gists still live under `git.repos_root`.

## Backends

| `storage.backend` | Where objects go                                                                                             |
| ----------------- | ------------------------------------------------------------------------------------------------------------ |
| `local` (default) | Files under `storage.local.root` (`./storage`; `/data/storage` in the Docker image)                          |
| `s3`              | An S3 bucket on AWS, or on any S3-compatible server such as Cloudflare R2, Backblaze B2, Garage or versitygw |

```go
type Backend interface {
    Put(ctx context.Context, key string, r io.Reader, size int64) error
    Get(ctx context.Context, key string) (io.ReadCloser, error)   // ErrNotFound when missing
    Delete(ctx context.Context, key string) error                 // nil when missing
    Exists(ctx context.Context, key string) (bool, error)
}
```

### Keys

- Keys are `[a-z0-9._-]` segments joined by `/`, with no empty, `.` or `..` segment. `storage.ValidKey` checks this, and
  both backends refuse anything else.
- The local backend opens its root through `os.Root`, so no key or symlink can reach outside it.
- Each local `Put` writes a temp file and renames it into place.

### The S3 backend

- It uses aws-sdk-go-v2.
- With both `access_key_id` and `secret_access_key` set, those keys are used. Otherwise the SDK's default chain applies:
  env vars, shared files, then an EC2/ECS/IRSA role.
- Request and response checksums are sent only when an operation requires them. Since early 2025 the SDK adds CRC32
  headers by default, and several S3-compatible servers reject them.
- `storage.s3.prefix` goes in front of every key, so one bucket can serve several instances.

Startup stops on a configuration error: an unknown backend, `s3` without a bucket, only one of the two keys, or an
invalid prefix. An unreachable bucket does not stop startup. Each request that needs storage fails and is logged
instead.

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

A local S3 endpoint for development is `docker compose --profile s3 up versitygw`, which serves the bucket `cloudzilla`
on port 7070:

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
attachments/repo/<repo id>/<token>.<png|jpg|gif|webp>
releases/repo/<repo id>/<release id>/<token>.bin
```

- `users.avatar_key` and `organizations.avatar_key` hold the full key, or `''` for none.
- Because the hash is part of the key, an object never changes once written, and `GET /avatars/<key>` can be cached for
  a year.
- The owner id in the path means no two rows share an object.
- Replacing or removing an avatar, or deleting its user or org, deletes the object after the database change commits.
- Uploads and removals for one user or org run under a Postgres advisory lock (`AvatarStore.WithOwnerLock`). The same
  image always gives the same key, so without the lock a removal could delete an object that a concurrent re-upload had
  just pointed the row at again.
- If that delete fails, the failure is logged and the object stays as an orphan. Any object that no `avatar_key` points
  to is an orphan and is safe to delete.

## Backup and restore

`cz-admin backup` includes the local storage root in its archive, next to the database; an S3 bucket is not copied (see
[Backup and restore](./deployment.md#backup-and-restore)). Back up the bucket and prefix with your provider's versioning
or replication.

| Restored      | Result                                                                                                                                                                                                                                         |
| ------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Both          | Everything works.                                                                                                                                                                                                                              |
| Database only | Avatars, attachments and release assets whose objects are missing return 404. Each `<img data-avatar>` sits over its initials, and a capture-phase `error` listener in the layout hides a failed image, so pages show initials in their place. |
| Objects only  | Objects no row points at are orphans: harmless, and safe to delete. For attachments, the `attachments` table holds the only link to the repo, so restore it with the database.                                                                 |

## Avatars

Uploads go to `POST /settings/avatar` and `POST /orgs/{org}/settings/avatar` (org owners only), as multipart field
`avatar`. The handler is in `internal/handler/avatar_handler.go`.

`avatar.Process` handles each upload:

- **Limits:** the file is capped at 2 MB. Its type comes from its bytes: PNG, JPEG, GIF or WebP; the filename and
  declared type are ignored.
- **Size check:** an image over 4096 px a side (so at most about 16.8 MP) is rejected from its header, before any pixel
  is decoded.
- **Output:** the image is centre-cropped and scaled to at most 460 × 460, then re-encoded. It becomes a PNG if any
  pixel is transparent, otherwise a JPEG, and all metadata is dropped.

`GET /avatars/*` serves objects through the app:

- The key must match `avatars/(user|org)/<id>/<64 hex>.(png|jpg)`, and the user or org must still point at it. That
  check is one primary-key read, so made-up keys never reach the backend; the route isn't rate-limited.
- `GET` and `HEAD` are both served.
- Response headers: `Cache-Control: public, max-age=31536000, immutable`, `ETag`, `nosniff` and
  `Content-Security-Policy: default-src 'none'; sandbox`.
- A missing object is a 404 with `no-store`.

`components.Avatar(name, …)` renders the image for a user or org name. It finds the key in a map that the page handler
puts on the request context with `withAvatars` (one batched query) or `withKnownAvatars`. With no key, it falls back to
initials. Git commit authors use `components.Initials`, because an author name can match an unrelated username.

## Markdown image attachments

Pasting or dropping an image into a markdown editor on a repo page uploads it to `POST /{owner}/{repo}/attachments`
(multipart field `file`) and inserts `![name](/attachments/<token>.<ext>)`. The editors are wired by a script in
`layout.templ`; `<body data-attachments-url>` turns it on, and `attachmentsURL` leaves it off when signed out, outside a
repo, and on the wiki. Wiki pages live in git, where the sweep below can't see what they reference. The handler is in
`internal/handler/attachment_handler.go`, the service in `internal/service/attachment_service.go`.

- **Who may upload:** anyone signed in who can read the repo, which is also who may comment. An unreadable or unknown
  repo is a 404.
- **Limits:** one image of at most 10 MB, PNG, JPEG, GIF or WebP, typed from its bytes (`attachment.Validate`). Declared
  dimensions over 16384 px a side are rejected from the header. The file is stored as uploaded, so animated GIFs survive
  and so does metadata such as EXIF GPS. Anyone who can read the repo can download the original.
- **Token:** 128 random bits. The URL carries no repo name, so renaming or transferring a repo doesn't break links in
  old comments.
- **Rows:** `attachments` holds the token, repo, uploader and storage key. `repo_id` has no foreign key, so the sweep
  can still find the rows of a repo removed by any path.

`GET` and `HEAD /attachments/<token>.<ext>` look the token up, then run `RepoService.CanRead` for the viewer on every
request. A repo made private, or soft-deleted, stops serving at once, and restoring it serves its images again. An
unknown token, a wrong extension and an unreadable repo are the same 404, so a private repo's images can't be told from
missing ones. Responses carry `Cache-Control: private, no-cache` with an `ETag` (the 304 comes after the permission
check), `Vary: Authorization, Cookie`, `nosniff` and `Content-Security-Policy: default-src 'none'; sandbox`. Unlike
avatars they can't be cached `public` or served by a CDN. `attachments` is a reserved owner name.

## Release assets

A writer uploads a file to a release with `POST /api/repos/{owner}/{repo}/releases/{id}/assets` (multipart field `file`,
one per request; see [Release assets](./api-reference.md#release-assets)). The handler is in
`internal/handler/release_asset_handler.go`, the service in `internal/service/release_asset_service.go`.

- **Limits:** `release.max_asset_bytes` (512 MiB) per file and `release.max_assets` (20) per release. There is no type
  restriction. The count is checked under a lock on the release row, so concurrent uploads can't pass it together.
- **Names:** the user's filename is cleaned to `[A-Za-z0-9._-]` and at most 100 characters, and is unique within a
  release. It never reaches the key, which ends in a 128-bit random token.
- **Streaming:** the multipart body is read as a stream, never parsed into a form, and the router lifts the global form
  cap and CSRF's form parse for this one route (`middleware.IsStreamedUpload`), so a session needs the `X-CSRF-Token`
  header. The handler also clears the server's read and write deadlines, which suit pages, not 512 MiB.
- **Spooling:** the file is copied to a temp file in the OS temp directory while its SHA-256 is computed, then put into
  the backend with its exact size, because S3 needs the length before the first byte. Free space in the temp directory
  must cover the largest upload in flight.
- **Order:** the object is written before the row, and removed again if the row fails.
- **Rows:** `release_assets` holds the name, size, SHA-256, key and uploader. `repo_id` repeats `releases.repo_id` so a
  repo purge can find the objects after the cascade has deleted the rows.
- **Download:** `GET /{owner}/{repo}/releases/download/{tag}/{name}` runs `RepoService.CanRead` on every request and
  hides a draft's assets from anyone without `CanWrite`. Every refusal is the same 404. Responses are
  `application/octet-stream` with `Content-Disposition: attachment`, `nosniff` and
  `Content-Security-Policy: default-src 'none'; sandbox`, and they are never cached publicly: nothing here may be served
  by a CDN.

Deleting an asset or a release deletes the objects after the database change commits; so does
`RepoService.PurgeExpired`. Deleting a user or an org drops its repos' rows but not their objects. If an object delete
fails, the failure is logged and the object stays as an orphan: any object under `releases/` that no
`release_assets.storage_key` names is safe to delete.

### Cleanup

- **Repo purge:** `RepoService.PurgeExpired` deletes a purged repo's objects and rows. A soft delete keeps them, so
  Restore loses nothing.
- **Sweep:** `AttachmentService.Run` runs hourly (`attachment.sweep` in `main.go`). It deletes attachments whose repo no
  longer exists, which also covers org and user deletion, and uploads older than 24 hours (`AttachmentGrace`) that no
  stored markdown contains. It looks in issue, PR and discussion bodies, comments, reviews, line comments, replies,
  releases, milestones, saved replies and project card descriptions. Removing an image from a comment, or deleting the
  comment, frees it at the next sweep after the grace period.
- **Cost accepted:** the reference check is a substring scan of those tables for each candidate, once an hour. A draft
  left unsaved for over 24 hours loses its uploads.
- A failed object delete leaves the row, so the next sweep retries it.
