# PR review flow

Created: 2026-11-06
Category: enhancement
Status: done

Parent: [ticket 09](../2026-11-06-uiux-desktop-9.5/issues/09-pr-review-flow.md). Design input: [mock.html](./mock.html)
and [README.md](./README.md). Docs to read first: [pr-merge](../../docs/pr-merge.md),
[notifications](../../docs/notifications.md).

## Problem

The backend already supports reviews (`POST /reviews`), auto-merge, drafts and applying a suggestion, but the PR page
renders almost none of it. There is no Approve / Request changes control, the merge box can't say why a PR can't merge
yet, auto-merge and convert-to-draft live only in the stale `fragments/pull_detail.templ`, and `/line_comments/{id}/apply`
has no UI. A merged PR's header names the wrong merger and a commit count of 0, and merging gives no look at the commit
message first.

## Decisions

Settled with the maintainer on 2026-11-07.

- **Write access is required to review.** `POST /reviews` keeps `writableRepoJSON`. Readers keep the lock line and
  comment through the normal comment box.
- **The author may leave a comment-only review.** `PullReviewService.SubmitReview` rejects `approved` and
  `changes_requested` from the author and allows `commented`, in the service rather than only the UI. The dialog's
  disabled radios and explanation line match. The author's own review fires no `pr_review` notification
  (`actorID == authorID`).
- **The Review prompt shows for any non-author writer** on an open, non-draft PR, with or without a branch protection
  rule. With no rule (`RequiredReviews == 0`) the merge box shows no "X of N" line.
- **Auto-merge is offered in "needs approvals" and "checks pending" only.** Failing required checks keep Merge disabled
  with "View checks →" and no auto-merge, since arming it would wait forever for a push.
- **The review dialog is flat**: summary plus verdict. Line comments already post on their own and reviews don't link to
  them, so the description line says so. A real pending-review model is a separate spec.
- **Suggestions**: single Apply and a persisted applied state first (migration adds `applied_at`, `applied_sha`,
  `applied_by` to `pull_line_comments`); batch "Commit suggestions" last, droppable without blocking anything.
- **Ticket 30 folds in** as the last ticket: the merged-PR header names the real merger and commit count, and Merge
  opens a confirm dialog showing the strategy and an editable commit title and message. The message applies to `merge`
  and `squash` only; a fast-forward creates no commit.
- **Dark-mode fix included**: `fragments/line_comments.templ` gets `dark:prose-invert`.
- **Out of scope**: pending-review model, review by readers, the 390 px shell-header overflow (`mockups/shell.js`).

## Backend gaps

| Gap                                                                                                  | Ticket |
| ---------------------------------------------------------------------------------------------------- | ------ |
| Author reviews rejected outright (`pull_review_service.go:40`)                                       | 01     |
| `SubmitReview` HTMX path renders the stale `fragments.PRReviews`                                     | 01     |
| `fragments.PullDetail` stale on `PATCH` (draft toggle, auto-merge, merge, close)                     | 01     |
| `MergeabilityBoxData` lacks auto-merge state, "armed by" name, review/checks states (`CanWrite` exists) | 03     |
| Suggestion body not rendered by `fragments.LineComments`; no applied columns                         | 05     |
| No batch endpoint; each apply is its own commit and `ErrRefMoved` (409) races the next               | 06     |
| Merged-PR header uses the author and the supplied commit count; merge commit message is hard-coded   | 07     |

## Tickets

Each is one PR, in this order. Every PR updates the standing mocks in `mockups/` (`pr.html`, `pr_files.html`) for what it
ships, and checks them under `make mockups`.

| #   | Ticket                                                           | Blocked by |
| --- | ---------------------------------------------------------------- | ---------- |
| 01  | [Review flow backend groundwork](./issues/01-backend-groundwork.md)  | none       |
| 02  | [Review dialog and entry points](./issues/02-review-dialog.md)       | 01         |
| 03  | [Merge box states and Close confirm](./issues/03-merge-box-states.md) | 01, 02    |
| 04  | [Draft toggles](./issues/04-draft-toggles.md)                        | 03         |
| 05  | [Apply suggestion and applied state](./issues/05-suggestion-apply.md) | 02        |
| 06  | [Batch-apply suggestions](./issues/06-suggestion-batch.md)           | 05         |
| 07  | [Merge accuracy and confirm](./issues/07-merge-accuracy-confirm.md)  | 03         |

02–04 and 07 all edit `components/mergeability_box.templ` and `pages/pull_detail.templ`, so they stay sequential. 05 and
06 touch `pages/pr_files.templ` and `fragments/line_comments.templ` and may run beside 03–04, but 05 after 02 (the Files
toolbar button).

## Rules for every ticket

- Verify at 1440 px in **light and dark** on a throwaway server (see the
  [parent spec](../2026-11-06-uiux-desktop-9.5/spec.md#verification-harness)); `ada-silva/hollow-atlas` is the busy repo.
- Update `docs/pr-merge.md` and `docs/notifications.md` when behaviour they describe changes.
- Take the next free migration number after 117 (ticket 14 holds it), re-checked after merging main.

## Comments

**Malith Kuruppu, 2026-11-07:** Grilled against the mock README's open questions; every recommendation accepted.
