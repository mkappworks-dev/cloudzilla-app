# Organizations in search results

Created: 2026-10-09
Category: enhancement
Status: ready-for-agent
Blocked by: none

## What

Search finds organizations. Add `SearchStore.SearchOrgs` (prefix of `name` or `display_name`, limit 20, ordered by name), `SearchResults.Orgs`, and accept `type=orgs` in `SearchService.Search` and `PageSearch`. Add the `Organizations` tab with a count. Rendering is ticket 04.

## Acceptance criteria

- [ ] `SearchOrgs("bra")` returns `brave-software` and an org whose display name starts with a `bra…` word; an unrelated org is excluded.
- [ ] `type=all` and `type=orgs` populate `Results.Orgs`; other types leave it empty.
- [ ] LIKE wildcards in the query (`%`, `_`) are escaped, as `SuggestOrgs` does through `likePrefix`.
- [ ] Store test against `TEST_DATABASE_DSN`.

## Relevant files

`internal/store/search_store.go`, `internal/service/search_service.go`, `internal/handler/search_handler.go`, `internal/view/viewmodels_social.go`
