# Blobs: pending packages, uploads, pulls and mount

Created: 2026-11-18
Category: enhancement
Status: done
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

- [x] Monolithic and chunked pushes store the object once; the same bytes pushed to two packages give one object and two link rows
- [x] A `PATCH` with and without `Content-Range` both work; out of order is `416` with the right `Range`
- [x] A digest mismatch is `DIGEST_INVALID` and leaves no object or row; a wrong size is `SIZE_INVALID`
- [x] A pending package is invisible to and cannot be continued by another user; a second user uploading to the same name gets their own pending package; the 21st pending package is refused; expiry after 24 h (injected clock) makes the session `BLOB_UPLOAD_UNKNOWN`
- [x] A session cannot be used by another user or for another package; the 11th session and the in-flight byte cap are refused; quota is refused mid-`PATCH`
- [x] A reader cannot upload; a stranger gets the missing-name answer for a private package's blob even when the digest is known; mount from an unreadable source falls back to a normal upload
- [x] A 100 MiB body streams through a reader that fails if any single buffer held exceeds 1 MiB; on S3 (`TEST_S3_*`) the put carries the exact size
- [x] No page changes, so no light and dark check; the standing mocks are unchanged (say so in the PR)
- [x] Verified on a throwaway server (scratch Postgres, unique `CZ_AUTH_COOKIE_NAME`, `admin@example.test` / `cloudzilla-seed`), not the shared dev DB
- [x] `golangci-lint run`, `go test ./...` and `make test-integration` pass; Prettier is not run on `.scratch/`
- [x] Status set to `done` and these boxes ticked in the same commit

## Verification

Throwaway server: push layers with the in-test client and a chunked `curl` upload; kill the server mid-upload, restart, confirm the temp directory is empty and a retry works.

## Comments

### 2026-11-18

- New files: `registry_blob_service.go`, `registry_blob_handler.go`, and the blob/link/session queries in `RegistryStore`;
  `registry_service.go` and `registry_handler.go` only gained fields and a one-call dispatch. The router needed no edit:
  ticket 02's `/v2/*` catch-all already routes to `Registry`, and `IsStreamedUpload` already covered `/v2/`.
- `RegistryAuth` now needs `push`, not `delete`, to `DELETE` an upload session (cancelling your own upload isn't deleting
  stored content, and a pending package's token never carries `delete`).
- Blob and session caps answer `429 TOOMANYREQUESTS` (sessions, in-flight bytes, pending packages); quota is `403
  DENIED`; `registry.max_blob_bytes` is `413 SIZE_INVALID`. `SIZE_INVALID` (400) is also a `Content-Range` whose span
  doesn't match the body. A refused chunk is truncated away, so the session keeps its previous `Range`.
- The sha256 is computed at commit by re-reading the spool file, not while spooling: chunks are separate requests and the
  hash state would have to be persisted. The cost is one local read per commit.
- Linking: `FOR SHARE` on the blob row finds it and links, so the sweep (ticket 05) can't delete the object in between;
  with no row, the object is put first, then the row inserted and linked. Nothing is put when a row exists.
- A creator can `HEAD`/`GET` blobs of their own pending package (the conformance flow checks a blob before the manifest);
  anyone else gets `NAME_UNKNOWN`.
- The spool is `os.TempDir()/cloudzilla-registry-spool`, emptied by `RegistryService.ResetSpool` at startup, which also
  deletes every session row. No new config key. Documented in `docs/storage.md`.
- The S3 criterion is covered by a recording `Backend` wrapper asserting the exact size passed to `Put`; no router test
  runs against a real `TEST_S3_*` bucket. The 100 MiB test asserts no read is offered a buffer over 1 MiB.
- Verified on a throwaway server (scratch DB `cz_t03_live`, unique cookie name, seeded admin) with `curl`: a 3 MB layer
  pushed as three `PATCH` chunks and a `PUT`, pulled back identical, a `Range` read, an anonymous pull of a private name
  refused, then `kill -9` mid-upload: the spool was empty after restart, the old session `404`, the session table empty
  and a retry `PUT` accepted. A real `docker push` is not possible until manifests (ticket 04) exist.
- `make test-integration` starts its own database; `go test ./...` ran on the `cz_t03` scratch DB, as in tickets 01 and 02.
  No page changed, so there is no light and dark check and the standing mocks are unchanged.
- Left for later tickets: blob `DELETE` (04) and the sweep that reclaims expired sessions and pending packages (05).
