# Local accessibility checks and keyboard support

Created: 2026-11-06
Category: enhancement
Status: done
Blocked by: 07

| Branch | Worktree | PR title | Wave | Effort | Score gain |
| --- | --- | --- | --- | --- | --- |
| `tech/a11y-axe-gate` | `.worktrees/tech+a11y-axe-gate` | `tech(a11y): desktop keyboard support and local axe checks` | 2 | M | A11y +1.0 |

## Problem

Accessibility regressions are caught only by hand. Custom selects in `repo_new.templ:188-213` have no keyboard handling; wiki reorder is drag-only (`wiki_shared.templ:28-121`); milestone tabs are `role="tab"` without arrow keys (`milestones.templ:35-58`).

## Scope

- Local Playwright checks against a migrated, seeded server on the desktop route list, failing on unallow-listed serious/critical violations. CI integration was removed from scope at the maintainer's request.
- Keyboard support for the repo_new selects (reuse `select-menu.js`), move up/down buttons for wiki reorder, arrow keys on milestone tabs.

## Files

- a Playwright script under a test directory
- `internal/view/pages/repo_new.templ`
- `internal/view/pages/wiki_shared.templ`
- `internal/view/pages/milestones.templ`

## Acceptance criteria

- [x] Local axe checks pass in both themes and detect an injected serious violation
- [x] Listed widgets usable by keyboard only

## Verification

Run the local Playwright harness; tab through each widget.

## Notes

See docs/testing.md for the local seeded-server commands. Accessibility checks do not run in CI.

## Comments

2026-11-06 (triage): ready-for-agent.

2026-11-06 (accessibility gate): Ticket 22: existing serious/critical findings outside this ticket's widgets are
recorded by route, theme, rule and selector in `tests/a11y/allowlist.js`. They include low-contrast text, accessible
names that omit visible text (including the shared account switcher and notification badge), invalid list and
description-list markup, prohibited ARIA attributes in notifications, and inline links distinguished only by color.
Remove the relevant entries when those pages are polished. The form error recovery, notification and admin-mode
sessions may also resolve entries in their files.

2026-11-06 (implementation): 159 local browser checks pass against a migrated, seeded production build in light and
dark at 1440px. The harness detects an injected serious image-alt violation. Go tests, lint, formatting and diff checks
pass. Standards and spec reviews ran with GPT-6.1 Sol at medium effort; findings were fixed. Verification is documented
in `../evidence/12/README.md`; screenshots are generated locally and are not committed.

2026-11-06 (maintainer scope change): removed the accessibility CI workflow and committed evidence PNGs. The local axe,
keyboard and screenshot harnesses remain available; the CI acceptance criterion is no longer applicable.

2026-11-06 (wave 1 re-score, ticket 22): Manual findings that axe will not catch, for the gate's allow-list review: muted
11–12px text on tinted rows looks under 4.5:1 in light (notifications actor line, "Showing 20 of 20", footer links,
heatmap legend; Claude, measure `--muted-foreground` against `--card` and `--muted`); pills and counters at 9–11px;
heatmap days with no text alternative (ticket 24); custom widget keyboard patterns (ticket 25). The polish PR made the
PR files diff headers real buttons (`role=button`, `tabindex`, `aria-expanded`).
