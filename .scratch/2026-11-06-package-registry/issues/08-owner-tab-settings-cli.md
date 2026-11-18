# Owner Packages tab, storage usage and `cz` scope docs

Created: 2026-11-18
Category: enhancement
Status: done
Blocked by: 07
Parallel with: none

| Branch | Worktree | PR title (into `feat/phase-16-package-registry`) | Effort |
| --- | --- | --- | --- |
| `feat/phase-16-owner-tab-settings-cli` | `.worktrees/feat+phase-16-owner-tab-settings-cli` | `feat(ui): owner packages tab and storage usage (16.2)` | S |

Spec: [spec.md](../spec.md). Parent: `../../2026-11-06-uiux-desktop-9.5/issues/19-package-registry.md`. The integration branch `feat/phase-16-package-registry` (worktree `.worktrees/feat+phase-16-package-registry`) collects every ticket; the one PR to `main` comes from it.

## Problem

People browse an owner's images from their profile, owners need to see their package storage against its quota, and the CLI docs must list the new scopes.

## Scope

- `/{owner}?tab=packages` on user and organization profiles: packages under the path the viewer can read, with type and Repository filters, matching `mockups/owner_packages.html`.
- Account and organization settings show package storage (ticket 01's accessor) next to the git storage usage, with the `package_bytes` quota when set.
- `docs/cli.md` lists the new scopes (the flag and approval page accept them since ticket 01).
- From ticket 10: when the viewer is the path owner (the user, or an owner of the organization), each package under the
  path shows Delete and Re-link through form `POST`s on `/{owner}/-/packages/{delete,relink}`, including a package in a
  private Repository the path owner can no longer read.
- The profile subnav and `mockups/COVERAGE.md` list the new page.

## Files (hotspots in bold)

- **`internal/view/pages/`** (profile, org profile, settings)
- profile and settings page handlers in `internal/handler/`
- **`mockups/owner_packages.html`**, `mockups/profile.html`, `mockups/org_profile.html`, `mockups/account_settings.html`, `mockups/org_settings.html`
- `docs/cli.md`

## Acceptance criteria

- [x] The tab lists only packages the viewer can read; a private Repository's package is absent for other users and anonymous viewers
- [x] The path owner sees Delete and Re-link for every package under the path, a private Repository they cannot read included; nobody else does
- [x] Settings show usage and quota for a user and an organization; at quota `0` only the usage shows
- [ ] A token from `cz auth login --scope packages:write` pushes an image on a throwaway server (works with `--scope packages:write --scope repo:read`; see the comment)
- [x] Checked at desktop 1440 in **light and dark** on a throwaway server; the touched standing mocks are updated in the same PR
- [x] `make generate-templ` re-run and the generated files committed
- [x] Verified on a throwaway server (scratch Postgres, unique `CZ_AUTH_COOKIE_NAME`, `admin@example.test` / `cloudzilla-seed`), not the shared dev DB
- [x] `golangci-lint run`, `go test ./...` and `make test-integration` pass; Prettier is not run on `.scratch/`
- [x] Status set to `done` and these boxes ticked in the same commit

## Verification

Throwaway server: a user and an org, each with a public and a private Repository with packages; check the tab as owner, stranger and anonymous, and settings for both, in light and dark at 1440.

## Comments

### 2026-11-18

- Tab: `/{owner}?tab=packages` renders inside the profile page (`OwnerPackagesTab` in `owner_packages.templ`, used by `User` and
  `Org`), with type and Repository filters (GET form, `?type=`, `?repo=`). The user nav has a Packages tab between Repositories
  and Stars, with a count only while it is the active tab (counting needs a per-repository read check, too much for every
  profile load); the org header has a Packages link. `PackageService.ListByOwner` now sets `RepoPrivate` and, for the path owner
  only, keeps packages in Repositories they cannot read, flagged `RepoHidden` (name and repository shown, no link).
  `IsPathOwner` and `InOwner` are new service methods; `UsageSummary` feeds settings.
- Path-owner forms (ticket 10's addition): `POST /{owner}/-/packages/delete` (typed name, `422` otherwise) and `.../relink` in
  `package_handler.go`. `-` cannot start a repository name, so the route never shadows `/{owner}/{repo}/...` (`/{owner}/packages/delete`
  would have shadowed deleting a repository called `packages`). A package the poster cannot see is a `404`, a visible one
  they do not own as path owner (a repo manager) a `403`; both services re-check. Success redirects to the tab. The delete and
  re-link dialogs are per row, after the table.
- Settings: a separate `#package-usage` line under the git storage line: `Packages 4 MiB of 100 MiB` with a quota, `Packages 4 MiB`
  with none (the git line still shows only with a quota). Superadmin's personal account has no package quota (ticket 01).
- `docs/cli.md`: a Packages section and a scopes-table row. `cz` has no package commands; it only mints the token, and has no
  command that prints it (`--insecure-storage` puts it in `credentials.json`).
- **Not met: `cz auth login --scope packages:write` alone.** `cz` verifies the token with `GET /api/user`, which only admits `repo:*`,
  `issues:write` and `pulls:write`, so with `packages:write` alone login ends `403 token lacks scope "repo:read"` and nothing is stored
  (the device grant is approved server-side). `docs/cli.md` still says `cz auth login` works with any scope. Works with
  `--scope packages:write --scope repo:read`: `docker login` with that stored token and `docker push` of four images succeeded,
  and `curl -X PUT` uploaded generic files. The fix I tried, admitting the exact `GET /api/user` for the packages scopes in
  `middleware/scope.go`, was denied by the permission classifier (it widens token scopes), so it is not in this branch; decide
  between that and having `cz` verify another way. Spec story 47 expects the single scope to work.
- Verified on a throwaway server (scratch DB `cz_ui8` on :5494, server in an `alpine` container on :8498, cookie `cz_ui_t08`,
  quota `quota.user.package_bytes` 100 MiB): `wen-yilmaz` and org `acme-labs`, each with a public and a private Repository and
  packages (containers pushed with a `cz`-minted token, generic files with `curl`), plus a package linked to another user's
  private Repository for the hidden row. Owner, stranger and anonymous at 1440 in light and dark; the owner saw Delete/Re-link
  on every row, the others none; a stranger and anonymous saw only readable rows. A real delete of the hidden row and a re-link
  through the dialogs changed the DB. Screenshots: `/private/tmp/czpkg-t08/shots/{light,dark}-{owner,stranger,anon}-{user,org}.png`,
  `-owner-delete.png`, `-owner-relink.png`, `-settings.png`, `-orgsettings.png`; mocks in the same folder as `mock-{light,dark}-*.png`
  from `make mockups`' server. The server must be built with `htmx.min.js` (`make download-htmx`) or dialogs submit as plain GETs.
- Mocks: `owner_packages.html` gained `?state=list|manage|delete|relink`; `profile.html` and `org_profile.html` link to it;
  `account_settings.html` and `org_settings.html` show the package line; `COVERAGE.md` lists the page and its states.
- `make test-integration` starts its own database; `go test ./...` ran on `cz_t08`. `make build-css` needs `bin/tailwindcss`, absent in
  this worktree: I ran the main checkout's binary, and `main.css` is gitignored.
