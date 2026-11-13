# Docs, ROADMAP and native-git end-to-end

Created: 2026-11-13
Category: enhancement
Status: ready-for-agent
Blocked by: 01, 02, 03, 04, 05, 06, 07
Parallel with: none

| Branch | Worktree | PR title | Effort |
| --- | --- | --- | --- |
| `feat/phase-17.2-signing-docs` | `.worktrees/feat+phase-17.2-signing-docs` | `docs(signing): signed commit verification (17.2)` | M |

Spec: [spec.md](../spec.md). Parent: `../../2026-11-06-uiux-desktop-9.5/issues/16-signed-commits.md`.

## Problem

A shipped feature needs its subsystem doc, a correct ROADMAP entry and an end-to-end check with real git clients.

## Scope

- `docs/signed-commits.md`: statuses and reasons, the email rule, expiry and the accepted forgery cost, key tables and routes, instance signing and rotation, `allowed_signers` setup for operators, the committer change, and the behaviour with no `security.secret_key`. Link it from CLAUDE.md's subsystem list.
- `docs/ROADMAP.md`: rewrite 17.2 (no `verify_commits`, `gpg_keys`/`ssh_signing_keys`/`instance_signing_keys`, `partially_verified`, instance signing), mark it ✅ with the migration numbers used. 17.3 is not touched.
- Cross-links in `docs/git-transport.md` (signing keys are not auth keys) and `docs/access-control.md` (session-only, step-up); `docs/api-reference.md` has the endpoints from ticket 01.
- End-to-end on a throwaway server with native git: a GPG-signed and an SSH-signed commit, a hardware-key-style `no-touch-required` SSH signature if available, an unknown key, an unverified email, an expired key, and a web squash merge; record the statuses seen in the PR.
- Tick the acceptance criteria of `../2026-11-06-uiux-desktop-9.5/issues/16-signed-commits.md`, set it `done`, update the Run order in that folder's `spec.md`.

## Files (hotspots in bold)

- `docs/signed-commits.md` (new), **`docs/ROADMAP.md`**, **`CLAUDE.md`**, `docs/git-transport.md`, `docs/access-control.md`, `docs/api-reference.md`
- `.scratch/2026-11-06-uiux-desktop-9.5/issues/16-signed-commits.md`, `.scratch/2026-11-06-uiux-desktop-9.5/spec.md`
- `mockups/README.md` / `COVERAGE.md` if a page's coverage changed

## Acceptance criteria

- [ ] `docs/signed-commits.md` exists, is Prettier-formatted (`npm run fmt`) and linked from CLAUDE.md
- [ ] ROADMAP 17.2 matches what shipped and the status table row is ✅
- [ ] The end-to-end run's statuses are in the PR body and match the doc
- [ ] Ticket 16 is `done` with all criteria ticked; the 9.5 Run order row is updated
- [ ] Light and dark 1440 spot check of one commit page and the Signing keys section (the final regression pass)
- [ ] `npm run fmt:check` passes (not run on `.scratch/`)

## Verification

Throwaway server: scratch Postgres on a free port (see `docs/testing.md`), a unique `CZ_AUTH_COOKIE_NAME`, sign in as `admin@example.test` / `cloudzilla-seed`. The full end-to-end above.

## Comments

