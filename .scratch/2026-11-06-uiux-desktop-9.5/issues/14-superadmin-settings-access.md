# Superadmin access to repo and org settings

Created: 2026-11-06
Category: enhancement
Status: needs-info
Blocked by: 00

| Branch | Worktree | PR title | Wave | Effort | Score gain |
| --- | --- | --- | --- | --- | --- |
| `fix/superadmin-settings-access` | `.worktrees/fix+superadmin-settings-access` | `fix(auth): let superadmins open repo and org settings` | 2 | M | IA +0.2 |

## Problem

The seeded superadmin gets 403 on other users' repo settings and org settings. An instance admin has no UI path to fix a repo they don't own.

## Scope

- Decide first (needs a human): should superadmin imply `CanManage` everywhere, or only via an explicit "act as admin" step that is audit-logged?
- Show an "Acting as instance admin" banner on those pages.

## Files

- internal/service (permission checks)
- `docs/access-control.md`
- `internal/view/pages/repo_settings.templ`
- `internal/view/pages/org_settings.templ`

## Acceptance criteria

- [ ] Decision recorded as an ADR in docs/adr/
- [ ] Superadmin can open settings per that decision
- [ ] Audit log entry for admin-acting changes

## Verification

Router page-check harness as superadmin, owner and reader.

## Notes

Changes an authorization check: do it in the main session, not a subagent (memory: subagent-security-mutation-denied). Set Status to ready-for-human or needs-info at triage.

## Comments

2026-11-06 (triage): needs-info; blocked on the superadmin access decision (see spec Comments).
