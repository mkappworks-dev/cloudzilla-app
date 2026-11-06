# Accessibility gate in CI

Created: 2026-11-06
Category: enhancement
Status: ready-for-agent
Blocked by: 07

| Branch | Worktree | PR title | Wave | Effort | Score gain |
| --- | --- | --- | --- | --- | --- |
| `tech/a11y-axe-gate` | `.worktrees/tech+a11y-axe-gate` | `tech(ci): axe accessibility check on desktop routes` | 2 | M | A11y +1.0 |

## Problem

Accessibility regressions are caught only by hand. Custom selects in `repo_new.templ:188-213` have no keyboard handling; wiki reorder is drag-only (`wiki_shared.templ:28-121`); milestone tabs are `role="tab"` without arrow keys (`milestones.templ:35-58`).

## Scope

- A CI job: seed a DB, start the server, run axe-core via Playwright on the desktop route list in the spec's harness, fail on serious/critical.
- Keyboard support for the repo_new selects (reuse `select-menu.js`), move up/down buttons for wiki reorder, arrow keys on milestone tabs.

## Files

- `.github/workflows/*.yml`
- a Playwright script under a test directory
- `internal/view/pages/repo_new.templ`
- `internal/view/pages/wiki_shared.templ`
- `internal/view/pages/milestones.templ`

## Acceptance criteria

- [ ] CI job green on main and fails on an injected violation
- [ ] Listed widgets usable by keyboard only

## Verification

Run the job locally; tab through each widget.

## Notes

Check docs/testing.md for what CI runs; seeding is local-only today, so the job needs its own seed step.

## Comments

2026-11-06 (triage): ready-for-agent.
