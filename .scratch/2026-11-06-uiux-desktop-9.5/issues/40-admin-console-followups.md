# Admin console follow-ups

Created: 2026-11-12
Category: enhancement
Status: ready-for-agent
Blocked by: —

| Branch | Worktree | PR title | Wave | Effort | Score gain |
| --- | --- | --- | --- | --- | --- |
| `fix/admin-console-followups` | `.worktrees/fix+admin-console-followups` | `fix(ui): admin console follow-ups` | polish (wave 2 re-score) | S | Content +0.1, IA +0.1 |

## Problem

Found by the wave 2 re-score (Claude and Codex).

1. The admin tables have no row links or sorting, pagination is Next-only, and the audit log has no details column (Claude, `admin-users`, `admin-audit-log`).
2. The admin-mode banner shows an absolute UTC expiry instead of time left (Claude, `home-admin-banner`).

## Scope

- Link usernames to `/admin/users/<name>`, add sort and a Previous link, add the details column, show the time left as well as the expiry.

## Files

- `internal/view/pages/admin_users.templ`, `audit_log.templ`, `layout/layout.templ` (banner)
- `mockups/admin_users.html`, `audit_log.html`

## Acceptance criteria

- [ ] Rows link to the user page
- [ ] Tables sort and page both ways
- [ ] Audit log shows what changed
- [ ] Mocks updated, light and dark

## Verification

Throwaway server at 1440×900 in light and dark, per the harness in [spec.md](../spec.md).

## Comments

2026-11-12 (wave 2 re-score): opened by ticket 22. Run `/triage` before any code.

2026-11-12 (triage): ready-for-agent. Show time left on the admin-mode banner.
