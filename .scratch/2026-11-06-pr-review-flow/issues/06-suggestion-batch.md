# Batch-apply suggestions

Created: 2026-11-06
Category: enhancement
Status: done

Spec: [../spec.md](../spec.md)

| Branch | Worktree | PR title |
| --- | --- | --- |
| `feat/pr-suggestion-batch` | `.worktrees/feat+pr-suggestion-batch` | `feat(pulls): batch several suggestions into one commit` |

## What to build

- "Add to batch" / "Remove from batch" on a pending suggestion, and a "Commit suggestions" button in the Files toolbar
  showing the batch size. The batch lives in the page (Alpine), not the database.
- A batch endpoint that applies many suggestions in one `CodeService` commit, bottom-up per file so line numbers stay
  valid, advancing the branch through one `gitref.Move`. A conflict between two suggestions on the same line fails the
  whole batch with a message naming them.
- Marks every comment applied with the same `applied_sha`.

## Acceptance criteria

- [x] Three suggestions across two files become one commit, all marked applied
- [x] Overlapping suggestions fail the batch and change nothing
- [x] A moved head branch returns 409 and changes nothing
- [x] `mockups/pr_files.html` shows the batch states; checked under `make mockups`
- [x] Verified at 1440 px in light and dark

## Comments

Droppable: if it grows past one PR, mark it `wontfix` and keep 05.

## Blocked by

05
