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

- [x] Scores recorded per wave (wave 1, wave 2)
- [ ] Each dimension ≥ 9.5 by both reviewers after wave 3

## Verification

Same harness and brief as the 2026-11-06 audit.

## Comments

2026-11-06 (triage): ready-for-agent.

2026-11-06 (wave 1): scored by Claude and Codex, 8.00 and 7.25 (mean 7.63); details in spec.md Comments. Small findings
fixed in `fix/uiux-polish-wave-1`, the rest are tickets 24–31, and findings in running tickets' files are comments on
10–14 and 20. Stays `ready-for-agent`: waves 2 and 3 repeat it, and the "≥ 9.5 after wave 3" criterion is open.

2026-11-12 (wave 2): scored by Claude and Codex, 7.25 and 7.59 (mean 7.42), below the 8.2 target and wave 1's 7.63;
details and harness notes in spec.md Comments. Small findings fixed in `fix/uiux-polish-wave-2`, the rest are tickets
32–40, and findings in open tickets' files are comments on 24, 25, 27, 28, 29 and 31. Stays `ready-for-agent`: wave 3
repeats it, and the "≥ 9.5 after wave 3" criterion is open.
