# Issue and PR list filters and pagination

Created: 2026-11-06
Category: enhancement
Status: ready-for-human
Blocked by: 03, 05

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

- [ ] Spec and tickets exist in the mock folder
- [ ] All its tickets done
- [ ] `mockups/issues.html`, `pulls.html` updated

## Verification

Per the spec; benchmark with the large-repo harness (memory: large-repo-benchmark-harness) for SQL paging.

## Comments

2026-11-06 (triage): ready-for-human; spec needs a `/grill-with-docs` session with the maintainer first.
