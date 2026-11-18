# Owner Packages tab, storage usage and `cz` scope docs

Created: 2026-11-18
Category: enhancement
Status: ready-for-agent
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
- The profile subnav and `mockups/COVERAGE.md` list the new page.

## Files (hotspots in bold)

- **`internal/view/pages/`** (profile, org profile, settings)
- profile and settings page handlers in `internal/handler/`
- **`mockups/owner_packages.html`**, `mockups/profile.html`, `mockups/org_profile.html`, `mockups/account_settings.html`, `mockups/org_settings.html`
- `docs/cli.md`

## Acceptance criteria

- [ ] The tab lists only packages the viewer can read; a private Repository's package is absent for other users and anonymous viewers
- [ ] Settings show usage and quota for a user and an organization; at quota `0` only the usage shows
- [ ] A token from `cz auth login --scope packages:write` pushes an image on a throwaway server
- [ ] Checked at desktop 1440 in **light and dark** on a throwaway server; the touched standing mocks are updated in the same PR
- [ ] `make generate-templ` re-run and the generated files committed
- [ ] Verified on a throwaway server (scratch Postgres, unique `CZ_AUTH_COOKIE_NAME`, `admin@example.test` / `cloudzilla-seed`), not the shared dev DB
- [ ] `golangci-lint run`, `go test ./...` and `make test-integration` pass; Prettier is not run on `.scratch/`
- [ ] Status set to `done` and these boxes ticked in the same commit

## Verification

Throwaway server: a user and an org, each with a public and a private Repository with packages; check the tab as owner, stranger and anonymous, and settings for both, in light and dark at 1440.

## Comments
