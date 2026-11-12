# Mockgen cutover

Created: 2026-11-12
Category: enhancement
Status: ready-for-agent
Blocked by: 02, 03, 04, 05, 06

| Branch                 | Worktree                         | PR title                                              | Effort |
| ---------------------- | -------------------------------- | ----------------------------------------------------- | ------ |
| `tech/mockgen-cutover` | `.worktrees/tech+mockgen-cutover` | `tech(mockups): remove hand-written mocks of shipped pages` | S      |

## Problem

See [spec.md](../spec.md). Once every family is generated, the leftovers are dead weight.

## Scope

- Delete `mockups/shell.js`, `markdown-editor.js`, `comment-editor.js` and any hand mock still present.
- Tidy `mockups/README.md`, `mockups/COVERAGE.md` notes, `index.html` and `CLAUDE.md`'s CSS section line about
  `mockups/`; run `npm run fmt`.
- Set ticket 21 of `.scratch/2026-11-06-uiux-desktop-9.5/` to `Status: done` with its criteria ticked, and update that
  spec's mocks track.

## Files

- `mockups/**`, `CLAUDE.md`, `.scratch/2026-11-06-uiux-desktop-9.5/issues/21-mockgen.md`

## Acceptance criteria

- [ ] `mockups/` holds no hand-written page of a shipped route
- [ ] Every page in `index.html` is generated and links resolve
- [ ] Ticket 21 is `done`
- [ ] CI `mockups` and `Format` jobs green

## Verification

Walk `index.html` under `make mockups` at 1440 in light and dark.

## Comments
