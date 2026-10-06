# Avatar uploads on a pluggable object store (local disk or S3-compatible)

Created: 2026-10-06
Category: enhancement
Status: ready-for-agent

## Problem

Users and organizations can't set a picture. Both settings pages show a disabled "Upload new picture" button (`internal/view/pages/settings.templ`, `internal/view/pages/org_settings.templ`), and every avatar in the UI is initials.

What exists today (checked against `origin/main` at 4d1e48d7):

- `users.avatar_url` (migration 001) and `organizations.avatar_url` (migration 009) exist, but no template renders either column. The only writer is Google sign-up, `UserStore.CreateOAuthUser`, which stores Google's picture URL. `SSOStore.ProvisionSSOUser` writes `''`. The only reader outside the stores is the JSON API (`publicUser` in `internal/handler/user_handler.go`, and `model.Org`'s JSON).
- `components.Avatar(name, size, alt)` draws initials from a name string. There are 41 call sites, plus `avatarInitials` in the nav menu (`layout.templ`) and the settings profile block. 4 of the 41 are git commit authors, not accounts: `commits.templ`, `pr_commits.templ`, `commit.templ`, and `repo.templ`'s latest commit.
- No file-storage abstraction exists. The only multipart upload is the New file form, which commits into git (`internal/handler/repo_files_handler.go`). Releases have no asset storage, so avatars are the first thing the app stores outside git and Postgres.
- `middleware.MaxFormBodySize(handler.MaxNewFileBodyBytes)` (26 MB) is global and runs ahead of `CSRF`. `CSRF` parses the form only when `X-CSRF-Token` is absent, and HTMX always sends that header, so a route-level `MaxBodySize` binds HTMX uploads. The `/api/orgs` route group applies a 1 MB `apiBodyLimit`, so an org avatar route inside that group could never accept 2 MB.
- go.mod has no S3 client. The roadmap's "No new Go dependencies" rule is waived for this work (decided 2026-10-06).
- MinIO's community edition stopped publishing Docker images in October 2025, and its repo was archived on 2026-04-25, so the dev stack can't use it.

## Decisions (2026-10-06)

| Question | Decision |
| --- | --- |
| S3 client | `github.com/aws/aws-sdk-go-v2` (`config`, `credentials`, `service/s3`). It has the full credential chain (static keys, env, shared files, IAM roles/IRSA), handles custom endpoints and path-style addressing for R2, B2, Garage and versitygw, and can serve later 20.1 consumers (LFS, release assets) through the multipart upload manager. |
| Image library | `golang.org/x/image` for WebP decoding and `draw.CatmullRom` resizing. |
| Serving | App proxy with immutable cache headers. No public bucket, no presigned URLs. |
| Surfaces | Every account avatar. Commit authors stay initials, because matching git emails to accounts is a separate feature. |
| Markdown image attachments | Out of scope. Follow-up issue on the same storage layer. |
| Google profile pictures | Not rendered and not imported. `avatar_url` keeps them for API compatibility only. |

## Proposed design

### 1. Storage layer: `internal/storage`

```go
type Backend interface {
    Put(ctx context.Context, key string, r io.Reader, size int64) error
    Get(ctx context.Context, key string) (io.ReadCloser, error)
    Delete(ctx context.Context, key string) error
    Exists(ctx context.Context, key string) (bool, error)
}

var ErrNotFound = errors.New("storage: object not found")
```

- The roadmap's signature is kept, so later consumers can plug in. `Get` on a missing key returns `ErrNotFound`. `Delete` on a missing key returns nil.
- Keys are validated in one place (`storage.ValidKey`): `[a-z0-9._-]` segments joined by `/`, no empty, `.` or `..` segment, and no leading or trailing `/`. Both backends refuse an invalid key.
- `LocalBackend` stores objects under `storage.local.root` through an `os.Root`, so no key can reach outside it. `Put` writes a temp file in the target directory and renames it into place, so a reader never sees a partial object. The root directory is created at startup.
- `S3Backend` wraps `s3.Client`:
  - The config comes from `config.LoadDefaultConfig` with the configured region. When `access_key_id` and `secret_access_key` are both set they become a static provider; otherwise the SDK's default chain applies.
  - `BaseEndpoint` is set when `endpoint` is non-empty, and `UsePathStyle` follows `path_style`.
  - `RequestChecksumCalculation` and `ResponseChecksumValidation` are both `WhenRequired`. Since early 2025 the SDK sends CRC32 checksums by default, and several S3-compatible servers reject them.
  - A non-empty `prefix` is prepended to every key, so one bucket can serve several instances.
  - `NoSuchKey`, and 404 on `HeadObject`, map to `ErrNotFound`.
- `storage.New(cfg config.StorageConfig) (Backend, error)` picks the backend. An unknown backend name, or `s3` with no bucket, stops startup with a clear error. A failed connection does not stop startup: the first request fails and is logged, so a storage outage doesn't take the forge down.

### 2. Configuration

| Key | Default | Env |
| --- | --- | --- |
| `storage.backend` | `local` | `CZ_STORAGE_BACKEND` |
| `storage.local.root` | `./storage` | `CZ_STORAGE_LOCAL_ROOT` |
| `storage.s3.endpoint` | `""` (AWS) | `CZ_STORAGE_S3_ENDPOINT` |
| `storage.s3.region` | `us-east-1` | `CZ_STORAGE_S3_REGION` |
| `storage.s3.bucket` | `""` (required for `s3`) | `CZ_STORAGE_S3_BUCKET` |
| `storage.s3.access_key_id` | `""` | `CZ_STORAGE_S3_ACCESS_KEY_ID` |
| `storage.s3.secret_access_key` | `""` | `CZ_STORAGE_S3_SECRET_ACCESS_KEY` |
| `storage.s3.path_style` | `false` | `CZ_STORAGE_S3_PATH_STYLE` |
| `storage.s3.prefix` | `""` | `CZ_STORAGE_S3_PREFIX` |

Every key gets a viper default, because `AutomaticEnv` only binds nested keys that viper already knows. The Docker image and `docker-compose.yml` set `CZ_STORAGE_LOCAL_ROOT=/data/storage`, next to `/data/git-repos`. `./storage` is gitignored.

### 3. Object layout

```
avatars/user/<user id>/<sha256 of stored bytes>.<png|jpg>
avatars/org/<org id>/<sha256 of stored bytes>.<png|jpg>
```

- The hash in the name makes every URL immutable, so it can be cached for a year. The owner prefix means no two owners share an object, so a delete never needs a reference count.
- `users.avatar_key` and `organizations.avatar_key` hold the full key, or `''`. Any object an `avatar_key` doesn't point to is an orphan and safe to delete.
- Back up the local root directory, or the bucket and prefix, together with the database. Restoring the database without the objects leaves avatars returning 404, and the UI falls back to initials. Restoring the objects without the database leaves orphans only.

### 4. Data

- A migration (the next free number when it's committed) adds `avatar_key TEXT NOT NULL DEFAULT ''` to `users` and `organizations`.
- `model.User.AvatarKey` and `model.Org.AvatarKey` are scanned wherever the stores load those rows: `userColumns`/`scanUser`, and `OrgStore`'s three org scans.
- `avatar_url` is left as is. In the JSON API, `avatar_url` becomes the absolute URL of the uploaded avatar (`server.base_url + "/avatars/" + key`) when `avatar_key` is set, and the stored `avatar_url` otherwise, so existing API clients don't break.

### 5. Image processing: `internal/avatar`

`avatar.Process(r io.Reader) (Image, error)` is a pure function and returns the bytes, the extension and the SHA-256:

1. Read at most `MaxUploadBytes` (2 MB) + 1. One byte over gives `ErrTooLarge`.
2. Detect the type from the bytes (`http.DetectContentType`). Allow `image/png`, `image/jpeg`, `image/gif` and `image/webp`, and give `ErrUnsupportedType` for anything else, including SVG, HTML, PDF and polyglots. The filename and the part's Content-Type are never read.
3. `image.DecodeConfig` reads only the header. Reject a side over 4096 px, an area over 16 MP, or a side of 0 with `ErrDimensions`, before any pixel is decoded. This is what stops decompression bombs.
4. Decode. A GIF gives its first frame.
5. Centre-crop to a square, then scale down to at most 460 × 460 with `draw.CatmullRom`. A smaller image is never scaled up.
6. Encode as PNG if any pixel has alpha < 255, otherwise as JPEG at quality 90. Re-encoding drops EXIF (including GPS), ICC profiles, PNG text chunks and GIF comments.

Only the decoders above are registered in the package, so `image.Decode` can't be steered to another format.

### 6. Service: `AvatarService`

- `SetUserAvatar(ctx, userID, r)`, `RemoveUserAvatar(ctx, userID)`, `SetOrgAvatar(ctx, orgID, actorID, r)`, `RemoveOrgAvatar(ctx, orgID, actorID)`.
- Set runs in this order:
  1. `Process`.
  2. `Put` the new key.
  3. One statement swaps the key and returns the previous one, read under `FOR UPDATE`.
  4. If that statement fails, delete the new object.
  5. Delete the previous object when it is non-empty and differs from the new key. Re-uploading the same image yields the same key, so nothing is deleted.
- A failed delete of the previous object is logged and not returned. The orphan is an accepted cost.
- Remove swaps the key to `''` and deletes the previous object.
- Org methods return `ErrNotOrgOwner` unless `OrgService.IsOwner(orgID, actorID)`. `UpdateProfile` returns an unwrapped string error for this case, so the avatar methods use a sentinel the handler can map to 403.
- `UserService.DeleteUser` and `OrgService.Delete` delete the avatar object after the database delete commits.

### 7. Routes and handlers

| Method | Path | Notes |
| --- | --- | --- |
| `POST` | `/settings/avatar` | auth; multipart field `avatar` |
| `POST` | `/settings/avatar/delete` | auth |
| `POST` | `/orgs/{org}/settings/avatar` | auth; org owner; outside the `/api/orgs` group (its 1 MB limit) |
| `POST` | `/orgs/{org}/settings/avatar/delete` | auth; org owner |
| `GET` | `/avatars/*` | no auth; serves objects |

- Upload routes get `middleware.MaxBodySize(avatarBodyBytes)`, which is 2 MB + 64 KB for multipart overhead.
- A non-htmx post without `X-CSRF-Token` is parsed by `CSRF` under the global 26 MB cap before the route limit runs. `Process` still rejects the file with `ErrTooLarge`, so the response is 413; buffering up to 26 MB first is accepted.
- `ErrTooLarge` and `*http.MaxBytesError` give 413.
- `ErrUnsupportedType` and `ErrDimensions` give 422 with a message in the form's error slot, for example "Use a PNG, JPEG, GIF or WebP image." or "Images can be at most 4096 × 4096 pixels."
- A non-owner gets 403 and an unknown org gets 404.
- On success the response re-renders the avatar block. The exact markup follows the mockup.
- Org avatar changes and removals are recorded in the audit log.
- `GET /avatars/*`:
  - The key must match `^avatars/(user|org)/[0-9]+/[0-9a-f]{64}\.(png|jpg)$`; anything else gives 404.
  - The object is streamed from `Backend.Get` with `Content-Type` taken from the extension.
  - Response headers:
    - `Cache-Control: public, max-age=31536000, immutable`
    - `ETag: "<hash>"`, and a matching `If-None-Match` gives 304
    - `X-Content-Type-Options: nosniff`
    - `Content-Security-Policy: default-src 'none'; sandbox`
  - `ErrNotFound` gives a plain 404. Other backend errors give 502 and are logged.

### 8. Rendering

- One component takes an image URL and falls back to the current initials when it is empty. It renders `<img src=… alt=… width=… height=… loading="lazy" decoding="async">` with the same round, bordered, size-class styling, and keeps the `alt == ""` decorative case.
- `view.AvatarURL(key string) string` returns `"/avatars/" + key`, or `""`.
- Every account call site gets its URL from data its handler already loads. Queries that join `users` for an author name add `u.avatar_key`. Pages that list usernames (participants, collaborators, assignees) look up a `map[username]key` in one batched `IN (...)` query.
- The nav menu's avatar comes from the per-request user lookup `basePage` already does (`UserService.CodeThemes`), extended to return `avatar_key`, so pages make no extra query. `UserOrgs` carry org avatars from `model.Org`.
- Identity surfaces: nav menu, `settings.templ` profile block, `user.templ` header and org list, `org.templ` header and member lists, `org_settings.templ`, `organizations.templ`, `fragments/org_members.templ`, `search.templ` user results, `stargazers.templ`.
- Activity surfaces:
  - comments: `fragments/comment.templ`, `comment_editor.templ`, `fragments/issue_meta.templ`, `issue_detail.templ`
  - pulls: `pull_detail.templ` (author, reviewers, collaborators, participants), `pull_new.templ`, `issue_new.templ`
  - discussions: `discussions.templ`, `discussion_detail.templ`
  - other pages: `release_detail.templ`, `gists.templ`, `gist_detail.templ`, `pulse.templ`, `contributors.templ`
  - `components/sidebar.templ` (collaborators, assignees)
- Commit-author sites keep initials.

### 9. Settings UI

The disabled "Upload new picture" buttons become a working control on both settings pages. The file input sits in its own form outside the profile form, because forms can't nest. The control:
- picks a file with `accept="image/png,image/jpeg,image/gif,image/webp"` and uploads it as soon as it's chosen, with htmx `hx-encoding="multipart/form-data"`
- shows "Remove picture" only when an avatar is set
- shows errors inline
- shows the same toast as the profile form

The help text becomes "PNG, JPEG, GIF or WebP, max 2 MB." Layout (mockup picked 2026-10-06): the avatar on the left, then an "Upload new picture" button and a "Remove" button on one row, with the help text and the error slot below them. The block moves above the profile form and is swapped in place on success.

### 10. Dev stack and docs

- `docker-compose.yml` gets a `versitygw` service (`versity/versitygw`, POSIX backend, static root keys) under the `s3` profile, plus a bucket directory, so `docker compose --profile s3 up` gives a local S3 endpoint for `make dev`.
- New `docs/storage.md`: backends, config, object layout, backup and restore, and how `avatar_key` ties rows to objects. It is linked from CLAUDE.md's subsystem docs. The operator backup work needs this layout.
- `docs/configuration.md` gets the `storage.*` keys. `docs/deployment.md` gets the `/data/storage` volume plus S3 and R2 examples. `docs/api-reference.md` gets the new `avatar_url` semantics.
- `docs/ROADMAP.md` 20.1 notes that avatars are the first consumer and that the dependency rule is waived.

## Testing

- **Storage:**
  - One conformance suite (`storagetest.Run(t, Backend)`) covers put/get/exists/delete, overwrite, `ErrNotFound`, delete of a missing key, invalid keys, and prefix isolation.
  - It runs against `LocalBackend` on `t.TempDir()`, and against `S3Backend` pointed at an `httptest` fake S3 server (path-style PUT/GET/HEAD/DELETE, which checks that a SigV4 `Authorization` header is present and that no `x-amz-checksum-*` header is sent unless required).
  - With `TEST_S3_ENDPOINT`, `TEST_S3_BUCKET`, `TEST_S3_ACCESS_KEY_ID` and `TEST_S3_SECRET_ACCESS_KEY` set, the suite also runs against a real server (versitygw in CI-free local runs).
  - A traversal test shows no key escapes the local root.
- **Processing:**
  - PNG, JPEG, GIF and WebP are accepted.
  - SVG, HTML, a PNG renamed `.svg`, and a declared `image/png` part holding SVG are rejected.
  - A tiny file declaring 65535 × 65535 is rejected before decode.
  - A 2 MB + 1 byte input is rejected.
  - A JPEG with an EXIF GPS segment comes out with no APP1 segment.
  - An image with alpha becomes PNG and an opaque one becomes JPEG.
  - Output is at most 460 px and square, and small inputs aren't upscaled.
- **Service (integration, `TEST_DATABASE_DSN`):**
  - Upload stores the object and sets the key.
  - Replace deletes the old object.
  - The same image twice keeps its object.
  - Remove clears the key and the object.
  - A failed key update deletes the new object.
  - User and org deletion remove the object.
  - A non-owner can't change an org avatar.
- **Handlers/router:**
  - 413 over the cap, including on a cookie-authenticated multipart post without `X-CSRF-Token`.
  - 422 for a bad image, 401 when signed out, and 403 for a non-owner.
  - `/avatars/*` serves with the cache headers, gives 304 on `If-None-Match`, and 404 on a malformed or unknown key.
  - A page with an uploaded avatar renders an `<img>` with the right `src`.
- **Browser check** on a throwaway server: upload, replace and remove on both settings pages; the avatar appears in the nav, on the profile and next to a comment.

## Acceptance criteria

- [ ] `storage.backend: local` (default) and `storage.backend: s3` both work, and S3 works with a custom endpoint, a region and path-style addressing. Bad S3 config stops startup with a clear error.
- [ ] A user can upload, replace and remove their avatar from Settings, and an org owner can do the same from org settings. Other members get 403.
- [ ] The image type comes from the bytes. SVG and non-images are rejected, as are files over 2 MB and images over 4096 px a side or 16 MP, the last before any pixel is decoded.
- [ ] Stored avatars are re-encoded (at most 460 × 460, PNG or JPEG), with no EXIF or other metadata.
- [ ] Object keys contain the content hash. Replacing or removing an avatar deletes the old object, and deleting the user or org deletes theirs.
- [ ] `GET /avatars/<key>` serves the image with immutable cache headers, ETag/304, `nosniff` and a sandbox CSP, and gives 404 for anything else.
- [ ] Every account avatar renders the uploaded image, with initials when there is none. Commit authors stay initials.
- [ ] The JSON API's `avatar_url` returns the uploaded avatar's absolute URL when one is set.
- [ ] `docs/storage.md`, `configuration.md`, `deployment.md` and `api-reference.md` describe the storage config, object layout and backup. `docker compose --profile s3 up` starts a local S3 endpoint.
- [ ] Storage conformance tests pass against the local backend and the fake S3 server.

## Relevant files

- Config: `internal/config/config.go`, `config.yaml`, `docker-compose.yml`, `Dockerfile`, `.gitignore`
- Wiring: `cmd/server/main.go`, `internal/service/services.go`, `internal/router/router.go`
- Stores: `internal/store/user_store.go` (`userColumns`, `scanUser`), `internal/store/org_store.go`, the author-join queries behind each activity surface
- Services: `internal/service/user_service.go` (`DeleteUser`, `CodeThemes`), `internal/service/org_service.go` (`IsOwner`, `Delete`)
- Handlers: `internal/handler/page_settings_handler.go`, `internal/handler/org_handler.go`, `internal/handler/user_handler.go` (`publicUser`), `internal/handler/page_handler.go` (`basePage`)
- Middleware: `internal/middleware/body_limit.go`, `internal/middleware/csrf.go`
- Views: `internal/view/components/avatar.templ`, `internal/view/layout/layout.templ`, `internal/view/pages/settings.templ`, `internal/view/pages/org_settings.templ`, and every call site listed in §8
- Docs: `docs/configuration.md`, `docs/deployment.md`, `docs/api-reference.md`, `docs/ROADMAP.md`, new `docs/storage.md`, `CLAUDE.md`

## Out of scope

- Images pasted or dropped into issue, PR and comment markdown. That needs an attachments table, access tied to the repo's visibility, and cleanup of unused uploads. Tracked as a follow-up on this storage layer.
- Moving git repos, wikis or gists onto the backend, and the rest of roadmap 20.1 (GCS, `migrate-storage`).
- Gravatar, which would send a hash of each user's email to a third party.
- Avatars for git commit authors who have no linked account.

## Open questions

1. Should Google sign-up import the provider picture into storage once, through the import SSRF guard, rather than ignoring it?
2. Add an optional `storage.public_url` (CDN or public bucket) so pages link to it directly, taking avatar bandwidth off the app?
3. Add a `cloudzilla-cli storage sweep` that deletes objects no `avatar_key` references, to clean up orphans left by failed deletes?
4. Should the operator health endpoint report storage reachability (a `HeadBucket` or a local stat)?
