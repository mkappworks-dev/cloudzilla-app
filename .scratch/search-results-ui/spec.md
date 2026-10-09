# Search results page UI

Created: 2026-10-09
Category: enhancement
Status: done

## Problem

`/search?q=brave` renders plain stacked lists that look unlike the rest of the app and show less than the topnav suggestions do:

- No Organizations group, although the dropdown suggests orgs.
- Rows differ from the main list pages: issues link to `/issues/N` with no repo (the store never loads the repo owner or name), PRs have no link at all, and repos show a Public badge the dropdown omits.
- The matched text isn't marked, so with prefix matching (#198) it is not obvious why `bra` matched `acme/brave-core`.
- The page's own search box has no suggestions, unlike the topnav one.

## Decisions

- Results look like the main pages' rows, not the dropdown's compact rows: the issue list row, `components.PRListRow`, the Explore repo row, and a new person/org row.
- Rows are extracted into shared components that the main pages also use, so the search page is not a third copy to keep in sync.
- Organizations become a group and a `type=orgs` tab.
- Matches are highlighted in names and titles only, never descriptions or bodies.
- The page's search box gets the topnav suggestions dropdown.

## Design

**Orgs.** `SearchStore.SearchOrgs(ctx, query, limit)` matches a prefix of name or display name (same predicate as `SuggestOrgs`, 20 rows). Orgs are public, so no viewer filter. `SearchResults.Orgs`, `type=orgs`, a tab count, and a group between Repositories and Issues.

**Result context.** `SearchIssues` and `SearchPulls` already join `repositories`; select `r.owner_name`, `r.name` into the existing `Issue.RepoOwner/RepoName` and `PullRequest.RepoOwner/RepoName` fields so rows can link to `/{owner}/{repo}/issues/{n}` and `/{owner}/{repo}/pulls/{n}`. Add `created_at` formatting in the view layer only.

**Row components** in `internal/view/components/`:

- `IssueListRow` extracted from `pages/issues.templ`; the repo issues page and search both render it. Search passes a repo prefix (`owner/repo`) shown before `#N`.
- `RepoListRow` extracted from `pages/explore.templ`; Explore keeps its stars, forks and created date, search shows the Private badge and omits stats it doesn't have.
- `PRListRow` reused as is; search leaves CI, labels and reviewers empty.
- `PersonListRow` (avatar, name, bio or display name) for users and orgs.

**Highlight.** `components.Highlight(text, query)` renders text with `<mark class="bg-highlight …">` around each word that starts with a query token, case-insensitively. It builds output from escaped segments, never `templ.Raw`. Tokenisation matches `prefixTSQuery` in the store (letters and digits).

**Page suggestions.** Reuse `topnavSearch` and `/search/suggest` on the results page's input. `topnav-search.js` finds `#topnav-suggest` by id; parameterise it so two instances don't collide, then add the listbox next to the existing clear button.

## Tickets

1. `01-org-results`
2. `02-result-context`
3. `03-shared-list-rows`
4. `04-results-page-rows` (blocked by 01, 02, 03)
5. `05-match-highlight` (blocked by 04)
6. `06-page-suggestions` (independent)

## Out of scope

Filter sidebar, sorting, pagination, code search (`/search/code`), highlighting descriptions.

## Relevant files

- `internal/handler/search_handler.go`, `internal/view/pages/search.templ`, `internal/view/viewmodels_social.go`
- `internal/service/search_service.go`, `internal/store/search_store.go`
- `internal/view/pages/issues.templ`, `internal/view/pages/explore.templ`, `internal/view/components/pr_list_row.templ`
- `internal/view/fragments/search_suggestions.templ`, `cmd/server/frontend/static/topnav-search.js`
