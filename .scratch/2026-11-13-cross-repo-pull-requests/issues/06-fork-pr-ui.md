# Open and read a fork PR in the UI

Created: 2026-11-13
Category: enhancement
Status: ready-for-agent
Blocked by: 03

## Problem

See [spec.md](../spec.md). The new-PR page has a head-repository picker, and the header, list rows, merge box and timeline show `owner:branch` and the "a fork" fallback.

## Acceptance criteria

- [ ] The compare page defaults the head repository to the viewer's fork of this repository; the picker lists only repositories in the tree the viewer can write to.
- [ ] PR header, lists and search show `owner:branch` when the head differs and link to the fork when readable.
- [ ] Standing mocks in `mockups/` (new PR, detail), `mockups/COVERAGE.md` and any shell are updated in this PR.
- [ ] Checked live at 1440 in light and dark; evidence for both.
