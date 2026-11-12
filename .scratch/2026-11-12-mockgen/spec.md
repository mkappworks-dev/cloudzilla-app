# Generate standing mocks from templ

Created: 2026-11-12
Category: enhancement
Status: ready-for-agent

Settles ticket 21 of [`../2026-11-06-uiux-desktop-9.5/`](../2026-11-06-uiux-desktop-9.5/spec.md) (grilled with the
maintainer on 2026-11-12). Tickets 24, 25, 29 and 31–40 of that effort take their mock changes from the generator once
ticket 01 here lands.

## Problem

The 91 hand-written files in `mockups/` copy classes from `.templ` files by hand, so they drift: ticket 20 raised
fidelity to about 8.3 and the plan needs 9.5. A generator that renders the real components removes the copying.

## Decisions

- **Binary.** `cmd/mockgen` is a separate binary that imports the real `layout`, `pages`, `fragments` and `components`
  packages and the `view` view-models, never copies of them. The server binary never links it. Shipped code is not
  changed to make a page render: a page that cannot render from a plain view-model is skipped and listed in
  `COVERAGE.md`.
- **Fixtures.** Hand-built Go view-models in `cmd/mockgen`, no database. Fixed IDs, names, avatar keys and a frozen
  clock, so output is byte-deterministic.
- **Output.** `mockups/generated/<page>[--<variant>].html`, committed, one file per variant. Routes stay extensionless
  (`/generated/issues`).
- **No runtime in the mocks.** Output is the real `layout.Base` markup with the htmx and Alpine `<script>` tags removed
  (the `x-*` and `hx-*` attributes stay and do nothing). Interaction (dropdowns, dialogs, tabs, theme toggle) comes from
  one hand-written `mockups/mock-ui.js` shared by every page. `assets.URL` hashes become stable paths; `main.css` is
  built by `server.js` as today.
- **Viewing needs only Node.** `make mockups` serves committed files. `make mockups-generate` runs Go and is needed only
  to regenerate.
- **Variants.** A registry (route, component, fixture, viewer) is the single source for the files, `index.html` and the
  generated region of `COVERAGE.md`. Viewers: `anon`, `viewer`, `writer`, `admin`, and admin mode (banner). A page gets
  a role variant only if its templ branches on it. Every list page gets `--empty`; pages with an error render get
  `--error`. The default (no suffix) is the happy path as `writer`. States that were `#hash` toggles in hand mocks
  become variants.
- **Light and dark.** One file per variant; the real theme bootstrap (`cz-theme`, else `prefers-color-scheme`) picks the
  theme. Checked visually under `make mockups` at 1440 in both.
- **Hand-written HTML** stays only for unshipped designs in `.scratch/`. Server-only states (busy spinner, failed-save
  toast) stay undrawn and listed in `COVERAGE.md`.
- **Docs.** `index.html` and `COVERAGE.md` each hold a generated region between `<!-- mockgen:start -->` and
  `<!-- mockgen:end -->`; the rest stays hand-written. The cutover (07) makes `index.html` wholly generated. Generated markdown is Prettier-stable (checked with
  `npm run fmt:check`).
- **CI.** A `mockups` job in `ci.yml`: Go from `go.mod`, the same action versions as the other jobs, `go run ./cmd/mockgen`, then
  `git diff --exit-code -- mockups/generated mockups/index.html mockups/COVERAGE.md`. No Postgres, no Node.
- **Fidelity check (local, per PR).** `tests/mockgen-diff/` fetches the live route as the seeded admin from a throwaway
  server, normalises CSRF tokens, asset hashes and timestamps, and compares structure (tags, classes, attributes) with
  the generated file; text is ignored. Documented in `docs/testing.md` beside the a11y harness. Not in CI.
- **Migration.** Each page-family PR deletes the hand mocks it replaces. `shell.js`, `markdown-editor.js` and
  `comment-editor.js` go in the cutover PR.

## Out of scope

- A generated component gallery (`mockups/generated/components/`): possible later, not planned.
- Mobile.
- Changing shipped templs or services.

## Tickets

| #   | Ticket                                    | Branch                     | Blocked by |
| --- | ----------------------------------------- | -------------------------- | ---------- |
| 01  | Harness, CI and three proof pages         | `tech/mockgen`             | —          |
| 02  | Generate repo browsing mocks              | `tech/mockgen-repo`        | 01         |
| 03  | Generate issues, milestones, discussions  | `tech/mockgen-issues`      | 01         |
| 04  | Generate pull request mocks               | `tech/mockgen-pulls`       | 01         |
| 05  | Generate settings, wiki, projects, releases, gists | `tech/mockgen-repo-extras` | 01 |
| 06  | Generate account and admin mocks          | `tech/mockgen-account`     | 01         |
| 07  | Cutover: delete hand mocks and shell.js   | `tech/mockgen-cutover`     | 02–06      |

02–06 can run in parallel; they touch disjoint pages and meet only in the registry, `index.html` and `COVERAGE.md`
region, which are regenerated, so a conflict is resolved by regenerating.

## Comments
