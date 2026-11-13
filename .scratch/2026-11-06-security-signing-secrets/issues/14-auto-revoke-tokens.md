# Auto-revoke Cloudzilla tokens

Created: 2026-11-13
Category: enhancement
Status: done
Blocked by: 11, 12, 13
Parallel with: 15, 16

| Branch | Worktree | PR title | Effort |
| --- | --- | --- | --- |
| `feat/phase-17.3-auto-revoke-tokens` | `.worktrees/feat+phase-17.3-auto-revoke-tokens` | `feat(secrets): auto-revoke leaked Cloudzilla tokens (17.3)` | M |

Spec: [spec-secret-scanning.md](../spec-secret-scanning.md). Parent: `../../2026-11-06-uiux-desktop-9.5/issues/17-secret-scanning.md`.

## Problem

Kiyo commits their personal access token in `scripts/smoke.sh`. The server stores only the token's hash, so it can tell exactly whether the match is live, and a live one should stop working at once.

## Scope

- `PushScanService` (ticket 13) resolves each `cloudzilla_token` finding with `AccessTokenService`: hash the value (SHA-256, as `newToken` does), `GetByHash`; a miss drops the finding, a hit deletes the token (`AccessTokenStore.Delete(tokenID, userID)`), closes the alert as `revoked` with `resolved_by` NULL and `auto_revoked = true`, writes event `auto_revoked`, audit `token.auto_revoke`, and calls `NotifyTokenRevoked` (ticket 11).
- In a blocked push the push is still refused, with the text `secret detected: Cloudzilla access token in <path>:<line> (the token was revoked); …` and no allow link. Backfill and post-receive hits revoke the same way.
- Add an expiry- and revoked-aware lookup if `GetByHash` returns an expired token (an expired token is not live: do not revoke, record the finding as closed `revoked`).
- Ticket 12 already renders the **Auto-revoked** badge from `auto_revoked` and hides Allow for it; this ticket only produces such alerts and checks them.

## Files (hotspots in bold)

- **`internal/service/push_scan_service.go`**, `internal/service/access_token_service.go`, `internal/store/access_token_store.go`
- `mockups/security_secrets.html`, `mockups/security_secret_alert.html` (the Auto-revoked badge state, built by 12, checked here)

## Acceptance criteria

- [x] A pushed live `czp_` token is deleted, its owner gets a notification and mail, the audit entry exists, and the next API request with it is 401
- [x] A well-formed but unknown or already-deleted `czp_` token produces no alert; an expired one produces a closed `revoked` alert and no revoke
- [x] A live token in a push by a different user revokes the owner's token and the pusher sees the revoked text; the alert has no Allow action
- [x] The same token seen again later (same hash) appends a location to the closed alert and does nothing else
- [x] Checked at desktop 1440 in **light and dark** on a throwaway server (scratch Postgres, unique `CZ_AUTH_COOKIE_NAME`, `admin@example.test` / `cloudzilla-seed`); evidence for both themes in the PR
- [x] The standing mocks in `mockups/` listed under Files are updated in the same PR (classes copied from the templ, `COVERAGE.md` / `index.html` / `shell.js` where affected) and checked under `make mockups` in both themes
- [x] Verified on a throwaway server (scratch Postgres on a free port, unique `CZ_AUTH_COOKIE_NAME`, `admin@example.test` / `cloudzilla-seed`) with native git; what was seen is in the PR
- [x] `make generate-templ` re-run and the `_templ.go` files committed (if `.templ` changed); `make lint` (`/Users/mk/go/bin/golangci-lint run`) and `go test ./...` pass

## Verification

Throwaway server: create a PAT in Settings, commit it into a file, push with native git over HTTP (using a second token) and over SSH; confirm the pushed token now returns 401, the owner's notification, and the Auto-revoked badge at 1440 in light and dark.

## Comments

Claude, 2026-11-13: the Auto-revoked badge state already ships in `mockups/security_secrets.html` and `security_secret_alert.html` from ticket 12, which checked them in both themes; this ticket changed no markup, so no further mock edit was needed.
