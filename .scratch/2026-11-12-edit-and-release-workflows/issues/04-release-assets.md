# Release assets: storage, API, download and detail list

Created: 2026-11-12
Category: enhancement
Status: done
Blocked by: —

| Branch | Worktree | PR title | Effort | Score gain |
| --- | --- | --- | --- | --- |
| `feat/release-assets` | `.worktrees/feat+release-assets` | `feat(releases): upload, list and download release assets` | L | Features +0.2 |

Spec: [spec.md](../spec.md#release-assets).

## Problem

A release can't carry a file, though the pages describe releases as packaging downloads.

## Scope

- Migration `122_create_release_assets.sql`: `release_assets` (id, release_id FK `ON DELETE CASCADE`, repo_id, name,
  size, sha256, storage_key, uploader_id, created_at; unique `(release_id, name)`). Model, store, `ReleaseAssetService`,
  wired into `Stores` / `Services`.
- `POST /api/repos/{owner}/{repo}/releases/{id}/assets` (multipart field `file`, one per request, streamed to the
  `Backend` while hashing) and `DELETE .../assets/{assetID}`, both `CanWrite`, draft or published. Limits
  `release.max_asset_bytes` (512 MiB) and `release.max_assets` (20) in config and `docs/configuration.md`; 413 / 422 /
  409 for size, count and duplicate name. The request body cap and CSRF handling accommodate large streamed bodies (see
  how `SubmitEditFile` and avatars handle the global cap).
- `GET /{owner}/{repo}/releases/download/{tag}/{name}`: permission on every request, draft assets for writers only,
  unreadable and unknown are the same 404; `application/octet-stream`, `Content-Disposition: attachment`, `nosniff`,
  sandbox CSP. A tag with `/` stays in one segment (`url.PathEscape`). Add the route to the setup-gating path check if
  needed.
- Detail page Assets card lists uploaded assets (name, size, date, SHA-256, download) after the source zip; writers get
  delete (via `ConfirmDialog`) and "Add asset" (single file, progress, inline error). The JSON release response gains an
  `assets` array.
- Cleanup: deleting a release or its repo deletes the objects after the DB change commits; a failed object delete is
  logged and left as an orphan. Document the layout `releases/repo/<repo id>/<release id>/<token>.bin` in
  `docs/storage.md`, the endpoints in `docs/api-reference.md`, and backup notes if affected.

## Files

- `internal/db/migrations/122_create_release_assets.sql`, `internal/model/`, `internal/store/`, `internal/service/`
- `internal/handler/release_handler.go`, `release_page_handler.go`, `internal/router/router.go`, `internal/middleware/setup.go`
- `internal/view/pages/release_detail.templ`, `internal/view/viewmodels_*.go`
- `docs/storage.md`, `docs/api-reference.md`, `docs/configuration.md`

## Acceptance criteria

- [x] A writer can upload, list, download and delete assets on a draft and a published release
- [x] Size, count and duplicate-name limits refuse with the right status and a visible message
- [x] Download enforces read access, hides draft assets from non-writers, and cannot run uploaded HTML on the origin
- [x] Deleting a release or repo removes its objects
- [x] Works on the local and S3 backends (storage tests)
- [x] Docs updated

## Mock updates

`mockups/release_detail.html`: asset rows, empty state, writer controls, upload-in-progress and error. Also
`releases.html` if the list shows an asset count. Light and dark at 1440.

## Verification

Throwaway server per the harness: upload a small and a large file, a duplicate, an over-limit file; download with
`curl` signed in and out; check a draft as a non-writer. Light and dark at 1440.

## Comments

Claude, 2026-11-12: Built as scoped, with these differences. The upload is spooled to a temp file while hashing rather
than streamed straight to the `Backend`, because S3 needs the content length before the first byte; the object is still
written before the row. `middleware.IsStreamedUpload` exempts the upload route from the global form cap and from CSRF's
form parse (a session must send `X-CSRF-Token`), and the handler lifts the server's read and write deadlines. A repo
purge deletes its objects; deleting a user or an org drops the rows but leaves the objects as documented orphans
(`docs/storage.md`). `releases.html` shows no asset count, so it is unchanged.
