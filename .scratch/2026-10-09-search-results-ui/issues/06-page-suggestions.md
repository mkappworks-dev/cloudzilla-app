# Suggestions on the page's search box

Created: 2026-10-09
Category: enhancement
Status: done
Blocked by: none

## What

Typing in the results page's own input opens the same dropdown as the topnav. `topnav-search.js` looks up `#topnav-suggest` by id and `fragments.SearchSuggestions` hard-codes the `suggest-N` option ids, so a second instance would collide on both. Parameterise the Alpine component (list element passed in, or found inside its own scope) and the option id prefix, then add the listbox beside the existing clear button, which keeps its `x-data q` scope. On the results page the topnav field is hidden below `md` and visible above it, so both exist at once; only one dropdown may be open at a time.

## Acceptance criteria

- [x] Typing 2+ characters on `/search` opens suggestions under the page input; arrows, Enter, Escape and click-outside behave as in the topnav.
- [x] The topnav dropdown still works on every page, including `/search`.
- [x] Option ids are unique in the document (checked in the browser).
- [x] The clear button clears the query and the dropdown.
- [x] Browser check: Alpine state asserted, since a hidden pane stalls transitions.

## Relevant files

`cmd/server/frontend/static/topnav-search.js`, `internal/view/fragments/search_suggestions.templ`, `internal/view/layout/` (topnav form), `internal/view/pages/search.templ`, `internal/handler/search_page_handler.go`
