# Prefix tsquery for repo, issue and pull request search

Created: 2026-10-09
Category: enhancement
Status: done

Spec: [../spec.md](../spec.md)

## What to build

- `prefixTSQuery(q string) string` in `internal/store/search_store.go`: letter/digit tokens, lowercased, each `token:*`, joined with ` & `; empty string when there are none.
- `SearchRepos`, `SearchIssues` and `SearchPulls` use `to_tsquery('english', $1)` with it in the `WHERE` and the `ts_rank`, and return nil without querying when it is empty.

## Acceptance criteria

- [x] A prefix of a repo name or description word, an issue title or body word, and a PR title or body word each find their row.
- [x] `alpha be` finds a row holding `alpha` and `beta`; `alpha zzz` does not.
- [x] `TestSearch_HidesWhatTheViewerCannotRead` still passes, and a private repo found by prefix is hidden from a stranger and anonymous viewer.
- [x] `"& | ! ( ) : * '"` and `"-"` return no results and no error from each of the three methods.

## Blocked by

None.
