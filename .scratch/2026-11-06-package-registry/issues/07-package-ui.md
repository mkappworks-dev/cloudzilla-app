# Repository Packages tab and package detail

Created: 2026-11-18
Category: enhancement
Status: done
Blocked by: 05, 06, 10
Parallel with: none

| Branch | Worktree | PR title (into `feat/phase-16-package-registry`) | Effort |
| --- | --- | --- | --- |
| `feat/phase-16-package-ui` | `.worktrees/feat+phase-16-package-ui` | `feat(ui): repository packages tab and package detail (16.1)` | L |

Spec: [spec.md](../spec.md). Parent: `../../2026-11-06-uiux-desktop-9.5/issues/19-package-registry.md`. The integration branch `feat/phase-16-package-registry` (worktree `.worktrees/feat+phase-16-package-registry`) collects every ticket; the one PR to `main` comes from it.

## Problem

Daisy has pushed an image; she needs to see it on the Repository, copy the pull command, see its tags and digests, and remove a tag or the whole package.

## Scope

- Packages tab after Releases in the repo subnav (the `.templ` subnav and `mockups/shell.js` `REPO_TABS`, with its icon), at `/{owner}/{repo}/packages`: type tabs, name filter, type icon, badge, latest version, downloads and updated columns, the empty state with docker and curl how-tos and the token scopes, and the Publish popover.
- Package detail at `/{owner}/{repo}/packages/{type}/{name}`: copyable `docker pull` / `curl -LO` command, tags table (tag, digest, size, publisher, pulls, delete), untagged manifests (referrers left out), files and versions for generic, the README card (ticket 10's method), and the aside (repository, owner-mismatch note after a transfer, platforms, storage against the package quota, delete package, re-link).
- Delete a tag or version (`CanWrite`), delete the package (`CanManage` or path owner, typed name), re-link (path owner or manager, own Repositories only); a quota refusal shows its message. Form `POST`s go in `package_handler.go`, `Page*` methods in `package_page_handler.go`, using ticket 10's service methods.
- Standing mocks updated in the same PR: fix the copy (README "default branch", storage "package quota", typed name only for the whole package), add `?state=` values `mismatch`, `relink` and `quota`, and delete the per-page `mock-subnav` templates and their `replaceWith` scripts once `shell.js` has the tab.

## Files (hotspots in bold)

- **`internal/view/pages/`** (packages list, package detail), repo subnav fragment
- `internal/view/viewmodels_packages.go`
- `internal/handler/package_page_handler.go`, `package_handler.go`
- `internal/router/router.go`
- **`mockups/repo_packages.html`**, **`mockups/package_detail.html`**, `mockups/shell.js`, `mockups/COVERAGE.md`, `mockups/index.html`

## Acceptance criteria

- [x] The pages render every mock state: `list`, `publish`, `empty`, `container`, `private`, `generic`, `delete`, `mismatch`, `relink`, `quota`
- [x] A reader sees no delete or re-link controls; a writer sees tag and version delete; only a manager or the path owner sees Delete package and Re-link
- [x] The README card renders the linked Repository's default-branch README with working relative links and images, and is absent when there is none
- [x] After a transfer the mismatch note shows and the path owner can re-link to one of their own Repositories; an unreadable package is a `404`
- [x] Checked at desktop 1440 in **light and dark** on a throwaway server, against `make mockups` in both themes; the standing mocks are updated in the same PR
- [x] `make generate-templ` re-run and the generated files committed
- [x] Verified on a throwaway server (scratch Postgres, unique `CZ_AUTH_COOKIE_NAME`, `admin@example.test` / `cloudzilla-seed`), not the shared dev DB
- [x] `golangci-lint run`, `go test ./...` and `make test-integration` pass; Prettier is not run on `.scratch/`
- [x] Status set to `done` and these boxes ticked in the same commit

## Verification

Throwaway server with a real `docker push` of two tags (one multi-platform), a transferred Repository, and a `curl -T` generic upload; walk every state in light and dark at 1440 and compare with `make mockups`.

## Comments

### 2026-11-18

- Routes: `GET /{o}/{r}/packages` (`?type=`, `?q=`), `GET .../packages/container/*`, `GET .../packages/generic/{name}`; form
  `POST`s `.../packages/tags/delete` (tag or untagged digest, via `RegistryService.DeleteManifest`), `.../versions/delete`,
  `.../packages/delete` (typed name, 422 otherwise) and `.../packages/relink`. Forms carry `package_id` and the package
  must be linked to the URL's Repository, so a package of another Repository is a `404`. A refusal is a toast.
- Service additions (no queries in handlers): `PackageSummary` gained `OwnerName` (the path owner), owner ids and `Pulls`;
  a container's `LatestVersion` is now its newest tag; `InRepo`, `InRepoByID`, `CountByRepo` (Packages tab badge),
  `CanDelete` (path owner or `CanManage`, the rule `Delete` and `Relink` enforce) and `Storage`.
- Mocks changed to match what exists: generic packages show `—` for downloads (decision 18 counts manifest pulls only), the
  list rows lose the description line (no source for it), a version row shows no publisher, the tags table has a Pulls
  column, the curl command carries the base URL scheme, "README.md on main", storage reads "x of y package quota", tag and
  version delete only confirm (the typed name is for the whole package), and `mismatch`, `relink`, `quota` states exist.
  `shell.js` has the Packages tab; the per-page `mock-subnav` templates are gone.
- The Re-link button appears only when the path owner has another Repository the viewer can see; a manager who is not the
  path owner usually has none. The quota state is the storage note plus an alert once usage reaches the quota; a refused
  push or upload already carries the registry's message.
- Verified on a throwaway server (scratch DB `cz_ui`, server in an `alpine` container on :8493, cookie `cz_ui_t07`) with a
  real `docker push` of `v0.1.0`, a `v0.2.0` index of arm64 and amd64 (and their tags), a private image, a `curl -T` generic
  package with two versions, and a Repository moved to another owner (rows updated by SQL; a real accept needs a session).
  A tag delete through the confirm dialog removed the tag. Light and dark at 1440 for every state of both pages against
  `make mockups`: real pages in `/private/tmp/czpkg-ui/shots/{light,dark}-*.png`, mocks in `/private/tmp/czpkg-ui/shots/mock/`.
  Quota checked by restarting the server with a 1 MiB `quota.user.package_bytes`.
- Seen, not mine: the first `docker push` of a fresh image name failed twice with `this image is not linked to a repository`
  / `the image name was linked by another push; retry` and succeeded on the immediate retry (five fresh names, reproducible).
  Looks like ticket 04's first-push linking.
- `make test-integration` starts its own database; `go test ./...` ran on `cz_t07`.
