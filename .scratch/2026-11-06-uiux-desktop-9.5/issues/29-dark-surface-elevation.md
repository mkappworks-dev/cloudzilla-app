# Dark mode surface elevation and input borders

Created: 2026-11-06
Category: enhancement
Status: done
Blocked by: —

| Branch | Worktree | PR title | Wave | Effort | Score gain |
| --- | --- | --- | --- | --- | --- |
| `fix/dark-surface-elevation` | `.worktrees/fix+dark-surface-elevation` | `fix(ui): dark mode surface elevation and input borders` | polish (wave 1 re-score) | M | Dark +0.5, Visual +0.1 |

## Problem

Both reviewers flagged it. Page `--background` is 0% lightness, `--card` is 4%, and input borders are 10% white, so
cards, nested boards and form fields separate only by hairlines (`tailwind/input.css:152`, `:155`, `:183`; seen on
`repo`, `blob`, `account_settings`, `project_board`, `settings`). Claude also found the saturated green Squash and
merge and Comment buttons read as neon next to an otherwise monochrome dark UI (`pr`).

## Scope

- Raise the card and popover tokens one step and the input border, and check nested surfaces (board columns, diff
  files) keep their own step.
- Offer a dimmer success tone for dark.
- This changes every page: take a screenshot diff of the full harness before and after.

## Files

- `tailwind/input.css`
- `mockups/` need no HTML change when the tokens are shared (they import `tailwind/input.css`)

## Acceptance criteria

- [x] Card, nested and input surfaces are distinguishable without relying on a 1px border
- [x] Contrast of text on the new surfaces stays at AA
- [x] Before/after harness screenshots, light unchanged

## Verification

Throwaway server at 1440×900 in light and dark, per the harness in [spec.md](../spec.md).

## Comments

2026-11-06 (wave 1 re-score): opened by ticket 22. Run `/triage` before any code.

2026-11-12 (triage): ready-for-agent. Restyles every page: run it alone, after 24, 25 and 31, with a full before/after screenshot diff.

2026-11-12 (wave 2 re-score): Wave 2 re-score: (Codex) near-black inputs, secondary buttons and nested surfaces on near-black cards have weak boundaries (`login`, `settings`, `repo-settings`); dark @mention links on comment surfaces are subdued blue, contrast unmeasured; (Claude) solid red buttons turn salmon with dark text while outlined reds keep their hue, and the blocked "Delete organization" button looks disabled-grey. Use a dedicated dark danger token with white text and measure link contrast at 4.5:1.
