# PR review flow

Created: 2026-11-06
Category: enhancement
Status: ready-for-human
Blocked by: 01, 02, 07

| Branch | Worktree | PR title | Wave | Effort | Score gain |
| --- | --- | --- | --- | --- | --- |
| `feat/pr-review-flow` | `.worktrees/feat+pr-review-flow` | `feat(pulls): review dialog, merge states, drafts and suggestions` | 2 | L | Features +0.8, Interaction +0.5 |

Mock: `../2026-11-06-pr-review-flow/` (13 states)

## Problem

The PR page has no Approve / Request changes control though `POST /reviews` exists; auto-merge and convert-to-draft live only in stale `fragments/pull_detail.templ`; apply-suggestion (`/line_comments/{id}/apply`) has no UI.

## Scope

- Run `/grill-with-docs` then `/to-spec` in `.scratch/2026-11-06-pr-review-flow/`, then `/to-tickets` there; this ticket closes when that spec's tickets are done.
- Backend gaps from the mock README: author comment-only reviews (`pull_review_service.go:40` rejects all author reviews), reviewers without write access, stale HTMX responses (`fragments.PRReviews`, `fragments.PullDetail`), `MergeabilityBoxData` auto-merge fields and review-required/checks-failing states, suggestion rendering + applied columns, batch apply.

## Files

- see the mock README

## Acceptance criteria

- [ ] Spec and tickets exist in the mock folder
- [ ] All its tickets done
- [ ] `mockups/pr.html`, `pr_files.html` updated

## Verification

Per the spec.

## Notes

Split into several PRs via its own tickets: review dialog, merge box states, drafts, suggestions.

## Comments

2026-11-06 (triage): ready-for-human; spec needs a `/grill-with-docs` session with the maintainer first.
