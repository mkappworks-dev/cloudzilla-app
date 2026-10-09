# Search results match word prefixes

Created: 2026-10-09
Category: enhancement
Status: done

## Problem

`/search?q=` finds repos, issues and pull requests with `plainto_tsquery('english', q)`, which matches whole stemmed words only. Typing `bra` finds nothing, while the topnav suggestions (`GET /search/suggest`) already show `brave-…` for the same input, so the `Search for "bra"` row can lead to an empty page.

Example: with a repo named `brave-vault`, `/search?q=brave` finds it but `/search?q=bra` does not.

## Proposed design

Build a prefix tsquery in the store instead of `plainto_tsquery`:

- Split the query on anything that isn't a letter or digit, lowercase the tokens, and turn each into `token:*`, joined with `&`, then pass it to `to_tsquery('english', $1)`. Every token is a prefix, so a whole word still matches (`word:*` matches `word`) and results are a superset of today's.
- Tokens contain only letters and digits, so tsquery syntax (`& | ! ( ) : * '`) in user input can't reach `to_tsquery` or raise a syntax error.
- A query with no tokens returns no rows without querying.
- Applies to `SearchRepos`, `SearchIssues` and `SearchPulls` in `internal/store/search_store.go`, in both the `WHERE` and the `ts_rank` order. Visibility predicates are untouched.
- Unchanged: user search (already prefix), `/search/code` (`CodeSearchStore`), and the suggestions endpoint.

Cost accepted: prefix terms on a GIN index are slower than exact terms; the result limit stays 20.

## Acceptance criteria

- [x] `/search?q=<prefix of a word in a repo name, description, issue or PR title or body>` finds it; whole-word queries return what they did before.
- [x] A multi-word query matches when every word is a prefix of some word in the row.
- [x] A private repo, private issue and deleted repo stay hidden from the same viewers as before.
- [x] Queries made only of tsquery syntax characters or punctuation return no results and no error.
- [x] Tests cover the above in the store.

## Relevant files

- `internal/store/search_store.go`, `internal/store/search_visibility_test.go`
- `internal/service/search_service.go` (swallows store errors, which would hide a bad tsquery as empty results)
- `internal/db/migrations/025_search_indexes.sql` (`search_vector`, `english`)
