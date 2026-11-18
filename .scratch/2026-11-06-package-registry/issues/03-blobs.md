# Blobs: pending packages, uploads, pulls and mount

Created: 2026-11-18
Category: enhancement
Status: ready-for-agent
Blocked by: 02
Parallel with: 06

| Branch | Worktree | PR title (into `feat/phase-16-package-registry`) | Effort |
| --- | --- | --- | --- |
| `feat/phase-16-blobs` | `.worktrees/feat+phase-16-blobs` | `feat(registry): blob uploads, pulls and mount (16.1)` | L |

Spec: [spec.md](../spec.md). Parent: `../../2026-11-06-uiux-desktop-9.5/issues/19-package-registry.md`. The integration branch `feat/phase-16-package-registry` (worktree `.worktrees/feat+phase-16-package-registry`) collects every ticket; the one PR to `main` comes from it.

## Problem

An image is its layers, and Docker sends them before the manifest says which Repository they belong to. Docker must be able to push a 2 GiB layer in chunks, ask whether the registry already has one, and pull it back, without the server holding it in memory, filling its disk, or exposing another package's layers.

## Scope

- Pending packages (decision 9): the first upload to a new name creates one for (path owner, name, creator), invisible to others, at most `registry.max_pending_per_user`, expiring `registry.upload_ttl` after creation. Expiry is checked lazily on access; ticket 05's sweep reclaims.
- Upload sessions: `POST /v2/{name}/blobs/uploads/` (and `?digest=` single POST), `PATCH` with `Content-Range` optional and checked when sent (out of order is `416`), `GET` status, `DELETE`, final `PUT ?digest=`. Bound to user and package; at most `registry.max_sessions_per_user` open and `registry.max_inflight_bytes_per_user` in flight; quota checked at each `PATCH` and at commit through ticket 01's accessor. `Location`, `Range` and `Docker-Upload-UUID` headers.
- Spooling to a temp file in a named directory emptied at startup; `sha256` verified (other algorithms `DIGEST_INVALID`) and size checked; the object put with its exact size under the `registry/` prefix if not already stored (hex-only key), then the link row written under `FOR SHARE` on the blob row, re-putting the object if the row is gone.
- `HEAD` and `GET /v2/{name}/blobs/{digest}`: `CanRead` on every request, the same answer for unlinked and missing, `Range` by skipping bytes in the stream, never cached publicly.
- `POST ...?mount=<digest>&from=<name>` copies only a link row after `CanRead` on the source package; otherwise `202` and a normal upload.
- The blob and upload handlers lift the server's read and write deadlines; `registry.max_blob_bytes` is `SIZE_INVALID` with `413`.

## Files (hotspots in bold)

- **`internal/router/router.go`** (03, 04, 06 and 10 all touch it: expect merge conflicts on the integration branch)
- **`internal/handler/registry_handler.go`**
- **`internal/service/registry_service.go`**
- `internal/store/` (blobs, links, sessions, pending packages)
- `cmd/server/main.go` (temp directory cleanup at startup)
- `docs/storage.md` (layout, the restart cost)

## Acceptance criteria

- [ ] Monolithic and chunked pushes store the object once; the same bytes pushed to two packages give one object and two link rows
- [ ] A `PATCH` with and without `Content-Range` both work; out of order is `416` with the right `Range`
- [ ] A digest mismatch is `DIGEST_INVALID` and leaves no object or row; a wrong size is `SIZE_INVALID`
- [ ] A pending package is invisible to and cannot be continued by another user; a second user uploading to the same name gets their own pending package; the 21st pending package is refused; expiry after 24 h (injected clock) makes the session `BLOB_UPLOAD_UNKNOWN`
- [ ] A session cannot be used by another user or for another package; the 11th session and the in-flight byte cap are refused; quota is refused mid-`PATCH`
- [ ] A reader cannot upload; a stranger gets the missing-name answer for a private package's blob even when the digest is known; mount from an unreadable source falls back to a normal upload
- [ ] A 100 MiB body streams through a reader that fails if any single buffer held exceeds 1 MiB; on S3 (`TEST_S3_*`) the put carries the exact size
- [ ] No page changes, so no light and dark check; the standing mocks are unchanged (say so in the PR)
- [ ] Verified on a throwaway server (scratch Postgres, unique `CZ_AUTH_COOKIE_NAME`, `admin@example.test` / `cloudzilla-seed`), not the shared dev DB
- [ ] `golangci-lint run`, `go test ./...` and `make test-integration` pass; Prettier is not run on `.scratch/`
- [ ] Status set to `done` and these boxes ticked in the same commit

## Verification

Throwaway server: push layers with the in-test client and a chunked `curl` upload; kill the server mid-upload, restart, confirm the temp directory is empty and a retry works.

## Comments
