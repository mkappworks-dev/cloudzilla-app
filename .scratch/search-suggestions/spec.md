# Topnav search suggestions

Created: 2026-10-08
Category: enhancement
Status: ready-for-agent

## Problem

`#topnav-search` (`internal/view/layout/layout.templ`) only submits to `/search`. Finding a repo, user or org means typing the full query, loading the results page and clicking through.

Example: typing `clou` in the topnav shows nothing until Enter. The full-text `search_vector` behind `SearchStore.SearchRepos` matches whole stemmed words, so even the results page finds nothing for the prefix.

## Proposed design

`GET /search/suggest?q=` returns an HTML fragment with a few top repos, users and orgs under the input, filtered to what the viewer may see.

- **Matching is prefix**, case-insensitive: repo `name` and `owner/name`, username, org `name` and `display_name`. LIKE wildcards in `q` are escaped so `%` and `_` match literally.
- **Visibility reuses `readableBy` / `viewerID`** (`internal/store/repo_store.go`), as `SearchRepos` does, plus `deleted_at IS NULL`. Users exclude the ghost via `notGhost`. Orgs are public, as `/{owner}` already shows them to anyone.
- **Limits**: `q` trimmed; under 2 characters returns an empty fragment; capped at 100 characters. 4 repos, 3 users, 3 orgs.
- **Errors are swallowed** per group like `SearchService.Search`; the `Search for "q"` row still shows.
- **Route**: `r.With(optAuthMW).Get("/search/suggest", h.SearchSuggest)` in `internal/router/router.go`. Not reachable before setup (the field is hidden when `SetupPending`), so `internal/middleware/setup.go` is unchanged. `classifyRequest` charges it to `ResourceSearch`.
- **Response varies by viewer**: `Cache-Control: private, no-store`.
- **UI**: the input becomes an ARIA combobox. `hx-get`, `hx-trigger="input changed delay:200ms"`, `hx-sync="this:replace"` so a stale response can't overwrite a newer one. Alpine state (`open`, `active`) lives on the `<form>`. ArrowDown/ArrowUp move and wrap, Enter follows the active row or submits the form when none is active, Escape closes and keeps the text, click outside closes. Rows: repo `owner/name` with a Private badge and description, user and org with `components.Avatar` (handler wraps the request with `withKnownAvatars` like `PageSearch`), and a last `Search for "q"` row linking to `/search?q=`.
- **Shortcut**: ⌘K stays with the command palette (`CommandKHook`, `layout.templ`). `/` focuses the field, ignored while typing in an input, textarea, select or contenteditable, and with modifier keys. The `⌘K` badge in the input becomes `/`.
- **Parallel branch** `feat/search-clear-and-wider-topnav` (not on origin yet) widens the input (`w-72`, `focus:w-96`) and adds `x-data="{ q: '' }"`. Our Alpine component goes on the `<form>`; when that branch merges first, rebase, fold `q` in and keep its widths. The dropdown is `absolute left-0 right-0` under the input wrapper, so it follows the width.

No migration or model change.

Out of scope: issue and PR suggestions, recent searches, highlighting the matched prefix, a mobile search UI.

## Acceptance criteria

- [ ] Typing 2+ characters shows matching repos, users and orgs under the input; fewer shows nothing.
- [ ] A private repo is suggested to its owner and to a user with a `permissions` row, and never to a stranger or an anonymous viewer. A soft-deleted repo is never suggested.
- [ ] Arrow keys, Enter and Escape work as described; the last row runs the full search.
- [ ] `/` focuses the field outside text inputs; ⌘K still opens the palette.
- [ ] Rapid typing never leaves a stale result list showing.
- [ ] Tests cover the store visibility rules, the short-query path, the handler (including HTML escaping) and the `classifyRequest` row.

## Relevant files

- `internal/store/search_store.go`, `internal/store/repo_store.go` (`readableBy`, `viewerID`), `internal/store/search_visibility_test.go`
- `internal/service/search_service.go`
- `internal/handler/search_handler.go`
- `internal/router/router.go`, `internal/middleware/api_rate_limit.go`
- `internal/view/layout/layout.templ`, `internal/view/fragments/`, `internal/view/components/avatar.templ`

## Decisions

- 2026-10-08: ⌘K stays with the command palette; `/` focuses search (maintainer's choice).
