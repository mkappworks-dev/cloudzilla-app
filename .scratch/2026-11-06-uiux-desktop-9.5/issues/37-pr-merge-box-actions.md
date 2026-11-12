# Pull request merge box: dismiss, resolve, viewer state

Created: 2026-11-12
Category: enhancement
Status: needs-triage
Blocked by: —

| Branch | Worktree | PR title | Wave | Effort | Score gain |
| --- | --- | --- | --- | --- | --- |
| `feat/pr-merge-box-actions` | `.worktrees/feat+pr-merge-box-actions` | `feat(ui): pull request merge box: dismiss, resolve, viewer state` | polish (wave 2 re-score) | M | Features +0.3, Interaction +0.2 |

## Problem

Found by the wave 2 re-score (Claude and Codex).

1. The merge box says a blocked PR waits "until they approve or the review is dismissed", but neither a repository owner nor an admin has a Dismiss review action, or an override, so the blocker cannot be cleared (Claude, `admin-merge-box-changes`).
2. A conflicted PR offers no "Resolve conflicts" or "Update branch" action, and no guidance: no file list, no command, no way to recheck (Claude and Codex, `pull-conflicts`).
3. A viewer without write access sees a green "Ready to merge" header and checkmarks but no button, with the reason only in the footer (Claude, `pull-ready`).
4. "Close pull request" appears in both the merge box and the comment footer, and "Ready for review" twice on a draft (Claude and Codex).

## Scope

- Dismiss review (with a reason, audited), an update-branch action when the base moved, conflict guidance, a neutral "Ready, waiting for a maintainer" header for non-writers, one close and one ready action.
- The wave 2 polish PR already lists every blocker as a row when conflicts and requested changes coincide.

## Files

- `internal/view/components/mergeability_box.templ`, `pages/pull_detail.templ`
- review and pull services
- `mockups/pr.html`

## Acceptance criteria

- [ ] Owners and admins can dismiss a review
- [ ] Conflict and viewer states say what to do next
- [ ] No duplicated actions
- [ ] Mocks updated, light and dark

## Verification

Throwaway server at 1440×900 in light and dark, per the harness in [spec.md](../spec.md).

## Comments

2026-11-12 (wave 2 re-score): opened by ticket 22. Run `/triage` before any code.
