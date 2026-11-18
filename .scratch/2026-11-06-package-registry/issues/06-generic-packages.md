# Generic packages API

Created: 2026-11-18
Category: enhancement
Status: ready-for-agent
Blocked by: 02
Parallel with: 03, 04

| Branch | Worktree | PR title | Effort |
| --- | --- | --- | --- |
| `feat/phase-16-generic-packages` | `.worktrees/feat+phase-16-generic-packages` | `feat(packages): generic file upload and download (16.2)` | M |

Spec: [spec.md](../spec.md). Parent: `../../2026-11-06-uiux-desktop-9.5/issues/19-package-registry.md`. Tickets are built on the integration branch `feat/phase-16-package-registry`; the one PR for the spec comes from it.

## Problem

A release engineer wants to publish a tarball or binary from CI with `curl -T` and pull it back by URL, with the same permissions as the Repository.

## Scope

- `PUT /api/repos/{owner}/{repo}/packages/generic/{name}/{version}/{filename}` (needs `CanWrite` and `packages:write`; under `/api/repos` so a target-restricted token is held to its Repository, and `packages` is already in the scope allow-list from ticket 01), with Bearer or Basic (PAT) auth, streams the body to a temp file while hashing, enforces `packages.max_file_bytes`, puts the object, then writes the row. Re-uploading an existing file is `409`.
- `GET /{owner}/{repo}/packages/generic/{name}/{version}/{filename}` (Bearer, Basic or anonymous) runs `CanRead` on every request, with the same `404` for every refusal and the release-asset response headers (`attachment`, `nosniff`, CSP, no public caching).
- JSON list and detail of packages and versions under `/api/repos/{owner}/{repo}/packages`, and delete of a file, a version and a package (version needs `CanWrite`, whole package `CanManage`).
- Names are cleaned to `[A-Za-z0-9._-]`; they never reach the storage key (which ends in a random token).
- A Basic PAT lacking the scope gets a plain `403`, as for git. The upload lifts the server deadlines and is a streamed upload for the form and CSRF middleware (ticket 02's exemption extended to this path), so `curl -u user:czp_… -T file` works.

## Files (hotspots in bold)

- **`internal/router/router.go`** (shared with 03: expect a merge conflict)
- **`internal/handler/package_handler.go`** (new)
- `internal/middleware/csrf.go`, `body_limit.go`
- `internal/service/package_service.go`
- `internal/store/`
- `internal/router/router.go`
- `internal/middleware/scope.go`
- `docs/api-reference.md`

## Acceptance criteria

- [ ] Upload, download and sha256 round trip; the response carries the stored sha256
- [ ] A second upload of the same file is `409`; a version cannot be changed except by deleting its file
- [ ] `curl -u user:czp_… -T` and a Bearer upload both work (no CSRF `403`); a target-restricted token is refused for another Repository
- [ ] A reader cannot upload; an anonymous download of a private Repository is `404`; a Repository made private stops serving at once
- [ ] `max_file_bytes` is enforced mid-stream without leaving an object
- [ ] Version delete needs `CanWrite`, whole-package delete `CanManage`
- [ ] `docs/api-reference.md` documents the endpoints and a `curl` recipe; `docs/ci.md` gets the secrets-PAT recipe for CI
- [ ] No page changes, so no light and dark check is needed; the standing mocks in `mockups/` are unchanged (say so in the PR)
- [ ] Verified on a throwaway server (scratch Postgres, unique `CZ_AUTH_COOKIE_NAME`, `admin@example.test` / `cloudzilla-seed`), not the shared dev DB
- [ ] `golangci-lint run`, `go test ./...` and the touched `*_integration` tests pass; Prettier is not run on `.scratch/`
- [ ] Status set to `done` and these boxes ticked in the same commit

## Verification

Throwaway server: `curl -T` a 100 MiB file with a PAT, download with and without a token on a public and a private Repository, compare sha256.

## Comments
