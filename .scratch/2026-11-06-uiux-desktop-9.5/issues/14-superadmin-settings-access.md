# Superadmin access to repo and org settings

Created: 2026-11-06
Category: enhancement
Status: done
Blocked by: 00

| Branch | Worktree | PR title | Wave | Effort | Score gain |
| --- | --- | --- | --- | --- | --- |
| `fix/superadmin-settings-access` | `.worktrees/fix+superadmin-settings-access` | `fix(auth): time-boxed admin mode for superadmins` | 2 | M | IA +0.2 |

## Problem

The seeded superadmin gets 403 on other users' repo settings and org settings. An instance admin has no UI path to fix a repo they don't own.

## Scope

Decided 2026-11-06: no blanket override. An explicit, time-boxed admin mode, modelled on GitLab's Admin Mode:

- "Enter admin mode" reuses the identity-confirm dialog (password plus 2FA or provider factor), as `admin.org.owner_add`
  does (`.scratch/2026-10-06-superadmin-org-owner/`).
- While active (1 hour, held server-side per session), `CanRead`, `CanWrite` and `CanManage` in
  `internal/service/repo_service_access.go` and the org permission checks return true for that superadmin.
- Audit entries for entering and leaving admin mode and for every change made under it.
- An "Acting as instance admin" banner with "Leave admin mode" on every page while active.
- Outside admin mode a superadmin is a normal user for repos and orgs; admin pages (`superadminMW`) are unchanged.
- Correct `docs/access-control.md`, which says superadmin can do "Everything" although no repo or org check has a
  superadmin case today.

## Files

- internal/service (permission checks)
- `docs/access-control.md`
- `internal/view/pages/repo_settings.templ`
- `internal/view/pages/org_settings.templ`

## Acceptance criteria

- [x] Decision recorded as an ADR in docs/adr/
- [x] Superadmin can open any repo or org settings only while in admin mode
- [x] Admin mode expires after 1 hour and can be left early
- [x] Audit entries for entering, leaving and each change
- [x] `docs/access-control.md` describes admin mode

## Verification

Router page-check harness as superadmin, owner and reader.

## Notes

Changes an authorization check: do it in the main session, not a subagent (memory: subagent-security-mutation-denied). Set Status to ready-for-human or needs-info at triage.

## Comments

2026-11-06 (triage): needs-info; blocked on the superadmin access decision (see spec Comments).

2026-11-06 (maintainer): decided admin mode over a blanket override; ready-for-human because it changes authorization
checks. Prior art: GitLab Admin Mode (re-auth, 6 h expiry), GitHub Enterprise Server repo unlock (audited, temporary).

2026-11-06 (wave 1 re-score, ticket 22): Findings in this ticket's files, left for it: the signed-out header shows
"Sign in" and "Register" on the sign-in page itself and the theme toggle changes both `aria-pressed` and `aria-label`
(`layout.templ:203`, `:400`; Claude, +0.05); admin pages show a bare tab row with no "Administration" label (Claude,
unverified); the repo settings default branch is free text (`repo_settings.templ:181`, ticket 28); "Renaming from
settings is not yet available." is roadmap copy on a disabled field (`repo_settings.templ:169`, ticket 15).

2026-11-07 (implemented): ADR 0001, migration 117, `internal/adminmode` (context value bound to the user id), banner and
`/admin/mode`. Admin mode is per browser and cookie-only: PATs, OAuth, git HTTP and SSH never get it. The 403 on repo and
org settings points to it for superadmins. Owner-only actions (transfer, delete, the `admin` role) are covered too, since
`IsOwner` is; SQL list filters (`readableBy`, `ownedBy`) are not.
