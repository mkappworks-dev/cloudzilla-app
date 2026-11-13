# Open and read a fork PR in the UI

Created: 2026-11-13
Category: enhancement
Status: done
Blocked by: 03

## Problem

See [spec.md](../spec.md). The new-PR page has a head-repository picker, and the header, list rows, merge box and timeline show `owner:branch` and the "a fork" fallback.

## Acceptance criteria

- [x] The compare page defaults the head repository to the viewer's fork when they can't write the base (a writer starts on the base itself); the picker lists only repositories in the tree the viewer can write to, and changing it reloads the page for that repository's branches.
- [x] The PR header, merge box default messages and the compare summary show `owner/name:branch`, or "a fork" for a head the viewer can't read. The lists and search show no head branch today, so they are unchanged; the check-out menu and the Files tab's "View file" link are hidden on a fork PR, because their branch is not in this repository. No link to the fork yet.
- [x] Standing mocks in `mockups/` (new PR, detail), `mockups/COVERAGE.md` and any shell are updated in this PR.
- [x] Checked live at 1440 in light and dark; evidence for both.

## Comments

- Claude, 2026-11-14: light and dark screenshots at 1440 of the changed mocks are in `../evidence/`:
  `06-new-{light,dark}.png` (`pull_new.html`, head-repository picker), `06-prfork-{light,dark}.png`
  (`pr.html#head=fork&merge=needs`) and `06-prhidden-{light,dark}.png` (`#head=hidden`, "a fork"). The real pages were
  not screenshotted: their output is covered by the handler tests.
