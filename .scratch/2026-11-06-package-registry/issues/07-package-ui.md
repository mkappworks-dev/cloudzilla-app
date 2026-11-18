# Repository Packages tab and package detail

Created: 2026-11-18
Category: enhancement
Status: ready-for-agent
Blocked by: 04, 05, 06
Parallel with: none

| Branch | Worktree | PR title | Effort |
| --- | --- | --- | --- |
| `feat/phase-16-package-ui` | `.worktrees/feat+phase-16-package-ui` | `feat(ui): repository packages tab and package detail (16.1)` | L |

Spec: [spec.md](../spec.md). Parent: `../../2026-11-06-uiux-desktop-9.5/issues/19-package-registry.md`. Tickets are built on the integration branch `feat/phase-16-package-registry`; the one PR for the spec comes from it.

## Problem

Daisy has pushed an image; she needs to see it on the Repository, copy the pull command, see its tags and digests, and remove a tag or the whole package.

## Scope

- Packages tab after Releases in the repo subnav (add to `shell.js` and the `.templ` subnav together), at `/{owner}/{repo}/packages` with type tabs, name filter, the type icon, badge, latest version, downloads and updated columns, the empty state with docker and curl how-tos and the token scopes, and the "Publish" popover.
- Package detail at `/{owner}/{repo}/packages/{type}/{name}`: copyable `docker pull` / `curl -LO` command, tags table (tag, digest, size, publisher, pulls, delete) with untagged manifests listed and deletable, files and versions for generic, the linked Repository's README card, and the aside (repository, owner-mismatch note after a transfer, platforms, storage, delete package).
- Deleting a version needs `CanWrite`; deleting the package needs `CanManage` and a typed name, and re-linking is offered to managers of the linked Repository (to any Repository they manage). The same `404` hides a private Repository's packages.
- View-models, Templ components, page handlers in `<concern>_page_handler.go`, and fragments for the HTMX delete flows. The pages and their `?state=` match the standing mocks, which are updated in the same PR: delete the per-page `mock-subnav` templates and their `replaceWith` scripts once `shell.js` has the real Packages tab; the generic URLs in the mocks already match the API and download paths.

## Files (hotspots in bold)

- **`internal/view/pages/`** (packages, package detail)
- `internal/view/viewmodels_packages.go`
- `internal/handler/package_page_handler.go`
- `internal/router/router.go`
- **`mockups/repo_packages.html`**, **`mockups/package_detail.html`**
- `mockups/shell.js` (add Packages to `REPO_TABS`), `mockups/COVERAGE.md`, `mockups/index.html`
- `tailwind/input.css` (only if a new class is needed)

## Acceptance criteria

- [ ] The list and detail pages match the standing mocks for every `?state=` (`list`, `publish`, `empty`, `container`, `private`, `generic`, `delete`)
- [ ] A reader sees no delete controls; a writer sees tag and version delete; only a manager sees "Delete package" and the typed-name dialog
- [ ] The README card renders the linked Repository's default-branch README, and is absent when there is none
- [ ] After a transfer, the owner-mismatch note shows; an unreadable package is a `404`, not a hidden row
- [ ] Checked at desktop 1440 in **light and dark** on a throwaway server
- [ ] `make generate-templ` re-run; `prettier --check` passes (never on `.scratch/`)
- [ ] Verified on a throwaway server (scratch Postgres, unique `CZ_AUTH_COOKIE_NAME`, `admin@example.test` / `cloudzilla-seed`), not the shared dev DB
- [ ] `golangci-lint run`, `go test ./...` and the touched `*_integration` tests pass; Prettier is not run on `.scratch/`
- [ ] Status set to `done` and these boxes ticked in the same commit

## Verification

Throwaway server with a real `docker push` of two tags (one multi-platform) and a `curl -T` generic upload; walk every state in light and dark at 1440 and compare with `make mockups`.

## Comments
