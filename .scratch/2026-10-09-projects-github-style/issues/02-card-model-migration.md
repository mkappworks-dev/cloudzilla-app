# Card model and migration

Created: 2026-10-09
Category: enhancement
Status: ready-for-agent

Spec: [../spec.md](../spec.md)

## Acceptance criteria

- [ ] Migration (next number after `110`) adds `title` and `due_date` to `project_cards`, and creates `card_assignees` and `card_labels` with `ON DELETE CASCADE` on card, user and label.
- [ ] The old `CHECK` is replaced: at most one of `issue_id`/`pull_id`; a card with neither has a non-empty `title`.
- [ ] Backfill moves each existing note's first line (≤120 chars) into `title` and the remainder into `note`; a migration test covers a long first line, a single-line note and a multi-line note.
- [ ] `model.ProjectCard`, store reads and writes carry the new fields; `KanbanCardView` exposes them.
