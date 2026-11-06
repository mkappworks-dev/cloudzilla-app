# Re-score and polish after each wave

Created: 2026-11-06
Category: enhancement
Status: ready-for-agent
Blocked by: 08 (wave 1), 14 (wave 2), 19 (wave 3)

| Branch | Worktree | PR title | Wave | Effort | Score gain |
| --- | --- | --- | --- | --- | --- |
| `fix/uiux-polish-wave-<n>` | `.worktrees/fix+uiux-polish-wave-<n>` | `fix(ui): polish findings from wave <n> re-score` | after each wave | M | closes the gap to 9.5 |

## Problem

The scoped tickets reach about 8.4. The rest is the long tail the reviewers find on the next pass.

## Scope

- Re-run the desktop harness (spec.md) and both reviewers (Claude and Codex) with the same brief as the first audit.
- Append scores to spec.md under Comments.
- Turn every finding that keeps a dimension under 9.5 into a ticket in this folder; small ones go straight into the polish PR.

## Files

- varies

## Acceptance criteria

- [ ] Scores recorded per wave
- [ ] Each dimension ≥ 9.5 by both reviewers after wave 3

## Verification

Same harness and brief as the 2026-11-06 audit.

## Comments

2026-11-06 (triage): ready-for-agent.
