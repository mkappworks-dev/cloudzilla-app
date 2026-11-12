# Home priority and heatmap text alternative

Created: 2026-11-06
Category: enhancement
Status: done
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

- [x] "Needs your attention" is visible in the first 900px at 1440 when there is anything to show
- [x] A screen reader can read the heatmap's non-zero days on home, repo and profile
- [x] Mocks updated, light and dark
- [x] Wave 2: lightest heatmap level is 3:1 against the card in light mode, and home and profile have a legend

## Verification

Throwaway server at 1440×900 in light and dark, per the harness in [spec.md](../spec.md).

## Comments

2026-11-06 (wave 1 re-score): opened by ticket 22. Run `/triage` before any code.

2026-11-12 (triage): ready-for-agent. Run after the wave 2 re-score (22).

2026-11-12 (wave 2 re-score): Wave 2 re-score (Claude): the contribution heatmap's lowest levels are nearly invisible in light mode and profile heatmaps have no legend (`user-profile`, `home`). Raise the lowest level to 3:1 and add a legend while the text alternative is added.
