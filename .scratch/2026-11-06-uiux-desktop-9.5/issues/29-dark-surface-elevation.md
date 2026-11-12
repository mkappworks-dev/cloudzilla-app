# Dark mode surface elevation and input borders

Created: 2026-11-06
Category: enhancement
Status: ready-for-agent
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

- [ ] Card, nested and input surfaces are distinguishable without relying on a 1px border
- [ ] Contrast of text on the new surfaces stays at AA
- [ ] Before/after harness screenshots, light unchanged

## Verification

Throwaway server at 1440×900 in light and dark, per the harness in [spec.md](../spec.md).

## Comments

2026-11-06 (wave 1 re-score): opened by ticket 22. Run `/triage` before any code.

2026-11-12 (triage): ready-for-agent. Restyles every page: run it alone, after 24, 25 and 31, with a full before/after screenshot diff.
