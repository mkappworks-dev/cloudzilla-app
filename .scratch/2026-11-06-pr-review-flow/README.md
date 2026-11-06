# PR review flow — mock

Give the PR page the review and merge controls the backend already supports but the shipped UI doesn't render: a
"Review changes" dialog (Comment / Approve / Request changes), a merge box that explains why a PR can't merge yet and
offers auto-merge, draft toggles, a confirm before Close, and Apply / batch actions on suggestion line comments. The
mock builds on `pages/pull_detail.templ`, `pages/pr_files.templ`, `components/mergeability_box.templ` and
`fragments/line_comments.templ`, and reuses their classes.

Open `mock.html` over HTTP from the repo root (`python3 -m http.server`, then `/.scratch/2026-11-06-pr-review-flow/mock.html`).
Rebuild the CSS with `bin/tailwindcss -i .scratch/2026-11-06-pr-review-flow/mock-input.css -o .scratch/2026-11-06-pr-review-flow/mock.css`.
The dashed bar sets the state. It can also be set from the hash, e.g. `#viewer=author&merge=auto&tab=files&sugg=batched`.
Screenshots go in `shots/` (gitignored, not committed).

## States

- Viewer: reviewer (write access), author, read-only.
- PR: open, draft. Draft adds the existing banner, a muted "Draft" merge box with Ready for review, and the Draft badge.
- Merge box: needs approvals (warning header with `1 of 2` and an "Enable auto-merge" split button), ready (current
  split button), checks failing (destructive header, merge disabled, "View checks →"), conflicts (no merge button, a
  how-to-fix line), and auto-merge enabled (who armed it, which strategy, Disable). Read-only viewers get a lock line
  instead of buttons. Close always opens `close-pull-dialog`.
- Review dialog (`components.Dialog`): summary textarea plus three radios in the `new_organization.templ` style. For
  the author, Approve and Request changes are disabled and a line explains why. Approving in the needs state flips the
  box to ready.
- Review entry points: a prompt strip above the merge box (same classes as the "This pull request is closed." strip)
  and a success button at the right of the Files toolbar.
- Sidebar: "Convert to draft" at the bottom, for writers and the author on an open PR.
- Files tab suggestion: pending (Apply suggestion, Add to batch), in batch (badge, Remove from batch, and a "Commit
  suggestions" button in the toolbar), applied (badge, the diff line updated, and "applied this in `sha`").

## What backs each control

| Control                    | Backed by                                                                                        | Missing                                                                                                                                                                                               |
| -------------------------- | ------------------------------------------------------------------------------------------------ | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Review dialog submit       | `POST /api/repos/{o}/{r}/pulls/{n}/reviews` → `SubmitReview` (form or JSON `state`, `body`)       | Its HTMX response renders the stale `fragments.PRReviews`. Needs a no-swap response plus toast/reload. Requires `writableRepoJSON`, so read-only users can't comment-review.                             |
| Author can only "Comment"  | —                                                                                                | `PullReviewService.SubmitReview` rejects **every** review by the author (`pull_review_service.go:40`), including `commented`. Relax it to allow `commented`, or route the author's submit to `/comments`. |
| "1 of 2 required reviews"  | `PullReviewService.Counts` → `MergeabilityBoxData.RequiredReviews/ApprovedReviews`                | A header state for "review required" and one for "checks failing": `mergeStateOf` only knows unavailable/conflicts/up-to-date/ready/checking.                                                        |
| Enable / Disable auto-merge | `PATCH …/pulls/{n}` with `auto_merge=enable|disable`, `auto_merge_strategy`; `tryAutoMerge`     | `MergeabilityBoxData` has no `AutoMergeEnabled/Strategy/By` (they're on `model.PullRequest`). The "armed by" name needs a lookup from `AutoMergeBy`. The HTMX path renders the stale `fragments.PullDetail`. |
| Merge gating by permission | `PATCH` merge already needs `repo:write` + `CheckContentWritable`                                | `MergeabilityBox` takes no `CanWrite`, so every viewer sees merge/Close buttons today. Add it to the data and render the read-only footer.                                                            |
| Close confirm              | `close-pull-dialog` already in `pull_detail.templ`                                               | Only the box's Close button needs `showModal()` instead of `hx-patch`.                                                                                                                                 |
| Ready for review / Convert to draft | `PATCH …/pulls/{n}` with `is_draft`                                                     | Same stale `fragments.PullDetail` HTMX render. The page hides the merge box while drafted; the mock shows a muted draft box instead.                                                                  |
| Apply suggestion           | `POST …/line_comments/{id}/apply` → `ApplySuggestion` (right side, open PR, `HX-Redirect`)        | No UI at all, and `fragments.LineComments` doesn't render `SuggestionBody`.                                                                                                                            |
| Add to batch / Commit suggestions | —                                                                                         | No batch endpoint. Each apply is its own commit, and `ErrRefMoved` → 409 means a second apply can race the first. A batch needs one `CodeService` commit over many hunks.                              |
| Applied state              | —                                                                                                | `pull_line_comments` has no `applied_at` / `applied_sha` / `applied_by`, so the "applied in `sha`" state can't survive a reload.                                                                      |

## Open questions

- Should read-only users be able to leave a comment-only review (GitHub allows it), or is `writableRepoJSON` intended?
- Required reviews come from branch protection only. With no rule, does the Review prompt still show? The mock shows it
  for any non-author writer.
- Auto-merge with failing checks: offer it (it waits for a re-run) or keep merge disabled as mocked?
- Should the review dialog carry pending line comments (a real "pending review" model)? Today line comments post on
  their own and reviews have no link to them, so the description line in the dialog overstates what happens.
- Dark mode: `fragments/line_comments.templ` renders comment bodies with `prose prose-xs` but no `dark:prose-invert`, so
  inline `code` is near-invisible on dark. The mock avoids inline code there; the port should fix it.
- The shell header overflows to about 490 px at 390 px wide in every mock (`mockups/shell.js`, not this page), so
  mobile screenshots are clipped to the viewport.
