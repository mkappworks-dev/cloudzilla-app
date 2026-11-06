# Review flow backend groundwork

Created: 2026-11-06
Category: enhancement
Status: done

Spec: [../spec.md](../spec.md)

| Branch | Worktree | PR title |
| --- | --- | --- |
| `feat/pr-review-flow` | `.worktrees/feat+pr-review-flow` | `feat(pulls): PR review flow spec and backend groundwork` |

## What to build

- This PR also carries `spec.md` and the tickets.
- `PullReviewService.SubmitReview` allows `commented` from the PR author and still rejects `approved` and
  `changes_requested` from them, with a message naming why. Unknown states are rejected.
- `h.SubmitReview` (HTMX): answer with a success toast and the same-page reload mechanism ticket 23 settled
  (`redirectAfterSave`), not the stale `fragments.PRReviews`. JSON callers are unchanged.
- `UpdatePull` HTMX paths (draft toggle, auto-merge enable/disable, merge, close, reopen): answer with a toast and a
  reload instead of `fragments.PullDetail`; delete `fragments.PullDetail` and `fragments.PRReviews` once nothing renders
  them.
- `MergeabilityBoxData.CanWrite`, the read-only footer and the Close confirm dialog already shipped with ticket 01 of the
  desktop effort, so they are not redone here; the auto-merge fields move to ticket 03.

## Acceptance criteria

- [x] The author's `commented` review is stored; their `approved` / `changes_requested` return 422 (tests)
- [x] No `pr_review` notification for the author's own review (existing `actorID == authorID` rule, now documented)
- [x] An HTMX review submit answers 204 with no body; the review dialog (ticket 02) reloads the page
- [x] Merge, close, reopen, draft toggle and auto-merge answer HTMX with 204; the merge box buttons reload instead of
      swapping a stale fragment; `fragments.PullDetail` and `fragments.PRReviews` are deleted
- [x] `docs/pr-merge.md` and `docs/notifications.md` updated; spec and tickets committed
- [x] No visible change in the mocks (the merge box markup is the same), so `mockups/` is untouched

## Blocked by

None.
