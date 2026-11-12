# Mocks stay design-first

Created: 2026-11-12
Category: enhancement
Status: done

Supersedes the mockgen effort (formerly `.scratch/2026-11-12-mockgen/`, which generated the standing mocks from templ).
Settles ticket 21 of [`../2026-11-06-uiux-desktop-9.5/`](../2026-11-06-uiux-desktop-9.5/spec.md) as `wontfix`.

## Problem

Ticket 21 proposed rendering the real templ components into `mockups/generated/` so a mock could not drift from its
template. Ticket 01 built that harness (#284) and tickets 02–07 were to migrate every page and delete the hand mocks.
The maintainer rejected the direction before 02–07 merged: a mock is where a UI is brainstormed, then the reference for
future work, then the reference for implementing the real templ files. Generating mocks from the templates inverts that
order, so the mock can only ever restate what was already built.

## Decisions

- **Hand-written mocks are the design source.** Nothing in `mockups/` or `.scratch/` is generated from templ, and
  nothing imports `internal/view` to make a mock.
- **Unbuilt designs** are an HTML file beside the spec they serve, in `.scratch/<date>-<slug>/`.
- **Standing mocks** of shipped pages live in `mockups/` (`make mockups`). A PR that changes a page updates its mock in
  the same PR (the rule already in `CLAUDE.md`); `mockups/COVERAGE.md` lists the pages with no mock.
- **Fidelity is checked by review, or by a diff someone runs**, not by generation. Ticket 20 raised mock fidelity to
  about 8.3 and the UI/UX plan wants 9.5; ticket 08 settled on review with a PR template line, no tooling.
- **Removed:** `cmd/mockgen`, `mockups/generated/`, `mockups/mock-ui.js`, the `mockups` CI job, `make mockups-generate`,
  the `mockgen:start/end` regions, and `tests/mockgen-diff`. The hand mocks for home and login are restored;
  `not_found` never had a mock and stays in `COVERAGE.md`.

## Tickets

| #   | Ticket                                   | Status                                              |
| --- | ---------------------------------------- | --------------------------------------------------- |
| 01  | Harness, CI and three proof pages        | `done`, then reverted by `tech/mockups-design-first` |
| 02  | Generate repo browsing mocks             | `wontfix`                                           |
| 03  | Generate issues, milestones, discussions | `wontfix`                                           |
| 04  | Generate pull request mocks              | `wontfix`                                           |
| 05  | Generate settings, wiki, projects, etc.  | `wontfix`                                           |
| 06  | Generate account and admin mocks         | `wontfix`                                           |
| 07  | Cutover: delete hand mocks and shell.js  | `wontfix`                                           |
| 08  | Cheap drift check for hand mocks         | `done`: review, no tooling                          |

## Out of scope

- Mobile.
- Changing shipped templs or services.

## Comments

2026-11-12 (maintainer): the unmerged branches `tech/mockgen-repo`, `-account`, `-issues`, `-pulls` and `-repo-extras`
and their PRs are closed by the maintainer, who also decides what happens to the branches.
