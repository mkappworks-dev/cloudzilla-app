# PR review filter and review-state indicator

Created: 2026-11-07
Category: enhancement
Status: ready-for-human
Blocked by: 09 (`../../2026-11-06-uiux-desktop-9.5/issues/09-pr-review-flow.md`)

| Branch | Worktree | PR title |
| --- | --- | --- |
| `feat/pr-review-filter` | `.worktrees/feat+pr-review-filter` | `feat(pulls): review filter and review-state indicator` |

Spec: [../spec.md](../spec.md). Split out of the list-filters PR because review state needs the protection rule per base
branch and 09 is reworking the merge box and review flow.

## Scope

- Collapse a PR's reviews into one state: any changes requested → `changes_requested`; approvals ≥ required →
  `approved`; otherwise `review_required`. "Required" comes from the base branch's protection rule, so read it from
  whatever 09 settles on.
- A `review:` qualifier in `listquery` (`none`, `required`, `approved`, `changes_requested`), a Review menu on the PR list
  only, and a `ReviewState` field on `PRListRowData` drawn as an indicator shaped like the CI badge.
- Decide in the grill whether the state is computed in SQL (needed to filter and page correctly) or per page.
- Standing mocks: `mockups/pulls.html`; `COVERAGE.md`.

## Acceptance criteria

- [ ] `review:` filters the PR list in SQL and pages correctly; tab counts stay unfiltered
- [ ] Rows show the review state; the collapse rule has table-driven tests
- [ ] Works with JavaScript disabled; back/forward and shared URLs reproduce the view
- [ ] Standing mock updated and checked under `make mockups`; verified at 1440 in light and dark on a throwaway server
- [ ] `/code-review` run

## Comments

2026-11-07 (Claude): open question from the mock README: does "required" read the protection rule per base branch, or a
repo-wide setting? Settle it once 09 merges.
