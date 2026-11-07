# Merge box states and Close confirm

Created: 2026-11-06
Category: enhancement
Status: done

Spec: [../spec.md](../spec.md)

| Branch | Worktree | PR title |
| --- | --- | --- |
| `feat/pr-merge-box-states` | `.worktrees/feat+pr-merge-box-states` | `feat(pulls): merge box review states and auto-merge` |

## What to build

- `mergeStateOf` gains "needs approvals" (warning header, `1 of 2`) and "checks failing" (destructive header, Merge
  disabled, "View checks →", no auto-merge). Data: `PullReviewService.Counts` and the commit's required statuses.
- A "changes requested" state too: the live check showed a PR with two Changes requested reviews still reading "Ready to
  merge", though `CanMerge` refuses it with a 422.
- Auto-merge: "Enable auto-merge" split button in the needs-approvals and checks-pending states, and an enabled state
  naming who armed it and the strategy, with Disable. Uses `PATCH` `auto_merge=enable|disable` and
  `auto_merge_strategy`.
- Approving in the needs state flips the box to ready after the reload.
- `MergeabilityBoxData` gains `AutoMergeEnabled`, `AutoMergeStrategy` and `AutoMergeByName` (looked up from
  `AutoMergeBy`), filled by `PagePullDetail`. The Close button already opens `close-pull-dialog`.

## Acceptance criteria

- [x] Needs-approvals, ready, checks-failing, conflicts and auto-merge-enabled each render for a writer (view-model tests)
- [x] Read-only viewers see the lock line only
- [x] With no branch protection rule there is no "X of N" line
- [x] `mockups/pr.html` shows every state; checked under `make mockups`
- [x] Verified at 1440 px in light and dark

## Blocked by

01, 02
