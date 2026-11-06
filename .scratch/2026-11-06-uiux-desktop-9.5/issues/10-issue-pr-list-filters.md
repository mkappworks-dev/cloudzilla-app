# Issue and PR list filters and pagination

Created: 2026-11-06
Category: enhancement
Status: done
Blocked by: 03, 05, 08

| Branch | Worktree | PR title | Wave | Effort | Score gain |
| --- | --- | --- | --- | --- | --- |
| `feat/issue-pr-list-filters` | `.worktrees/feat+issue-pr-list-filters` | `feat(issues,pulls): qualifier search, filter menus and pagination` | 2 | M | Features +0.4, IA +0.5 |

Mock: `../2026-11-06-issue-pr-list-filters/` (11 states)

## Problem

The repo issue and PR lists load every row, the PR list has no search or filters, and the issues page advertises qualifiers its handler ignores.

## Scope

- `/to-spec` in `.scratch/2026-11-06-issue-pr-list-filters/`, then `/to-tickets`.
- Backend gaps from the mock README: qualifier parser for `q`, multi-label, milestone by title, author/assignee/`no:` filters, filtering and paging pushed into SQL, `ListWithCIStatus(…, limit, offset)`, priority and comment count on rows.

## Files

- see the mock README

## Acceptance criteria

- [x] Spec and tickets exist in the mock folder
- [x] All its tickets done
- [x] `mockups/issues.html`, `pulls.html` updated

## Verification

Per the spec; benchmark with the large-repo harness (memory: large-repo-benchmark-harness) for SQL paging.

## Comments

2026-11-06 (triage): ready-for-human; spec needs a `/grill-with-docs` session with the maintainer first.

2026-11-06 (wave 1 re-score, ticket 22): Both reviewers listed this: repo PR lists have no search, filters, sorting or
pagination (Codex, `pulls.templ:29`, `:48`, +0.6 Features); issue filters need the Apply button, so they could submit on
change (Claude, `issues.templ`). The polish PR only made the empty state filter-aware ("No open issues match these
filters." with a Clear filters link); keep it when the filters land.

2026-11-07 (maintainer grill + Claude): decisions are in `.scratch/2026-11-06-issue-pr-list-filters/spec.md`, split into
tickets 01 (parser), 02 (issues), 03 (PRs). `review:` and the PR review-state indicator are deferred until 09 merges;
saved filters are out. Close this ticket when 03 merges.

2026-11-07 (Claude): all three tickets ship in one PR (#279); `mockups/issues.html`, `issues_closed.html`, `pulls.html`
and a new `issues_filtered.html` are updated. Status set to done in that PR.
