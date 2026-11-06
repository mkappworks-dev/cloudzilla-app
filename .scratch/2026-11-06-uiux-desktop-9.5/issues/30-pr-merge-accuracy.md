# Accurate merged-PR summary and merge confirmation

Created: 2026-11-06
Category: bug
Status: needs-triage
Blocked by: 09

| Branch | Worktree | PR title | Wave | Effort | Score gain |
| --- | --- | --- | --- | --- | --- |
| `fix/pr-merge-accuracy` | `.worktrees/fix+pr-merge-accuracy` | `fix(ui): accurate merged-PR summary and merge confirmation` | polish (wave 1 re-score) | M | Interaction +0.3, Edge +0.2 |

## Problem

Found by the wave 1 re-score (Codex).

1. A merged PR's header says its author "merged 0 commits" while the timeline names a different merger; the summary
   always uses the author and the supplied commit count (`internal/view/fragments/pull_chrome.templ:74`).
2. Merge runs the moment the primary button is pressed: no look at the resulting commit message or strategy first
   (`internal/view/components/mergeability_box.templ:118`, `:132`).

The wave 1 polish PR already made the "ready" subtitle truthful (it claimed passed checks and approvals it never
checked).

## Scope

- Merged and closed PRs: name the actual merger and the real commit count.
- Merge: a confirm step (or inline fields) for the commit title/message, with strategy shown. Coordinate with 09, which
  owns the merge box.

## Files

- `internal/view/fragments/pull_chrome.templ`, `internal/view/components/mergeability_box.templ`
- `mockups/pr.html`

## Acceptance criteria

- [ ] A merged PR names its merger and the number of commits that merged
- [ ] Merging shows the commit message and strategy before it runs

## Verification

Throwaway server at 1440×900 in light and dark, per the harness in [spec.md](../spec.md).

## Comments

2026-11-06 (wave 1 re-score): opened by ticket 22. Run `/triage` before any code.
