# Accessibility quick fixes

Created: 2026-11-06
Category: bug
Status: ready-for-agent
Blocked by: 01

| Branch | Worktree | PR title | Wave | Effort | Score gain |
| --- | --- | --- | --- | --- | --- |
| `fix/a11y-quick-fixes` | `.worktrees/fix+a11y-quick-fixes` | `fix(a11y): broken names, missing labels and roles` | 1, lane A | S | A11y +0.5 |

## Problem

From the audit: `release_detail.templ:18` `aria-labelledby="rel-title"` points at nothing; milestone due-date input unlabeled (`milestone_meta.templ:120`); account-delete confirm input unlabeled (`settings.templ:894`); SSO and profile banners lack `role` (`sso_settings.templ:25,28`, `settings.templ:23`); status icons are glyph-only (`status_icon.templ:8-16`); line-comment delete has no label or type (`line_comments.templ:25-30`); dependency headings jump h1→h3 (`dependency_group.templ:18`); footer's static status line has `role="status"` (`layout.templ:288`). Plurals such as "1 reviewers" are in ticket 08.

## Scope

- Fix each item; sr-only text next to status glyphs.
- Add `dark:prose-invert` to `line_comments.templ` prose.

## Files

- the files named above

## Acceptance criteria

- [ ] Each listed item fixed
- [ ] axe (run once by hand) reports no violations on release detail, milestone detail, settings, PR files

## Verification

Run axe-core in the Playwright harness on the touched pages.

## Comments

2026-11-06 (triage): ready-for-agent.
