# Blobs: uploads, link rows and mount

Created: 2026-11-18
Category: enhancement
Status: ready-for-agent
Blocked by: 02
Parallel with: 06

| Branch | Worktree | PR title | Effort |
| --- | --- | --- | --- |
| `feat/phase-16-blobs` | `.worktrees/feat+phase-16-blobs` | `feat(registry): blob uploads, pulls and mount (16.1)` | L |

Spec: [spec.md](../spec.md). Parent: `../../2026-11-06-uiux-desktop-9.5/issues/19-package-registry.md`. Tickets are built on the integration branch `feat/phase-16-package-registry`; the one PR for the spec comes from it.

## Problem

An image is its layers. Docker must be able to push a 2 GiB layer in chunks, ask whether the registry already has one, and pull it back, without the server holding it in memory or exposing another Repository's layers.

## Scope

- Pending package: the first upload to a new name creates a package with no Repository, visible to nobody and usable only by its creator (rule from ticket 02); blob links belong to the package, and ticket 04 links it. A pending package never linked expires with the session TTL.
- Upload sessions: `POST /v2/{name}/blobs/uploads/` (also `?digest=` single POST), `PATCH` with `Content-Range` optional and checked when sent (out of order is `416`), `GET` status, `DELETE`, and the final `PUT ?digest=` (monolithic or with the last chunk). Sessions are bound to their user and package, each user has a cap on open sessions, and in-flight bytes count against the quota. Chunks spool to a temp file in a named temp directory that startup cleans; the digest and size are verified; the object is put with its exact size, then the link row is written.
- Shared content-addressed object (one per digest) under a new `registry/` prefix; per-repository link rows decide access. `HEAD` and `GET /v2/{name}/blobs/{digest}` run `CanRead` on every request and return the same `404` for unlinked and missing; `Range` is served by skipping bytes in the stream, because `Backend` has no ranged read (cost accepted).
- `POST …?mount=<digest>&from=<name>` copies only a link row, after `CanRead` on the source and a link there; otherwise `202` and a normal upload.
- `registry.max_blob_bytes` (`SIZE_INVALID` past it, with the standard `413`) and `registry.upload_ttl` (an expired session is `BLOB_UPLOAD_UNKNOWN`); `Docker-Upload-UUID`, `Location` and `Range` headers as the spec says.
- The handlers lift the server's read and write deadlines and the streamed-upload exemption of ticket 02 applies, so a multi-GiB `PATCH` is not cut at 15 s or buffered by the form middleware.
- Blobs are never cached publicly; only `sha256` digests are accepted (`DIGEST_INVALID` otherwise).

## Files (hotspots in bold)

- **`internal/router/router.go`** (shared with 06: expect a merge conflict)
- **`internal/handler/registry_handler.go`**
- **`internal/service/registry_service.go`**
- `internal/store/` (blobs, upload sessions)
- `internal/storage/` (key helper)
- `docs/storage.md`

## Acceptance criteria

- [ ] Monolithic and chunked pushes store the object once; pushing the same bytes to two Repositories stores one object and two link rows
- [ ] A digest mismatch is `DIGEST_INVALID` and leaves no object or row; a wrong size is `SIZE_INVALID`
- [ ] A `PATCH` with and without `Content-Range` both work; a blob upload to a new name creates a pending package that no one else can read or continue, and that expires unlinked
- [ ] A session cannot be used by another user or for another package; the per-user session cap and in-flight quota refuse extra uploads
- [ ] A chunk out of order is `416` with the right `Range`; an expired session is `BLOB_UPLOAD_UNKNOWN` and its temp file is removed
- [ ] A reader cannot upload; a stranger gets `404` on a private Repository's blob even when the digest is known
- [ ] Mount succeeds only when the caller can read the source and the source is linked, otherwise it falls back to a normal upload
- [ ] A 100 MiB layer pushes with bounded memory (a counting reader fails the test if more than a fixed window is buffered) and, on S3 (`TEST_S3_*`), with the exact size
- [ ] `docs/storage.md` documents the object layout and the cost accepted (a restart loses in-flight uploads)
- [ ] No page changes, so no light and dark check is needed; the standing mocks in `mockups/` are unchanged (say so in the PR)
- [ ] Verified on a throwaway server (scratch Postgres, unique `CZ_AUTH_COOKIE_NAME`, `admin@example.test` / `cloudzilla-seed`), not the shared dev DB
- [ ] `golangci-lint run`, `go test ./...` and the touched `*_integration` tests pass; Prettier is not run on `.scratch/`
- [ ] Status set to `done` and these boxes ticked in the same commit

## Verification

Throwaway server: push a multi-layer image with the in-test client and `curl` a chunked upload; kill the server mid-upload, restart, confirm the session is gone and a retry works; check the named temp directory is clean after the startup cleanup.

## Comments
