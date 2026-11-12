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

Mocks score 7.0; mock↔real fidelity 7.6 (Claude 7.0, Codex 8.2). The mocks track (ticket 20; 21 was dropped) targets 9.5 for both.

Measured so far (mean of both reviewers; details under Comments): 7.0 baseline, **7.63** after wave 1, **7.42** after
wave 2. The wave 2 pass used full-page shots of every state and both reviewers saw all of them, so it is stricter than
wave 1's: treat 7.42 as the baseline for what follows.

## Run order

The current order, updated after each re-score. Rows in one step run as parallel sessions; a ticket also waits for its
**Depends on**. Ticket numbers stay IDs.

| Step | Tickets                    | Runs as                                         | Status                                      |
| ---- | -------------------------- | ----------------------------------------------- | ------------------------------------------- |
| ✅    | 00–14, 20, 22 (waves 1–2), 23–26, 28–33, 35–40 | —                  | merged (24, 32, 33, 38, 39: #291–#295; 25, 31, 35, 36, 37, 40: #296–#301; 26, 28: #303, #304; 29: #302) |
| 6    | 27, 34, 15                 | builds from their specs in `.scratch/2026-11-12-edit-and-release-workflows/`, `.scratch/2026-11-12-unfinished-settings-surfaces/` and `.scratch/2026-11-12-repo-rename/` | running now: 27/01, 27/04, 34/01, 15/01; held until 34/01 merges: 34/02, 34/04; the rest wait on their `Blocked by` lines |
| 6    | 16, 17, 18, 19             | spec sessions any time; builds here             | ready-for-human (spec sessions needed)      |
| 7    | 22 (wave 3)                | after 19                                        | re-score and polish                         |
| —    | 21 mock generator          | —                                               | wontfix: mocks stay hand-written            |

Polish tickets update the hand mocks in `mockups/` in the same PR as the page (ticket 21, the generator, is `wontfix`).

## Plan

One ticket = one PR = one branch = one worktree = one Claude Code session. Branch and worktree names follow
CLAUDE.md. Create each worktree fresh from `origin/main`:

```sh
git fetch origin && git worktree add .worktrees/<type>+<slug> -b <type>/<slug> origin/main
```

Ticket 00 must merge first: worktrees branch from `origin/main`, so these tickets and mocks are invisible to them
until committed there.

Ticket numbers are IDs, not an order. Start a ticket once every ticket in **Depends on** has merged. **Step** is the
earliest point it can start: rows with the same step can run as parallel sessions. Within a step, take wave 1 first,
because the waves are ordered by score gain per effort.

### Wave 1 — quick wins (≈7.0 → 7.6)

Small PRs, under a day each.

| Step | #   | Ticket                      | Branch                            | Depends on     | Gain                        |
| ---- | --- | --------------------------- | --------------------------------- | -------------- | --------------------------- |
| 0    | 00  | Commit plan and mocks       | `tech/uiux-plan-and-mocks`        | —              | —                           |
| 1    | 01  | Merge box permissions       | `fix/pr-merge-box-permissions`    | 00             | Interaction +0.3            |
| 1    | 02  | htmx loading states, toasts | `feat/htmx-loading-states`        | 00             | Interaction +1.0            |
| 1    | 03  | Shared LabelChip            | `feat/label-chip-component`       | 00             | Visual +0.4, A11y +0.3      |
| 1    | 04  | Avatar/initials consistency | `fix/avatar-initials-consistency` | 00             | Visual +0.3                 |
| 2    | 05  | List display bugs           | `fix/account-list-display-bugs`   | 03, 04         | Edge +0.6                   |
| 2    | 06  | htmx fragment parity        | `fix/htmx-fragment-parity`        | 02             | Interaction +0.2, Edge +0.3 |
| 2    | 07  | Accessibility quick fixes   | `fix/a11y-quick-fixes`            | 01             | A11y +0.5                   |
| 3    | 08  | Copy pass                   | `tech/ui-copy-pass`               | 04, 05, 06, 07 | Content +1.35               |
| 3    | 23  | Same-page reload, dialog scroll | `fix/settings-redirect-dialog-scroll` | 06        | Interaction +0.2, Edge +0.2 |

### Wave 2 — core workflows (≈7.6 → 8.2)

| Step | #   | Ticket                     | Branch                           | Depends on | Gain                            |
| ---- | --- | -------------------------- | -------------------------------- | ---------- | ------------------------------- |
| 1    | 14  | Superadmin admin mode      | `fix/superadmin-settings-access` | 00         | IA +0.2                         |
| 2    | 13  | Notifications triage       | `feat/notifications-triage`      | 02         | IA +0.3, Features +0.2          |
| 3    | 09  | PR review flow             | `feat/pr-review-flow`            | 01, 02, 07 | Features +0.8, Interaction +0.5 |
| 3    | 11  | Error recovery             | `fix/form-error-recovery`        | 02, 06     | Edge +0.8                       |
| 3    | 12  | Accessibility gate         | `tech/a11y-axe-gate`             | 07         | A11y +1.0                       |
| 4    | 10  | Issue & PR list filters    | `feat/issue-pr-list-filters`     | 03, 05, 08 | Features +0.4, IA +0.5          |

### Wave 3 — roadmap features (≈8.2 → 8.4)

Each starts with `/grill-with-docs` and `/to-spec` inside its mock folder, then `/to-tickets`; the ticket here only
tracks the effort. The spec sessions can start at step 1; the build PRs come after wave 2.

| Step | #   | Ticket                     | Branch (first PR)                    | Depends on | Phase     | Gain          |
| ---- | --- | -------------------------- | ------------------------------------ | ---------- | --------- | ------------- |
| 1    | 15  | Repository rename          | `feat/repo-rename`                   | 00         | —         | Features +0.2 |
| 1    | 16  | Signed commit verification | `feat/phase-17.2-signed-commits`     | 00         | 17.2      | Features +0.3 |
| 1    | 17  | Secret scanning            | `feat/phase-17.3-secret-scanning`    | 00         | 17.3      | Features +0.3 |
| 1    | 18  | CI pipelines (Actions tab) | `feat/phase-18.1-ci-runner`          | 00         | 18.1–18.3 | Features +0.6 |
| 1    | 19  | Package registry           | `feat/phase-16.1-container-registry` | 00         | 16.1–16.2 | Features +0.4 |

### Polish findings (ticket 22)

Opened by the wave 1 re-score. All `needs-triage`; each is one PR and one session like the rest.

| Step | #   | Ticket                                      | Branch                                | Depends on | Gain                           |
| ---- | --- | ------------------------------------------- | ------------------------------------- | ---------- | ------------------------------ |
| 1    | 24  | Home priority, heatmap text alternative     | `fix/home-priority-heatmap-text`      | —          | Visual +0.2, A11y +0.2         |
| 1    | 25  | Keyboard and ARIA for custom widgets        | `fix/widget-aria-patterns`            | 24         | A11y +0.5                      |
| 1    | 26  | Gist scope, privacy wording, revision copy  | `fix/gist-scope-and-wording`          | —          | IA +0.2, Content +0.3          |
| 1    | 29  | Dark surface elevation                      | `fix/dark-surface-elevation`          | —          | Dark +0.5, Visual +0.1         |
| 1    | 31  | Heading, timestamp, excerpt consistency     | `tech/ui-consistency-sweep`           | —          | Visual +0.2, Content +0.2      |
| 2    | 27  | Edit conflict, branch commits, release assets | `feat/edit-and-release-workflows`   | 11         | Features +0.4, Edge +0.2       |
| 2    | 28  | Navigation context, search peers, explore   | `feat/navigation-context-and-discovery` | 14       | IA +0.4, Features +0.2         |
| 3    | 30  | Merged-PR summary, merge confirmation       | `fix/pr-merge-accuracy`               | 09         | Interaction +0.3, Edge +0.2    |

Opened by the wave 2 re-score. All `needs-triage`.

| Step | #   | Ticket                                          | Branch                                 | Depends on | Gain                          |
| ---- | --- | ----------------------------------------------- | -------------------------------------- | ---------- | ----------------------------- |
| 1    | 32  | Merged PRs keep their commits and diff          | `fix/merged-pr-keeps-diff`             | —          | Edge +0.3, Features +0.2      |
| 1    | 33  | Inline field validation and recovery            | `fix/inline-validation-recovery`       | —          | Edge +0.4, A11y +0.2          |
| 1    | 34  | Unfinished settings surfaces                    | `feat/unfinished-settings-surfaces`    | —          | Features +0.5, Content +0.1   |
| 1    | 35  | Notifications inbox follow-ups                  | `fix/notifications-inbox-followups`    | —          | Interaction +0.2              |
| 1    | 36  | Destructive actions: prominence, confirmation   | `fix/destructive-action-confirmation`  | 31         | Interaction +0.3, Visual +0.1 |
| 1    | 37  | PR merge box: dismiss, resolve, viewer state    | `feat/pr-merge-box-actions`            | 32         | Features +0.3, Interaction +0.2 |
| 1    | 38  | Account issue and PR lists use the qualifier bar | `feat/account-lists-qualifier-bar`    | —          | IA +0.2, Features +0.1        |
| 1    | 39  | Commit identity, pulse and blame presentation   | `fix/history-identity-pulse-blame`     | —          | Content +0.2, Visual +0.1     |
| 1    | 40  | Admin console follow-ups                        | `fix/admin-console-followups`          | —          | Content +0.1, IA +0.1         |

### Mocks track

| Step | #   | Ticket                    | Branch                     | Depends on | Gain                            |
| ---- | --- | ------------------------- | -------------------------- | ---------- | ------------------------------- |
| 1    | 20  | Mock drift and COVERAGE   | `tech/mockups-drift-fixes` | 00         | Mocks 7.0 → 8.3, fidelity → 8.8 |
| 2    | 21  | Generate mocks from templ | `tech/mockgen`             | 20         | Mocks → 9.5, fidelity → 9.5     |

### Polish loop

| Step           | #   | Ticket              | Branch                     | Depends on                        | Gain                  |
| -------------- | --- | ------------------- | -------------------------- | --------------------------------- | --------------------- |
| after each wave | 22 | Re-score and polish | `fix/uiux-polish-wave-<n>` | the wave's last ticket (08, 10, 19) | Closes the gap to 9.5 |

Rules for every UI ticket:

- Update the standing mocks in `mockups/` (served by `make mockups`) in the same PR wherever the change is visible
  there: the page's `.html`, `shell.js` for header, subnav or footer changes, `COVERAGE.md` when a state gains or loses
  a mock, `index.html` for a new page. Copy classes from the templ, and check the mock under `make mockups`. A change
  with nothing the mocks draw (server-only, or an unmocked state already listed in `COVERAGE.md`) says so in the PR.
- `.scratch/` feature mocks are design input, not served by `make mockups`. When a feature from one ships (09, 10,
  15–19), its pages get standing mocks in `mockups/` in the PR that ships them.
- Every visual check, of the mock and of the real page, covers **both light and dark mode** at 1440px, and the PR
  evidence shows both.

## Hotspot files

Two tickets editing these at once will conflict, so **Depends on** orders every pair that shares one.

- `internal/view/layout/layout.templ` — 04 (header initials), 05 (bell badge), 08 (footer)
- `components/pr_list_row.templ`, `components/issue_list_row.templ` — 03, 05, 10
- `pages/account_pulls.templ`, `pages/account_issues.templ` — 03, 05, 08, 10
- `components/mergeability_box.templ`, `pages/pull_detail.templ` — 01, 07, 09

## Verification harness

Desktop screenshots of every mock/real pair at 1440px, in **both light and dark mode** (Playwright `colorScheme`
`'light'` and `'dark'`), against a seeded throwaway server:

1. `docker run -d --name czui-pg -e POSTGRES_DB=czui -e POSTGRES_USER=cloudzilla -e POSTGRES_PASSWORD=test -p <port>:5432 postgres:18-alpine`
2. `cz-admin migrate` and `cz-admin seed` with `CZ_DATABASE_DSN` pointing at it (seed takes ~2 min), then run the server
   with a unique `CZ_SERVER_PORT`, `CZ_GIT_SSH_PORT` and `CZ_AUTH_COOKIE_NAME`.
3. `node mockups/server.js <port>` for the mocks.
4. Sign in as `admin@example.test` / `cloudzilla-seed`. The seeded superadmin owns `siteadmin/rusty-meadow` and the
   `paper-planes` org; use those for owner-only pages, `ada-silva/hollow-atlas` for busy repo pages.

## Comments

2026-11-06: added 23 (same-page `HX-Redirect` does not reload; `components.Dialog` does not scroll), found in
#270's live check.

2026-11-06: lanes replaced by **Depends on** and **Step**. Added 05 → 04 (`layout.templ`), 09 → 07
(merge box) and 10 → 08 (account pages) so tickets sharing a hotspot file never run at once.

2026-11-06 (triage): Wave 1 tickets (01–08), 11–13, 20 and 22 are `ready-for-agent`. 09, 10, 15–19 and 21 are
`ready-for-human`: each needs a `/grill-with-docs` + `/to-spec` session first. 14 is `needs-info` until the superadmin
access model is decided.

2026-11-06 (maintainer): 14 decided as a time-boxed, audited admin mode (no blanket override), now `ready-for-human`.
18: built-in CI gets an Actions repo tab; the PR Checks tab stays the results view.

2026-11-06 (wave 1 re-score, ticket 22): real app, desktop, 1440 light and dark, at origin/main 7ce58dbb (tickets
01–08 and 23 merged), seeded throwaway server. Two independent reviewers with one brief: Claude (Sonnet 5.5, medium) and
Codex (GPT-6.1 Sol). Neither was told the baseline or the target.

| Dimension                | Baseline | Wave 1 target | Claude | Codex | Mean of both |
| ------------------------ | -------- | ------------- | ------ | ----- | ------------ |
| Visual design            | 7.9      | 8.6           | 8.0    | 8.0   | 8.0          |
| Information architecture | 7.75     | 7.75          | 8.25   | 7.5   | 7.88         |
| Interaction & feedback   | 6.0      | 7.5           | 7.75   | 7.0   | 7.38         |
| Empty / error / edge     | 6.2      | 7.1           | 7.5    | 7.0   | 7.25         |
| Accessibility            | 6.6      | 7.4           | 8.0    | 7.0   | 7.5          |
| Content & microcopy      | 6.85     | 8.2           | 8.0    | 7.0   | 7.5          |
| Dark mode                | 8.25     | 8.25          | 8.5    | 8.0   | 8.25         |
| Feature completeness     | 6.2      | 6.2           | 8.0    | 6.5   | 7.25         |
| **Overall**              | **7.0**  | **7.6**       | **8.00** | **7.25** | **7.63**  |

Wave 1 met its 7.6 target on the mean of both reviewers (Claude above it, Codex below). The reviewers disagree most on
Feature completeness (1.5 points) and Accessibility (1.0): Claude counted what exists, Codex counted what a forge user
expects (PR discovery, notification triage, editing workflows). Neither reviewer is near 9.5 on any dimension.

Limits of this pass:

- The harness cropped pages taller than 3200px to the viewport (a `clip` without `fullPage`), so 31 of the 75 light
  shots are 900px tall and the long ones (settings, org and account settings, admin users, audit log, projects) lost
  their lower parts. Codex also read the source for each page; Claude viewed 15 of the shots. Fixed for wave 2.
- Claude viewed about 15 of the 150 shots (mostly light) and sampled the rest by route; Codex viewed all of them. Findings
  Claude could not confirm are marked "(unverified)" in the tickets.
- The Codex reviewer did not run the app; interaction scores come from source reading.

Findings: 54 from Claude and 38 from Codex. Fixed in the polish PR: merge-ready subtitle claimed passed checks and
approvals it never checked; topic "Next" always shown; native controls stayed light in dark mode (`color-scheme`);
error toasts vanished in 4s with no dismiss; the wiki header offered Edit on a missing page; PR files diff headers were
mouse-only; the diff-load failure gave no next step; admin settings showed raw keys; the register form hid the
8-character rule; a filtered issue list said "No open issues" with no way back. Opened as tickets: 24–31. Left as
comments on running tickets: 10, 11, 12, 13, 14, 20.

2026-11-12 (wave 2 re-score, ticket 22): real app, desktop, 1440 light and dark, at origin/main df324147 plus the wave 1
polish (tickets 09–14 and 30 merged), seeded throwaway server. Same two reviewers and brief as wave 1: Claude (Sonnet
5.5, medium) and Codex (GPT-6.1 Sol), neither told the baseline, target or earlier scores.

| Dimension                | Baseline | After wave 1 (measured) | Wave 2 target | Claude   | Codex    | Mean of both |
| ------------------------ | -------- | ----------------------- | ------------- | -------- | -------- | ------------ |
| Visual design            | 7.9      | 8.0                     | 8.6           | 7.5      | 8.0      | 7.75         |
| Information architecture | 7.75     | 7.88                    | 8.75          | 7.0      | 8.0      | 7.5          |
| Interaction & feedback   | 6.0      | 7.38                    | 8.0           | 7.0      | 7.25     | 7.13         |
| Empty / error / edge     | 6.2      | 7.25                    | 7.9           | 7.0      | 7.0      | 7.0          |
| Accessibility            | 6.6      | 7.5                     | 8.4           | 6.5      | 7.25     | 6.88         |
| Content & microcopy      | 6.85     | 7.5                     | 8.2           | 7.0      | 8.0      | 7.5          |
| Dark mode                | 8.25     | 8.25                    | 8.25          | 8.5      | 8.0      | 8.25         |
| Feature completeness     | 6.2      | 7.25                    | 7.6           | 7.5      | 7.25     | 7.38         |
| **Overall**              | **7.0**  | **7.63**                | **8.2**       | **7.25** | **7.59** | **7.42**     |

Wave 2 did **not** reach its 8.2 target, and the mean is 0.21 below wave 1's 7.63. Only Feature completeness rose
(7.25 to 7.38); Content and Dark mode held; Visual, IA, Interaction, Edge and Accessibility fell. The two passes are not
like for like: wave 1 sampled about 15 of 150 shots (Claude) and Codex read source, while this pass has full-page shots
of 124 states per theme and both reviewers viewed all 290 slices, including the new wave 2 states. A stricter, fuller
look explains some of the drop, but the pass cannot separate that from real regressions. Claude scored lower than Codex
on every dimension except Edge (equal), Dark mode and Features; Visual, IA and Content differ by a full point.

Harness notes for wave 3:

- The brief wrongly said admin pages are refused outside admin mode. `/admin/users` and the other instance pages are
  superadmin-only and not gated by admin mode (`docs/access-control.md`), so both reviewers' "renders without refusal"
  finding on `admin-forbidden` is a brief error, not a bug. Say instead that repository and organization pages are
  gated.
- The merge-strategy menu shot (`117`) never opened: the harness selected on text and the trigger is an icon with
  `aria-label="Choose merge strategy"`. Both reviewers flagged it as unverified; opened and checked in the polish PR.
  A full-page shot also clips a modal overlay to the first viewport, so modal states are viewport shots (fixed).
- The harness probed `sort:comments-desc` and `sort:updated-desc`, which are not valid sort values (`newest`, `oldest`,
  `updated`, `comments`). Both reviewers reported the empty list as a bug; the real defect underneath was that nothing
  told the user the token was searched as text (fixed).
- The account lists (`/issues`, `/pulls`) take `filter`/`state`/`sort`, not `q`, so the probed URLs showed an unfiltered
  list: a real inconsistency, ticket 38.
- Claude's first reads of many images returned "request limit"; it re-read them (notes file) and listed the shots whose
  valid view came last. Treat its per-shot claims as seen.

Findings: 35 from Claude and 22 from Codex. Fixed in the polish PR: a known qualifier with a bad value (`sort:comments-desc`)
searched as text with no explanation (unknown keys such as `bogus:value` stay silent: `std::vector` and `12:30` are
ordinary text); "Clear filters" shown twice in an empty list; code search showed
literal `**` around matches (now `<mark>`); org settings said "Teams aren't wired up yet" and showed disabled buttons;
the release form placeholder named a roadmap phase; the merge box listed only the conflicts when changes were also
requested. Opened as tickets: 32–40. Added as comments on open tickets: 24, 25, 27, 28, 29, 31. Not bugs: the admin
users page outside admin mode and the empty merge-strategy menu (harness), and the sort-zeroes-list report (invalid
sort value, see above).

2026-11-12 (triage): 32, 33, 35–40 `ready-for-agent`; 34 `ready-for-human` (teams needs a spec). 36 waits for 31
(project-row Delete moved from 31), 37 for 32 (pull pages), 25 for 24 (`home.templ`). Order: the polish tickets update the hand mocks in
their own PRs.

2026-11-12 (maintainer): ticket 21 is `wontfix`; mocks stay design-first and hand-written. See
[`../2026-11-12-mockups-design-first/spec.md`](../2026-11-12-mockups-design-first/spec.md).
