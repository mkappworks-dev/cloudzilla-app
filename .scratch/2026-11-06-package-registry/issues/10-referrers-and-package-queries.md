# Referrers, platforms and the package read and admin API

Created: 2026-11-18
Category: enhancement
Status: ready-for-agent
Blocked by: 04
Parallel with: 05, 06

| Branch | Worktree | PR title (into `feat/phase-16-package-registry`) | Effort |
| --- | --- | --- | --- |
| `feat/phase-16-referrers-and-package-queries` | `.worktrees/feat+phase-16-referrers-and-package-queries` | `feat(registry): referrers, package queries, delete and re-link (16.1)` | M |

Spec: [spec.md](../spec.md). Parent: `../../2026-11-06-uiux-desktop-9.5/issues/19-package-registry.md`. The integration branch `feat/phase-16-package-registry` (worktree `.worktrees/feat+phase-16-package-registry`) collects every ticket; the one PR to `main` comes from it.

## Problem

Signing tools need the referrers API, and the Packages pages need queries and actions the wire protocol does not give: package lists, tag tables with sizes and platforms, whole-package delete and re-link. Ticket 04 is already large, so these sit here.

## Scope

- `GET /v2/{name}/referrers/{digest}` from the database: an OCI index of manifests whose `subject` is the digest, never `404` for an unknown subject (empty index), `artifactType` filter with `OCI-Filters-Applied`, the index content type; `OCI-Subject` returned on a manifest push with a `subject`.
- Platform summary of an index stored at push and returned with the manifest.
- `PackageService` read methods for the UI and JSON: packages of a Repository and of a path owner (readable only), tags with digest, size, publisher and pulls, untagged manifests without referrers, generic versions and files.
- `PackageService.Delete` (whole package, `CanManage` or path owner) and `Relink` (path owner or manager, to a Repository of the path owner only), with JSON endpoints under `/api/repos/{owner}/{repo}/packages`.
- `CodeService` method returning a Repository's rendered default-branch README with relative links and images resolved against the code tab, extracted from what the repo page does today.

## Files (hotspots in bold)

- **`internal/router/router.go`** (shared with 03, 04, 06)
- `internal/handler/registry_handler.go`, `package_handler.go`
- **`internal/service/package_service.go`**, `registry_service.go`
- `internal/service/code_service_tree.go`, `internal/handler/repo_page_handler.go`
- `docs/api-reference.md`

## Acceptance criteria

- [ ] A cosign-style signature manifest with `subject` appears under its subject; an unknown subject returns an empty index; the filter works and is announced
- [ ] Tagged referrer manifests stay in `tags/list`; untagged ones are absent from the untagged list query
- [ ] The list queries return only readable packages; a pending package is never listed
- [ ] Whole-package delete: a writer is refused, a manager and the path owner succeed; afterwards its links are gone and the sweep can reclaim the blobs
- [ ] Re-link to a Repository of another owner is refused, to one of the path owner's is accepted, and after a transfer the path owner (not a manager of the new owner) can re-link
- [ ] The README method resolves a relative image and link the same way the code tab does
- [ ] No page changes, so no light and dark check; the standing mocks are unchanged (say so in the PR)
- [ ] Verified on a throwaway server (scratch Postgres, unique `CZ_AUTH_COOKIE_NAME`, `admin@example.test` / `cloudzilla-seed`), not the shared dev DB
- [ ] `golangci-lint run`, `go test ./...` and `make test-integration` pass; Prettier is not run on `.scratch/`
- [ ] Status set to `done` and these boxes ticked in the same commit

## Verification

Throwaway server: `cosign sign` (or `oras attach`) an image if available, otherwise push a subject manifest with the in-test client; delete and re-link through the JSON API after a transfer.

## Comments
