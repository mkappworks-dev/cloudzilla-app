# Consistent initials and avatars

Created: 2026-11-06
Category: bug
Status: done
Blocked by: 00

| Branch | Worktree | PR title | Wave | Effort | Score gain |
| --- | --- | --- | --- | --- | --- |
| `fix/avatar-initials-consistency` | `.worktrees/fix+avatar-initials-consistency` | `fix(ui): one initials rule for header, rows and activity` | 1, lane D | S | Visual +0.3 |

## Problem

The header shows "SI" (`layout.templ:653`), the body "S" (`components.Initials`), the profile activity lowercase "ad" (`activity_row.templ:77`). Home repo icons are empty squares that read as checkboxes (`home.templ:359`).

## Scope

- Route the header and `activity_row` through `components.Initials`/`components.Avatar` (real avatar images where uploaded).
- Replace the home repo icon with the repo glyph used in `repo_list_row.templ`.
- Update mocks: they draw two-letter initials everywhere; match the real rule.

## Files

- `internal/view/layout/layout.templ`
- `internal/view/components/activity_row.templ`
- `internal/view/pages/home.templ`
- `mockups/shell.js`
- `mockups/*.html`

## Acceptance criteria

- [x] Same user shows the same initials in header, rows and activity
- [x] Home repo rows show a repo icon
- [x] Mocks use the real initials rule

## Verification

Screenshot home, profile, PR page at 1440.

## Comments

2026-11-06 (triage): ready-for-agent.
