# Projects list: clickable rows and New project dialog

Created: 2026-10-09
Category: enhancement
Status: ready-for-agent

Spec: [../spec.md](../spec.md)

## Acceptance criteria

- [ ] Clicking anywhere on a project row opens `/{owner}/{repo}/projects/{id}`; Delete still works without navigating.
- [ ] "New project" opens a dialog with name and description; submit creates the board and lands on its page.
- [ ] Empty state and signed-out hint still render; the inline form is gone.
