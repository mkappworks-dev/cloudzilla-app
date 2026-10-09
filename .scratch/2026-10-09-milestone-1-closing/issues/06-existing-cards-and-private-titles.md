# Clean up project cards that point at other repos, and private titles on boards

Created: 2026-10-09
Category: bug
Status: done

## Problem

#205 stops new project cards from referencing another repository's issue or pull request. Two gaps remain:

1. **Existing rows.** Cards created before #205 may still reference an issue or PR in a different repo, so the board keeps showing that title and state to readers of this repo. No data migration was written.
2. **Private issues in the same repo.** An issue with `visibility = 'private'` is visible only to its author and collaborators with write access. A card for it on a project board is shown with its title to anyone who can read the board. Not checked whether `ListCardsByColumn` filters by viewer.

## Where

- `internal/store/project_store.go` `ListCardsByColumn` and the board queries
- `project_cards`, `issues`, `pull_requests` tables

## Acceptance criteria

- [x] Count affected rows on a copy of production data: cards whose issue or pull request has a different `repo_id` than the project's repo. (No production copy was available; the migration's `WHERE` is the count query, so run it as a `SELECT count(*)` before deploying.)
- [x] A migration (next sequential number) deletes or detaches those cards; state the choice in the PR. (`113_drop_cross_repo_project_cards.sql` deletes them.)
- [x] Confirm by test whether a private issue's title reaches a non-writer through a board; if it does, hide the title and state (show a placeholder card) for viewers who can't see the issue.
- [x] Tests for both.

## Blocked by

Nothing.

## Comments

Claude, 2026-10-09: Triage found both gaps real, so neither was closed as `wontfix`.

1. Existing rows: nothing cleaned up cards from before #205. Migration 113 deletes them rather than detaching: the `project_cards` CHECK allows only an issue, a pull request, or a non-empty note, so detaching means a note, and the note would have to copy the foreign title that is the leak.
2. Private titles: `ListCardsByColumn` took no viewer and the board handler passed none, so a reader of a public repository saw every private issue's title, number and state. The store now applies `issueVisibleTo` and blanks those fields. The board renders a "Private issue" placeholder card (no number, no link).
