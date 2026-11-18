# Generic packages API

Created: 2026-11-18
Category: enhancement
Status: ready-for-agent
Blocked by: 02
Parallel with: 03, 04, 10

| Branch | Worktree | PR title (into `feat/phase-16-package-registry`) | Effort |
| --- | --- | --- | --- |
| `feat/phase-16-generic-packages` | `.worktrees/feat+phase-16-generic-packages` | `feat(packages): generic file upload and download (16.2)` | M |

Spec: [spec.md](../spec.md). Parent: `../../2026-11-06-uiux-desktop-9.5/issues/19-package-registry.md`. The integration branch `feat/phase-16-package-registry` (worktree `.worktrees/feat+phase-16-package-registry`) collects every ticket; the one PR to `main` comes from it.

## Problem

A release engineer wants to publish a tarball or binary from CI with `curl -u user:token -T file` and pull it back by URL, with the same permissions as the Repository.

## Scope

- `PUT /api/repos/{owner}/{repo}/packages/generic/{name}/{version}/{filename}` with Bearer or Basic (PAT): `CanWrite` and `packages:write`; a name, version or filename outside `[A-Za-z0-9._-]`, or `.`/`..`, is `400`; a package name already linked to another Repository of the owner is `409`; streams to a temp file while hashing; `packages.max_file_bytes` and the quota (ticket 01's accessor) enforced mid-stream; the object (key ending in a random token) put before the row; re-upload of an existing file is `409`.
- `GET /{owner}/{repo}/packages/generic/{name}/{version}/{filename}` (Bearer, Basic or anonymous): `CanRead` on every request, the same `404` for every refusal, the release-asset response headers.
- JSON list and detail of generic packages and versions under `/api/repos/{owner}/{repo}/packages`, and delete of a file and a version (`CanWrite`).
- Middleware: the generic upload is a streamed upload (`IsStreamedUpload`), CSRF exempts it when Basic-authenticated, the handler lifts the server deadlines, and the rate limiter keys a Basic PAT here per user; a Basic PAT lacking the scope gets a plain `403`, as for git.

## Files (hotspots in bold)

- **`internal/router/router.go`** (shared with 03, 04, 10)
- **`internal/handler/package_handler.go`** (new)
- `internal/service/package_service.go`
- `internal/store/` (generic versions and files)
- `internal/middleware/csrf.go`, `body_limit.go`, `api_rate_limit.go`
- `docs/api-reference.md`, `docs/ci.md` (the secrets-PAT recipe)

## Acceptance criteria

- [ ] Upload, download and sha256 round trip; the response carries the stored sha256
- [ ] `curl -u user:czp_... -T` and a Bearer upload both work (no CSRF `403`); a `repo:write` token is `403`
- [ ] A bad name, `.` and `..` are `400`; the same package name under another Repository of the owner is `409`; a second upload of the same file is `409`
- [ ] A reader cannot upload; an anonymous download of a private Repository is `404`; a Repository made private stops serving at once
- [ ] `max_file_bytes` and the quota are enforced mid-stream without leaving an object or a row
- [ ] No page changes, so no light and dark check; the standing mocks are unchanged (say so in the PR)
- [ ] Verified on a throwaway server (scratch Postgres, unique `CZ_AUTH_COOKIE_NAME`, `admin@example.test` / `cloudzilla-seed`), not the shared dev DB
- [ ] `golangci-lint run`, `go test ./...` and `make test-integration` pass; Prettier is not run on `.scratch/`
- [ ] Status set to `done` and these boxes ticked in the same commit

## Verification

Throwaway server: `curl -u -T` a 100 MiB file, download with and without a token on a public and a private Repository, compare sha256.

## Comments
