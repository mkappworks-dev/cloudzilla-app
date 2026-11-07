# PR review filter and review-state indicator

Created: 2026-11-07
Category: enhancement
Status: done
Blocked by: none (it read the protection rule that already exists, so it did not wait for 09)

| Branch | Worktree | PR title |
| --- | --- | --- |
| `feat/issue-pr-list-filters` | `.worktrees/feat+issue-pr-list-filters` | same PR as 01-03 |

Spec: [../spec.md](../spec.md). Shipped in the same PR as 01-03. Review state reads `branch_protections.require_review_count` per base branch and
`pull_reviews`, neither of which 09 changes.

## Scope

- Collapse a PR's reviews into one state: any changes requested → `changes_requested`; approvals ≥ required →
  `approved`; otherwise `review_required`. "Required" comes from the base branch's protection rule, so read it from
  whatever 09 settles on.
- A `review:` qualifier in `listquery` (`none`, `required`, `approved`, `changes_requested`), a Review menu on the PR list
  only, and a `ReviewState` field on `PRListRowData` drawn as an indicator shaped like the CI badge.
- Computed in SQL, so the filter and the row badge share one expression (`reviewStateExpr`); the per-base-branch
  requirement is resolved in Go (`filepath.Match`, as the merge check does) and passed in as arrays.
- Standing mocks: `mockups/pulls.html`; `COVERAGE.md`.

## Acceptance criteria

- [x] `review:` filters the PR list in SQL and pages correctly; tab counts stay unfiltered
- [x] Rows show the review state; the collapse rule has table-driven tests
- [x] Works with JavaScript disabled; back/forward and shared URLs reproduce the view
- [x] Standing mock updated and checked under `make mockups`; verified at 1440 in light and dark on a throwaway server
- [x] `/code-review` run

## Comments

2026-11-07 (maintainer): implement now instead of waiting for 09. Rule, as the merge check counts: any changes requested
→ `changes_requested`; else approvals ≥ max(required, 1) → `approved`; else required > 0 → `required`; else `none`. The
ghost user's reviews do not count. The row badge shows only on open, non-draft PRs.
