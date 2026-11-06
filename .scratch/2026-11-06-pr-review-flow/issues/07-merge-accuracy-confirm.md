# Merge accuracy and confirm

Created: 2026-11-06
Category: bug
Status: ready-for-agent

Spec: [../spec.md](../spec.md)

Folds in [ticket 30](../2026-11-06-uiux-desktop-9.5/issues/30-pr-merge-accuracy.md).

| Branch | Worktree | PR title |
| --- | --- | --- |
| `fix/pr-merge-accuracy` | `.worktrees/fix+pr-merge-accuracy` | `fix(ui): accurate merged-PR summary and merge confirmation` |

## What to build

- `fragments/pull_chrome.templ:74`: a merged or closed PR names the user who merged or closed it (from the timeline
  event) and the commits that merged, not the author and the supplied count.
- Merge opens a confirm dialog with the strategy and an editable commit title and message, defaulting to the current
  hard-coded text. `PATCH` takes `merge_title` / `merge_message` for `merge` and `squash`; `ff` shows no fields.
  `CodeService.ThreeWayMergePullRequest` and `SquashMergePullRequest` take the message instead of hard-coding it.
  Auto-merge keeps the default.

## Acceptance criteria

- [ ] A merged PR names its merger and the number of commits that merged
- [ ] Merging shows the strategy and message before it runs, and the commit carries the edited message
- [ ] Closing keywords in the edited message still close issues
- [ ] `mockups/pr.html` updated; checked under `make mockups`
- [ ] Ticket 30 `Status: done` with a comment pointing here
- [ ] Verified at 1440 px in light and dark

## Blocked by

03
