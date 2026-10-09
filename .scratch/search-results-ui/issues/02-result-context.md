# Repo context on issue and PR results

Created: 2026-10-09
Category: bug
Status: ready-for-agent
Blocked by: none

## What

Issue and PR results can't link to their item: the page links issues to `/issues/N` (a route without a repo) and renders PR titles as plain text. `SearchIssues` and `SearchPulls` already join `repositories r`; select `r.owner_name` and `r.name` into `Issue.RepoOwner/RepoName` and `PullRequest.RepoOwner/RepoName` (the model comment says these exist for exactly this case). Fix the existing links in `search.templ` in the same change so it is useful before the redesign lands.

## Acceptance criteria

- [ ] Results carry `RepoOwner` and `RepoName`.
- [ ] Issue results link to `/{owner}/{repo}/issues/{n}`; PR titles link to `/{owner}/{repo}/pulls/{n}`.
- [ ] Store test asserts the owner and repo names for an issue and a PR in different repos.

## Relevant files

`internal/store/search_store.go`, `internal/model/issue.go`, `internal/model/pull.go`, `internal/view/pages/search.templ`
