# Projects list: clickable rows and New project dialog

Created: 2026-10-09
Category: enhancement
Status: done

Spec: [../spec.md](../spec.md)

## Acceptance criteria

- [x] Clicking anywhere on a project row opens `/{owner}/{repo}/projects/{id}`; Delete still works without navigating.
- [x] "New project" opens a dialog with name and description; submit creates the board and lands on its page.
- [x] Empty state and signed-out hint still render; the inline form is gone.

## Comments

claude, 2026-10-09: A click on empty space in a row opened the board in the browser on a running server (twice, including the final-review fix wave). Delete sitting above the overlay (`relative z-10` wrapper) was confirmed by code review only; nobody clicked Delete in a browser.
