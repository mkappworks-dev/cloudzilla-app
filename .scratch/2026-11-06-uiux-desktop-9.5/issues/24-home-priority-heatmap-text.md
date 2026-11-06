# Home priority and heatmap text alternative

Created: 2026-11-06
Category: enhancement
Status: needs-triage
Blocked by: —

| Branch | Worktree | PR title | Wave | Effort | Score gain |
| --- | --- | --- | --- | --- | --- |
| `fix/home-priority-heatmap-text` | `.worktrees/fix+home-priority-heatmap-text` | `fix(ui): home priority and heatmap text alternative` | polish (wave 1 re-score) | M | Visual +0.2, A11y +0.2 |

## Problem

Found by the wave 1 re-score (Codex).

1. Home lists twenty repositories before "Needs your attention", which pushes the urgent work far below the first
   viewport (`internal/view/pages/home.templ:90`, `:170`).
2. The contribution heatmap's days are non-focusable `div`s with a `title`. The image label gives only the total, so
   keyboard and screen-reader users get none of the per-day data (`internal/view/components/heatmap.templ:18`, `:30`).
   The same component renders on repo and profile pages.
3. (Claude) The year select sits orphaned right of the heading and the grid has a wide empty left gutter on `home`.

## Scope

- Move the attention section above the repository inventory, or cap the inventory with a "View all" link.
- Give the heatmap a text alternative (visually hidden table or list of the non-zero days) and keep the pointer
  tooltips.
- Tidy the year select and gutter alignment.

## Files

- `internal/view/pages/home.templ`
- `internal/view/components/heatmap.templ`
- `mockups/home.html`, `mockups/profile.html`, `mockups/repo.html`

## Acceptance criteria

- [ ] "Needs your attention" is visible in the first 900px at 1440 when there is anything to show
- [ ] A screen reader can read the heatmap's non-zero days on home, repo and profile
- [ ] Mocks updated, light and dark

## Verification

Throwaway server at 1440×900 in light and dark, per the harness in [spec.md](../spec.md).

## Comments

2026-11-06 (wave 1 re-score): opened by ticket 22. Run `/triage` before any code.
