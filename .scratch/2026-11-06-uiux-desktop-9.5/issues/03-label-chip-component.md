# One LabelChip component with readable text

Created: 2026-11-06
Category: enhancement
Status: done
Blocked by: 00

| Branch | Worktree | PR title | Wave | Effort | Score gain |
| --- | --- | --- | --- | --- | --- |
| `feat/label-chip-component` | `.worktrees/feat+label-chip-component` | `feat(ui): shared LabelChip with contrast-safe text` | 1, lane C | S | Visual +0.4, A11y +0.3 |

## Problem

Labels are drawn at least five ways (`pr_list_row.templ:148`, `project_detail.templ:80`, `sidebar.templ:125`, `account_pulls.templ:160`, `discussions.templ:162`). List chips draw text in the label colour, so light colours ("enhancement" cyan) are unreadable in light mode. The board picks a readable foreground; the lists don't.

## Scope

- Add `components.LabelChip(name, hex string)` using the board's contrast logic (`project_detail.templ:80`), tuned for light and dark.
- Replace every call site listed above plus `issue_list_row.templ:51`, `issue_new.templ:279`, `pull_new.templ:226`, `discussion_detail.templ:258`; delete the duplicate `labelChipStyle` helpers.
- Unit test: every seeded label colour reaches 4.5:1 against its chip background in both themes.

## Files

- `internal/view/components/label_chip.templ (new)`
- the call sites above
- `mockups/*.html chips that copy the old style`

## Acceptance criteria

- [x] One chip implementation; `grep labelChipStyle` finds one helper
- [x] Contrast test passes for the seed palette in both themes
- [x] Mocks updated

## Verification

Screenshot issues, pulls, PR sidebar, board, discussions at 1440 light and dark.

## Comments

2026-11-06 (triage): ready-for-agent.

2026-11-06 (implementation): `components.LabelChip` fills the chip with the label colour in light mode and tints the
page with it in dark mode; per-theme colours travel as CSS variables in the inline style. The board's multi-select
renders the same chip, so `MultiSelectOption.Style` became `Color`. `seed.DefaultLabels` is exported for the test, which
also sweeps every `#rgb` colour.
