# Docs, ROADMAP and native-git end-to-end

Created: 2026-11-13
Category: enhancement
Status: done
Blocked by: 09, 10, 11, 12, 13, 14, 15, 16
Parallel with: none

| Branch | Worktree | PR title | Effort |
| --- | --- | --- | --- |
| `feat/phase-17.3-secret-scanning-docs` | `.worktrees/feat+phase-17.3-secret-scanning-docs` | `docs(secrets): secret scanning (17.3)` | M |

Spec: [spec-secret-scanning.md](../spec-secret-scanning.md). Parent: `../../2026-11-06-uiux-desktop-9.5/issues/17-secret-scanning.md`.

## Problem

A shipped feature needs its subsystem doc, a correct ROADMAP entry (the 17.3 migration number and `state` values are stale) and an end-to-end check with real git clients.

## Scope

- `docs/secret-scanning.md`: patterns and confidence, the alert model and resolutions, push protection and the exact rejection text over HTTP and SSH, fail-open budget and its config keys, allow semantics, auto-revoke, backfill, who sees what, notifications and audit actions, and the accepted costs (hash guessability of low-entropy values, loose objects left by a refused push until `cz-admin gc`, no side-band). Link it from CLAUDE.md's subsystem list.
- `docs/ROADMAP.md`: rewrite 17.3 (no `117_…`, `secret_scan_alerts` with `open/closed` + `resolution`, push protection, auto-revoke, backfill), mark it ✅ with the migration numbers used; update the phase table.
- Cross-links: `docs/git-transport.md` (the vet hook now also scans; the status text), `docs/access-control.md` (Security tab needs `CanWrite`), `docs/configuration.md` (keys, if ticket 13 and 16 did not finish), `docs/testing.md`, `docs/webhooks.md` (no `secret_scanning_alert` event yet), `GLOSSARY.md` if terms moved.
- End-to-end on a throwaway server with native git over HTTP and SSH: AWS key, private key, live and dead `czp_` token, a fixture allowed then pushed again, a tag, two refs sharing a commit, a web edit, a backfill of an existing repo, protection off; record each outcome in the PR.
- Tick the acceptance criteria of `../2026-11-06-uiux-desktop-9.5/issues/17-secret-scanning.md`, set it `done`, update the Run order in that folder's `spec.md`, and set this spec's status to `done`.

## Files (hotspots in bold)

- `docs/secret-scanning.md` (new), **`docs/ROADMAP.md`**, **`CLAUDE.md`**, `docs/git-transport.md`, `docs/access-control.md`, `docs/configuration.md`, `docs/testing.md`, `docs/webhooks.md`
- `.scratch/2026-11-06-uiux-desktop-9.5/issues/17-secret-scanning.md`, `.scratch/2026-11-06-uiux-desktop-9.5/spec.md`, `.scratch/2026-11-06-security-signing-secrets/spec-secret-scanning.md`

## Acceptance criteria

- [x] Docs describe behaviour that was seen in the end-to-end run, not planned behaviour; any difference from this spec is recorded in the spec's Comments
- [ ] `npm run fmt` has been run on the changed `.md` files (not on `.scratch/`) and `npm run fmt:check` passes
- [x] ROADMAP 17.3 and the phase table are consistent with the migrations that merged
- [ ] ~~Checked at desktop 1440 in light and dark~~ (no UI in this ticket)
- [ ] ~~The standing mocks in `mockups/` are updated~~ (no UI in this ticket)
- [x] Verified on a throwaway server (scratch Postgres on a free port, unique `CZ_AUTH_COOKIE_NAME`, `admin@example.test` / `cloudzilla-seed`) with native git; what was seen is in the PR
- [x] `make lint` (`/Users/mk/go/bin/golangci-lint run`) and `go test ./...` pass

## Verification

The end-to-end run above is the verification: screenshots of the Security tab, an alert and a rejected push in light and dark at 1440 on the throwaway server, plus the terminal output of both transports.

## Comments


Claude, 2026-11-13 (end-to-end): throwaway server (scratch Postgres on :55912, port 18093, cookie `cz_secretdocs`,
`cloudzilla seed`), native git over HTTP with a token and over SSH with an ed25519 key. Refusal line, exactly:
`! [remote rejected] <ref> -> <ref> (secret detected: aws_access_key_id in config.env:1; if it is not a live secret, a writer can allow it at http://127.0.0.1:18093/siteadmin/secretlab/security/secrets/1)`.

- AWS key, new branch: refused over HTTP and SSH, one alert (the second push added no alert).
- Fast-forward adding a key: refused. Lightweight tag (HTTP) and annotated tag (SSH): refused with the commit's alert URL.
- Private key: `secret detected: private_key in id_rsa:1; ...`.
- Two refs sharing a commit in one push: both refused, one alert (`#4`).
- Live `czp_` token: `secret detected: Cloudzilla access token in tok.env:1 (the token was revoked); see <url>`; token row deleted, alert closed `revoked` with Auto-revoked, owner notified (`token_revoked`).
- Dead `czp_` token: unknown hash accepted with no alert; expired token accepted, alert opened closed `revoked` without the Auto-revoked badge.
- Allow (`POST .../allow`, `used_in_tests`, `303`), then push again: accepted over HTTP and SSH. Allow on the live-token alert and `resolution=revoked`: `422`.
- Reopen, push the same commit under a new branch name: accepted (nothing added). Reopen, push a new commit with the key: refused.
- Web editor: new file and edit with a key `422` with the same line, no commit; clean file `303`.
- Backfill: a history pushed straight into the bare repo (AWS key added then removed, OpenSSH key on a side branch, a live token) and the row set to `pending`: alerts for all three within one poll, token deleted, one `secret_alert` digest.
- `push_protection: false`: key accepted over HTTP and SSH and alerted after the push; a live token accepted then deleted by the post-receive scan.
- Security list, alert page (Blocked push banner) and closed list checked at 1440 in light and dark.

Not as the spec says: the Security page intro keeps saying pushes are rejected when `push_protection` is off; the history
walk ignores `secret_scanning.max_blob_bytes` (fixed 1 MiB); a web-editor alert has no commit. `npm run fmt` could not run
(no `node_modules`), so the new `.md` files are wrapped by hand and Prettier's table alignment is unchecked.
