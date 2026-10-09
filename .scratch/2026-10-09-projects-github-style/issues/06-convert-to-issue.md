# Convert a note card to an issue

Created: 2026-10-09
Category: enhancement
Status: done
Blocked by: 02, 03

Spec: [../spec.md](../spec.md)

## Acceptance criteria

- [x] `POST /cards/{cardID}/convert` creates an issue from the card's title and description, copies its labels and assignees, links the card and clears its title and note (create-then-link).
- [x] Only note cards without a link convert; others are rejected. Requires the same permission as creating an issue.
- [x] If linking the card fails the new issue is deleted and the card is unchanged; tests cover success, rejection and that rollback.
