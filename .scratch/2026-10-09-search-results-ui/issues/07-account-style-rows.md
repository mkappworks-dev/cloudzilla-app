# Rows match the account pages

Created: 2026-10-09
Category: bug
Status: done
Blocked by: 03-shared-list-rows

## What

Ticket 03 took its row markup from the repo-level issues page and Explore, so search results didn't match `/repos`, `/issues` and `/pulls`. Restyle the shared rows to the account pages' design (hover accent bar, icon + title + badges line, muted meta line with `·` separators and stats on the right) and have every list use them:

- `RepoListRow` and `IssueListRow` carry the account pages' optional fields (role, language, topics, stats; labels, priority, comments) and a `LiAttrs` hook for the client-side filter.
- `/repos`, `/issues`, the repo-level issues page, Explore and search all render them; `PRListRow` was already this design.
- `PersonListRow` follows `orgListRow` (large avatar, name, badge, subtitle).
- Both Public and Private badges show, as on `/repos`.

## Acceptance criteria

- [x] `/repos` and `/issues` render through the shared rows with filtering, role, language, topics, stats, labels, priority and comment counts intact.
- [x] Search repos, issues, PRs, users and orgs look like those pages (checked in the browser, signed in as the seed admin).
- [x] Row tests cover the optional fields; `make lint` and `go test ./...` pass.

## Notes

The repo-level issues page and Explore change look: they now use the account design too.
