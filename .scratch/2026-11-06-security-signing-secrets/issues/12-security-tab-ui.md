# Security tab, alert list and alert page

Created: 2026-11-13
Category: enhancement
Status: done
Blocked by: 10
Parallel with: 11, 13

| Branch | Worktree | PR title | Effort |
| --- | --- | --- | --- |
| `feat/phase-17.3-security-tab` | `.worktrees/feat+phase-17.3-security-tab` | `feat(security): secret scanning alerts UI (17.3)` | L |

Spec: [spec-secret-scanning.md](../spec-secret-scanning.md). Parent: `../../2026-11-06-uiux-desktop-9.5/issues/17-secret-scanning.md`.

## Problem

The mock draws a Security tab, an alert list and an alert page with a Close-as menu, but the repo has none of them, so a writer cannot see or triage an alert.

## Scope

- `GET /{owner}/{repo}/security/secrets` (Open/Closed tabs with counts, a type filter, rows, the empty state) and `GET …/{n}` (masked snippet read from the commit, How to fix, Activity from `secret_scan_events`, the aside with pattern, all locations and whether the value is on the default branch, Close as / Reopen, the `blocked` banner with Allow as used in tests / false positive). Both need `CanWrite` and otherwise return the missing-repo 404 ([git-transport](../../docs/git-transport.md) "private repos look missing" rule for HTML).
- `POST …/{n}/close`, `…/reopen`, `…/allow` (HTMX swap as elsewhere, plain redirect without HTMX), writing the audit entries from ticket 11. A blocked alert's banner buttons post to `allow`. `allow` on an `auto_revoked` or `cloudzilla_token` alert is refused.
- Snippet: read `file_path` at `commit_sha` for the alert's first `reached_ref` location (else any), show up to 3 lines of context, and mask the secret's span with `••••`; "commit no longer available" when the object is missing (spec decision 14). Never render the full value.
- Repo subnav: a `Security` tab after Releases and before Settings, only when the viewer `CanWrite`, its badge `OpenCount`; add it to `mockups/shell.js` `REPO_TABS`.
- View models in `internal/view/viewmodels_security.go` (new); pages `security_secrets.templ` and `security_secret_alert.templ` in `internal/view/pages/`. Route registration in `internal/router/router.go`; no setup-path change (not reachable before setup).
- Mocks: bring `security.html` and `alert.html` here in line with the shipped templ; add standing `mockups/security_secrets.html` and `mockups/security_secret_alert.html` with the open, closed, empty, blocked and each close-reason states; register both in `mockups/index.html`; add the tab to `shell.js`.

## Files (hotspots in bold)

- **`internal/view/fragments/repo_subnav.templ`**, `internal/view/pages/security_secrets.templ` (new), `security_secret_alert.templ` (new), **`internal/view/viewmodels_security.go`** (new)
- `internal/handler/security_page_handler.go` (new, `Page*` methods), `internal/handler/security_handler.go` (new, the three POSTs), **`internal/router/router.go`**
- `mockups/shell.js`, `mockups/security_secrets.html`, `mockups/security_secret_alert.html`, `mockups/index.html`, `mockups/COVERAGE.md`, `.scratch/2026-11-06-security-signing-secrets/security.html`, `alert.html`

## Acceptance criteria

- [x] A reader and an anonymous visitor get the same 404 as for a missing repo on both pages and all three POSTs; no tab, no count for them (handler tests)
- [x] Close, reopen and allow change state and write the events and audit entries; allow on a live-token alert is refused with 422
- [x] The snippet masks the secret, shows "commit no longer available" for a pruned commit, and no response body contains the full value (test greps for it)
- [x] Open/closed counts and the type filter agree with the list; the tab badge excludes blocked-only alerts
- [x] Seed alerts through `SecretAlertService.Record` in a test or a throwaway script; the dev database is not touched
- [x] Checked at desktop 1440 in **light and dark** on a throwaway server (scratch Postgres, unique `CZ_AUTH_COOKIE_NAME`, `admin@example.test` / `cloudzilla-seed`); evidence for both themes in the PR
- [x] The standing mocks in `mockups/` listed under Files are updated in the same PR (classes copied from the templ, `COVERAGE.md` / `index.html` / `shell.js` where affected) and checked under `make mockups` in both themes
- [x] Verified on a throwaway server (scratch Postgres on a free port, unique `CZ_AUTH_COOKIE_NAME`, `admin@example.test` / `cloudzilla-seed`) with native git; what was seen is in the PR
- [x] `make generate-templ` re-run and the `_templ.go` files committed (if `.templ` changed); `make lint` (`/Users/mk/go/bin/golangci-lint run`) and `go test ./...` pass

## Verification

Throwaway server: scratch Postgres, unique `CZ_AUTH_COOKIE_NAME`, `admin@example.test` / `cloudzilla-seed`. Insert alerts with a throwaway Go test or SQL on the scratch database (all states: open, closed per resolution, auto-revoked, blocked, empty). Check the list, the alert page in each state and the Close-as menu at 1440 in light and dark; sign in as a reader of a private repo and confirm the 404 and the absent tab. Do not seed rows on the shared dev database.

## Comments

