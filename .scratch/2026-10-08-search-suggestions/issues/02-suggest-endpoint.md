# GET /search/suggest returns the suggestions fragment

Created: 2026-10-08
Category: enhancement
Status: done

Spec: [../spec.md](../spec.md)

## What to build

- `SearchSuggest` in a new `internal/handler/search_suggest_handler.go`: read `q`, take the viewer from the claims, call `Suggest`, render `fragments.SearchSuggestions` through `withKnownAvatars(r, userAvatarKeys(users))`. Sets `Cache-Control: private, no-store`.
- View-model in `internal/view/viewmodels_search.go` (or the existing search view-model file) and `internal/view/fragments/search_suggestions.templ`: `role="listbox"`, grouped rows with stable `id="suggest-N"`, `role="option"`, Private badge, avatars, and the final `Search for "q"` row. An empty `q` renders nothing. Run `make generate-templ`.
- Route `r.With(optAuthMW).Get("/search/suggest", h.SearchSuggest)` beside `/search`.
- `classifyRequest` in `internal/middleware/api_rate_limit.go` returns `ResourceSearch` for the path.

## Acceptance criteria

- [x] Router test (page-check harness): an anonymous request for a private repo's prefix is 200 and omits it; the owner's request includes it.
- [x] `q=a` is 200 with no options; `q=<script>` is escaped in the `Search for` row.
- [x] The response carries `Cache-Control: private, no-store`.
- [x] `{"GET", "/search/suggest", ResourceSearch, true}` is in the `classifyRequest` table test.

## Blocked by

01
