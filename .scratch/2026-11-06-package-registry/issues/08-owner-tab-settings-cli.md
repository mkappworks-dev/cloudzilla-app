# Owner Packages tab, storage usage and `cz` scope docs

Created: 2026-11-18
Category: enhancement
Status: ready-for-agent
Blocked by: 05, 07
Parallel with: none

| Branch | Worktree | PR title | Effort |
| --- | --- | --- | --- |
| `feat/phase-16-owner-tab-settings-cli` | `.worktrees/feat+phase-16-owner-tab-settings-cli` | `feat(ui): owner packages tab and storage usage (16.2)` | S |

Spec: [spec.md](../spec.md). Parent: `../../2026-11-06-uiux-desktop-9.5/issues/19-package-registry.md`. Tickets are built on the integration branch `feat/phase-16-package-registry`; the one PR for the spec comes from it.

## Problem

People browse an owner's images from their profile, owners need to see how much registry storage they use against the quota, and the CLI docs must list the new token scopes.

## Scope

- `/{owner}?tab=packages` on user and organization profiles: a Packages tab across the owner's readable Repositories with type and Repository filters, matching `mockups/owner_packages.html`.
- Account and organization settings show registry storage (the usage accessor from ticket 05) next to the git storage usage and the `package_bytes` quota when set.
- `docs/cli.md` gets the new scopes (the flag and the approval page already accept them from ticket 01).
- The profile subnav and `mockups/COVERAGE.md` list the new page.

## Files (hotspots in bold)

- **`internal/view/pages/`** (profile, settings)
- `internal/handler/` profile and settings page handlers
- **`mockups/owner_packages.html`**, `mockups/profile.html`, `mockups/org_profile.html`, `mockups/account_settings.html`
- `docs/cli.md`

## Acceptance criteria

- [ ] The tab lists only packages the viewer can read; a private Repository's package is absent for other users
- [ ] Settings show usage and quota for a user and an organization, and nothing changes at quota `0`
- [ ] A token from `cz auth login --scope packages:write` pushes an image on a throwaway server
- [ ] Checked at desktop 1440 in **light and dark** on a throwaway server; the touched standing mocks are updated in the same PR
- [ ] Verified on a throwaway server (scratch Postgres, unique `CZ_AUTH_COOKIE_NAME`, `admin@example.test` / `cloudzilla-seed`), not the shared dev DB
- [ ] `make generate-templ` re-run; `golangci-lint run`, `prettier --check` (not on `.scratch/`) and `go test ./...` pass
- [ ] Status set to `done` and these boxes ticked in the same commit

## Verification

Throwaway server: two owners (a user and an org), a public and a private Repository each with packages; check the tab as owner, as a stranger and anonymous, in light and dark at 1440.

## Comments
