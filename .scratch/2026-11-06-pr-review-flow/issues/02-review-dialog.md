# Review dialog and entry points

Created: 2026-11-06
Category: enhancement
Status: ready-for-agent

Spec: [../spec.md](../spec.md)

| Branch | Worktree | PR title |
| --- | --- | --- |
| `feat/pr-review-dialog` | `.worktrees/feat+pr-review-dialog` | `feat(pulls): review dialog with approve and request changes` |

## What to build

- A `components.Dialog` with a summary textarea and Comment / Approve / Request changes radios in the
  `new_organization.templ` style. For the author, Approve and Request changes are disabled with a line explaining why.
  The description says line comments are already posted.
- Entry points: a prompt strip above the merge box (same classes as the "This pull request is closed." strip) and a
  success "Review changes" button at the right of the Files toolbar. Both show for a non-author writer on an open,
  non-draft PR; the author gets the dialog with Comment only.
- Submits `POST …/reviews` (form `state`, `body`); on success the page reloads with a toast.
- A Comment review must not replace the reviewer's standing Approve / Request changes: `PullReviewStore.Upsert` keeps one
  row per reviewer, so today a plain comment would clear a `changes_requested` block. Keep the verdict and update only
  the body (or store comments separately).
- The dark-mode `dark:prose-invert` fix in `fragments/line_comments.templ` is ticket 05's.

## Acceptance criteria

- [ ] Each of the three verdicts can be submitted and shows in the reviews list after the reload
- [ ] A Comment after Request changes leaves the merge blocked (service test)
- [ ] The author sees Approve and Request changes disabled and can submit Comment
- [ ] A read-only viewer sees neither the strip nor the button
- [ ] Empty summary is allowed for Approve, required for Comment and Request changes, with inline error text
- [ ] `mockups/pr.html` and `pr_files.html` show the dialog and entry points; checked under `make mockups`
- [ ] Verified at 1440 px in light and dark

## Blocked by

01
