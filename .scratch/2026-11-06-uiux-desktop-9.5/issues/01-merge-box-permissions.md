# Show merge controls only to writers

Created: 2026-11-06
Category: bug
Status: done
Blocked by: 00

| Branch | Worktree | PR title | Wave | Effort | Score gain |
| --- | --- | --- | --- | --- | --- |
| `fix/pr-merge-box-permissions` | `.worktrees/fix+pr-merge-box-permissions` | `fix(pulls): gate merge box actions on write access` | 1, lane A | S | Interaction +0.3 |

Mock: `../2026-11-06-pr-review-flow/mock.html#viewer=reader` shows the read-only line.

## Problem

The merge box (merge buttons and Close) renders for any viewer of an open PR; neither `pull_detail.templ:123-127` nor `pull_detail_page_handler.go:108-127` checks `CanWrite`. Its Close (`mergeability_box.templ:97`) fires `hx-patch` with no confirm, while the Close under the comment editor asks first.

## Scope

- Add `CanWrite` to `MergeabilityBoxData`; read-only viewers see the mergeability status and a one-line explanation, no buttons.
- Remove Close from the merge box or route it through the existing `close-pull-dialog`.
- Server side already rejects non-writers (`UpdatePull` → `writableRepoJSON`, `pull_handler.go:151`); keep it that way and add a handler test asserting the page omits the buttons for a reader.

## Files

- `internal/view/components/mergeability_box.templ`
- `internal/view/pages/pull_detail.templ`
- `internal/handler/pull_detail_page_handler.go`
- `internal/view/viewmodels_pull.go`
- `mockups/pr.html`

## Acceptance criteria

- [x] A reader sees no Merge or Close in the merge box
- [x] A writer sees both; Close asks for confirmation
- [x] Test covers reader and writer
- [x] `mockups/pr.html` shows the writer state and COVERAGE lists the reader state

## Verification

Router page-check harness (memory: router-page-check-harness) as reader and writer.

## Comments

2026-11-06 (triage): ready-for-agent.
