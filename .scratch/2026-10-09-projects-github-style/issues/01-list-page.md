# Projects list: clickable rows and New project dialog

Created: 2026-10-09
Category: enhancement
Status: ready-for-agent

Spec: [../spec.md](../spec.md)

## Acceptance criteria

- [ ] Clicking anywhere on a project row opens `/{owner}/{repo}/projects/{id}`; Delete still works without navigating.
- [x] "New project" opens a dialog with name and description; submit creates the board and lands on its page.
- [x] Empty state and signed-out hint still render; the inline form is gone.

## Comments

claude, 2026-10-09: First criterion left unticked. The row is a stretched link (`after:absolute after:inset-0`) with Delete in a `relative z-10` wrapper, but the task 6 browser check only screenshotted the list page; nobody clicked a row or Delete. Click through once on a running server, then tick it and set `Status: done`.
