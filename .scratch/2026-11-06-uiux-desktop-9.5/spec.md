# Desktop UI/UX to 9.5

Created: 2026-11-06
Category: enhancement
Status: ready-for-agent

## Problem

A desktop-only audit of the 86 mocks in `mockups/` against the shipped templ pages, plus scoring by two independent
reviewers (Claude and Codex), puts the real app at **7.0/10** on desktop. Interaction feedback, edge states,
accessibility and feature completeness pull it down; visual design and dark mode are already strong. Mobile is out of
scope for this effort.

Evidence: [audit.md](./audit.md) (drift, mock-only features, bugs). Feature mocks:

- [`../2026-11-06-pr-review-flow/`](../2026-11-06-pr-review-flow/) — review dialog, merge box states, drafts, suggestions
- [`../2026-11-06-issue-pr-list-filters/`](../2026-11-06-issue-pr-list-filters/) — qualifiers, filter menus, pagination
- [`../2026-11-06-ci-pipelines/`](../2026-11-06-ci-pipelines/) — runs list, run detail + log viewer, PR checks
- [`../2026-11-06-package-registry/`](../2026-11-06-package-registry/) — container + generic packages
- [`../2026-11-06-security-signing-secrets/`](../2026-11-06-security-signing-secrets/) — signed commits, secret scanning

Each mock folder has a `README.md` with the routes it assumes, backend gaps and open questions. Compiled CSS and
screenshots are gitignored. To view a mock, build its CSS, serve the repo root and open the page:

```sh
bin/tailwindcss -i .scratch/<folder>/mock-input.css -o .scratch/<folder>/mock.css
python3 -m http.server 8000   # then /.scratch/<folder>/mock.html
```

## Scores (desktop, equal-weight mean of 8 dimensions)

| Dimension                | Real now | After wave 1 | After wave 2 | After wave 3 | After polish (22) |
| ------------------------ | -------- | ------------ | ------------ | ------------ | ----------------- |
| Visual design            | 7.9      | 8.6          | 8.6          | 8.6          | 9.5               |
| Information architecture | 7.75     | 7.75         | 8.75         | 8.75         | 9.5               |
| Interaction & feedback   | 6.0      | 7.5          | 8.0          | 8.0          | 9.5               |
| Empty / error / edge     | 6.2      | 7.1          | 7.9          | 7.9          | 9.5               |
| Accessibility            | 6.6      | 7.4          | 8.4          | 8.4          | 9.5               |
| Content & microcopy      | 6.85     | 8.2          | 8.2          | 8.2          | 9.5               |
| Dark mode                | 8.25     | 8.25         | 8.25         | 8.25         | 9.5               |
| Feature completeness     | 6.2      | 6.2          | 7.6          | 9.4          | 9.5               |
| **Overall**              | **7.0**  | **7.6**      | **8.2**      | **8.4**      | **9.5**           |

Each column adds only the gains the tickets below claim, so the scoped work reaches about 8.4. The last point comes from
ticket 22: re-score with both reviewers after each wave and turn every finding under 9.5 into a ticket. Gains are the
reviewers' estimates, not measurements.

Mocks score 7.0; mock↔real fidelity 7.6 (Claude 7.0, Codex 8.2). The mocks track (tickets 20–21) targets 9.5 for both.

## Plan

One ticket = one PR = one branch = one worktree = one Claude Code session. Branch and worktree names follow
CLAUDE.md. Create each worktree fresh from `origin/main`:

```sh
git fetch origin && git worktree add .worktrees/<type>+<slug> -b <type>/<slug> origin/main
```

Ticket 00 must merge first: worktrees branch from `origin/main`, so these tickets and mocks are invisible to them
until committed there.

### Wave 1 — quick wins (≈7.0 → 7.6)

Small PRs, under a day each. Lanes run in parallel; tickets in the same lane touch the same files, so run them in order.

| #   | Ticket                     | Branch                            | Lane | Blocked by     | Gain                  |
| --- | -------------------------- | --------------------------------- | ---- | -------------- | --------------------- |
| 00  | Commit plan and mocks      | `tech/uiux-plan-and-mocks`        | —    | —              | —                     |
| 01  | Merge box permissions      | `fix/pr-merge-box-permissions`    | A    | 00             | Interaction +0.3      |
| 02  | htmx loading states, toasts | `feat/htmx-loading-states`       | B    | 00             | Interaction +1.0      |
| 03  | Shared LabelChip           | `feat/label-chip-component`       | C    | 00             | Visual +0.4, A11y +0.3 |
| 04  | Avatar/initials consistency | `fix/avatar-initials-consistency` | D    | 00             | Visual +0.3           |
| 05  | List display bugs          | `fix/account-list-display-bugs`   | C    | 03             | Edge +0.6             |
| 06  | htmx fragment parity       | `fix/htmx-fragment-parity`        | B    | 02             | Interaction +0.2, Edge +0.3 |
| 07  | Accessibility quick fixes  | `fix/a11y-quick-fixes`            | A    | 01             | A11y +0.5             |
| 08  | Copy pass                  | `tech/ui-copy-pass`               | D    | 04, 05, 06, 07 | Content +1.35         |

### Wave 2 — core workflows (≈7.6 → 8.2)

| #   | Ticket                   | Branch                       | Blocked by | Gain                           |
| --- | ------------------------ | ---------------------------- | ---------- | ------------------------------ |
| 09  | PR review flow           | `feat/pr-review-flow`        | 01, 02     | Features +0.8, Interaction +0.5 |
| 10  | Issue & PR list filters  | `feat/issue-pr-list-filters` | 03, 05     | Features +0.4, IA +0.5         |
| 11  | Error recovery           | `fix/form-error-recovery`    | 02, 06     | Edge +0.8                      |
| 12  | Accessibility gate       | `tech/a11y-axe-gate`         | 07         | A11y +1.0                      |
| 13  | Notifications triage     | `feat/notifications-triage`  | 02         | IA +0.3, Features +0.2         |
| 14  | Superadmin settings access | `fix/superadmin-settings-access` | 00    | IA +0.2                        |

### Wave 3 — roadmap features (≈8.2 → 8.4)

Each starts with `/grill-with-docs` and `/to-spec` inside its mock folder, then `/to-tickets`; the ticket here only
tracks the effort.

| #   | Ticket                     | Branch (first PR)                 | Phase      | Gain          |
| --- | -------------------------- | --------------------------------- | ---------- | ------------- |
| 15  | Repository rename          | `feat/repo-rename`                | —          | Features +0.2 |
| 16  | Signed commit verification | `feat/phase-17.2-signed-commits`  | 17.2       | Features +0.3 |
| 17  | Secret scanning            | `feat/phase-17.3-secret-scanning` | 17.3       | Features +0.3 |
| 18  | CI pipelines               | `feat/phase-18.1-ci-runner`       | 18.1–18.3  | Features +0.6 |
| 19  | Package registry           | `feat/phase-16.1-container-registry` | 16.1–16.2 | Features +0.4 |

### Mocks track (any time, after 00)

| #   | Ticket                    | Branch                    | Gain                       |
| --- | ------------------------- | ------------------------- | -------------------------- |
| 20  | Mock drift and COVERAGE   | `tech/mockups-drift-fixes` | Mocks 7.0 → 8.3, fidelity → 8.8 |
| 21  | Generate mocks from templ | `tech/mockgen`            | Mocks → 9.5, fidelity → 9.5 |

### Polish loop (after each wave)

| #   | Ticket                 | Branch                       | Gain                      |
| --- | ---------------------- | ---------------------------- | ------------------------- |
| 22  | Re-score and polish    | `fix/uiux-polish-wave-<n>`   | Closes the gap to 9.5     |

Rule for every UI ticket: change the page's mock in `mockups/` in the same PR (CLAUDE.md).

## Hotspot files

Two tickets editing these at once will conflict; the lanes above already serialise them.

- `internal/view/layout/layout.templ` — 04 (header initials), 05 (bell badge), 08 (footer)
- `components/pr_list_row.templ`, `components/issue_list_row.templ` — 03, 05, 10
- `pages/account_pulls.templ`, `pages/account_issues.templ` — 03, 05, 08, 10
- `components/mergeability_box.templ`, `pages/pull_detail.templ` — 01, 07, 09

## Verification harness

Desktop screenshots of every mock/real pair at 1440px, light and dark, against a seeded throwaway server:

1. `docker run -d --name czui-pg -e POSTGRES_DB=czui -e POSTGRES_USER=cloudzilla -e POSTGRES_PASSWORD=test -p <port>:5432 postgres:18-alpine`
2. `cz-admin migrate` and `cz-admin seed` with `CZ_DATABASE_DSN` pointing at it (seed takes ~2 min), then run the server
   with a unique `CZ_SERVER_PORT`, `CZ_GIT_SSH_PORT` and `CZ_AUTH_COOKIE_NAME`.
3. `node mockups/server.js <port>` for the mocks.
4. Sign in as `admin@example.test` / `cloudzilla-seed`. The seeded superadmin owns `siteadmin/rusty-meadow` and the
   `paper-planes` org; use those for owner-only pages, `ada-silva/hollow-atlas` for busy repo pages.

## Comments

2026-11-06 (triage): Wave 1 tickets (01–08), 11–13, 20 and 22 are `ready-for-agent`. 09, 10, 15–19 and 21 are
`ready-for-human`: each needs a `/grill-with-docs` + `/to-spec` session first. 14 is `needs-info` until the superadmin
access model is decided.

2026-11-06 (maintainer): 14 decided as a time-boxed, audited admin mode (no blanket override), now `ready-for-human`.
18: built-in CI gets an Actions repo tab; the PR Checks tab stays the results view.
