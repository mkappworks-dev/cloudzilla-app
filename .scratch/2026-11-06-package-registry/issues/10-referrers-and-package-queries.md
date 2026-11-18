# Referrers, platforms and the package read and admin API

Created: 2026-11-18
Category: enhancement
Status: done
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

- [x] A cosign-style signature manifest with `subject` appears under its subject; an unknown subject returns an empty index; the filter works and is announced
- [x] Tagged referrer manifests stay in `tags/list`; untagged ones are absent from the untagged list query
- [x] The list queries return only readable packages; a pending package is never listed
- [x] Whole-package delete: a writer is refused, a manager and the path owner succeed; afterwards its links are gone and the sweep can reclaim the blobs
- [x] Re-link to a Repository of another owner is refused, to one of the path owner's is accepted, and after a transfer the path owner (not a manager of the new owner) can re-link
- [x] The README method resolves a relative image and link the same way the code tab does
- [x] No page changes, so no light and dark check; the standing mocks are unchanged (say so in the PR)
- [ ] Verified on a throwaway server (scratch Postgres, unique `CZ_AUTH_COOKIE_NAME`, `admin@example.test` / `cloudzilla-seed`), not the shared dev DB
- [x] `golangci-lint run`, `go test ./...` and `make test-integration` pass; Prettier is not run on `.scratch/`
- [x] Status set to `done` and these boxes ticked in the same commit

## Verification

Throwaway server: `cosign sign` (or `oras attach`) an image if available, otherwise push a subject manifest with the in-test client; delete and re-link through the JSON API after a transfer.

## Comments

### 2026-11-18

- Referrers: `GET /v2/{name}/referrers/{digest}` (`GET`/`HEAD`) answers an OCI index from `package_manifests.subject_digest`, `200` with an empty `manifests` array for an unknown subject, oldest first, `artifactType` filter with `OCI-Filters-Applied: artifactType`, `Cache-Control: private, no-cache`. A private or unknown name is refused by `RegistryAuth` exactly as for any other operation. `RegistryService.PutManifest` now returns `(digest, subject, err)` so the handler can send `OCI-Subject`; it had no other callers.
- Platform summary: an index's children's `os/arch[/variant]` (no `unknown/unknown` attestation entries, de-duplicated, in index order) is stored in `package_manifests.platforms` and returned as `Manifest.Platforms` by `GetManifest` and as `platforms` on the tag and untagged JSON. An image manifest has none: reading its config blob for the platform would cost a read per push and the spec only asks for indexes.
- `PackageService` (new `package_query_service.go`, wired with `WithAccess(repos, orgs)`): `ListByRepo`, `ListByOwner` (readable only, a pending package is never listed, soft-deleted repositories skipped), `ContainerDetail` (tags with digest, size, publisher, pulls; untagged list leaves out any manifest with a `subject`), `Delete`, `Relink`. Generic versions and files keep their existing methods. Tag and untagged size is the distinct blob bytes of the image, plus every child's for an index.
- `Delete`: `CanManage` on the linked Repository or path ownership (user, org owner, or a superadmin in admin mode), so the path owner still can after a transfer. It removes rows and the objects of generic files; container blobs are left for ticket 05's sweep, which sets `unlinked_at` for blobs whose links went with the cascade.
- `Relink`: the target must belong to the path owner (`ErrRelinkTarget`). The path owner may always; anyone else needs `CanManage` on both the current and the target Repository, so after a transfer the new owner's manager is refused (`ErrPackageForbidden`) and the path owner is not.
- JSON: `GET /api/repos/{o}/{r}/packages` now lists containers too (`tag_count` added; existing fields unchanged), `GET`/`DELETE .../packages/container/{name...}`, `DELETE .../packages/generic/{name}`, `POST .../packages/relink` with `{type, name, repo: "owner/repo"}`. They sit under the package's current Repository, so a path owner who cannot read that Repository (a private one transferred away) can only use the service methods, which the UI tickets can expose on an owner route. The typed-name confirmation is a UI rule, not enforced by the JSON endpoint.
- README: `CodeService.RepoReadme` is the lookup loop extracted from the repo page, plus a link resolver (`markdown.RenderRepoFile`): a relative link goes to `/{owner}/{repo}/blob/{branch}/...`, an image to `.../raw/{branch}/...`, a leading `/` is the repository root, `..` stops at the root, and schemes, `//host` and `#fragment` are untouched. The code tab did not resolve relative links at all before, so the repo page itself now resolves them (a small fix, not a layout change); no standing mock changes.
- Docs: `docs/api-reference.md` only; there is no `docs/package-registry.md` yet (ticket 09 owns the final shape).
- Not done: `make test-integration` (it starts its own database; `go test ./...` ran on the `cz_t10` scratch DB instead, as in earlier tickets) and the throwaway-server check with a real signing tool: `cosign`/`oras` are not installed, so the referrers flow is covered by the in-test OCI client only.
