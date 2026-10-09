# Clean up project cards that point at other repos, and private titles on boards

Created: 2026-10-09
Category: bug
Status: needs-triage

## Problem

#205 stops new project cards from referencing another repository's issue or pull request. Two gaps remain:

1. **Existing rows.** Cards created before #205 may still reference an issue or PR in a different repo, so the board keeps showing that title and state to readers of this repo. No data migration was written.
2. **Private issues in the same repo.** An issue with `visibility = 'private'` is visible only to its author and collaborators with write access. A card for it on a project board is shown with its title to anyone who can read the board. Not checked whether `ListCardsByColumn` filters by viewer.

## Where

- `internal/store/project_store.go` `ListCardsByColumn` and the board queries
- `project_cards`, `issues`, `pull_requests` tables

## Acceptance criteria

- [ ] Count affected rows on a copy of production data: cards whose issue or pull request has a different `repo_id` than the project's repo.
- [ ] A migration (next sequential number) deletes or detaches those cards; state the choice in the PR.
- [ ] Confirm by test whether a private issue's title reaches a non-writer through a board; if it does, hide the title and state (show a placeholder card) for viewers who can't see the issue.
- [ ] Tests for both.

## Blocked by

Nothing.
