# UI Overhaul — Design Spec

**Date:** 2026-05-14
**Status:** Approved, ready for plan
**Source mockups:** [mockups/](../../../mockups/) (42 HTML files)

## 1. Goals & non-goals

**Goal.** Port the visual language from `mockups/` onto the existing Templ pages — pixel-perfect output via the existing shadcn token vocabulary — and add the backend store/service/handler glue for every widget a mockup displays but the codebase doesn't yet back. Ship phase-by-phase in tier order, one PR per phase, until every mockup'd page matches its mockup.

**Non-goals.**

- No rewriting the component primitives that landed in `36c8d6b`. Button, Badge, Card, Table, Dropdown, CommandPalette stay as-is; we extend them where a mockup needs something they don't cover (heatmap, sparkline, kanban column, etc.).
- No full redesign of orphan templ pages (admin_settings, audit_log, milestones, saved_replies, notifications, notification_settings, oauth_apps, oauth_authorize, security, setup, sso_settings, totp_verify, tokens, search, register, login, invite, not_found, forbidden, wiki_edit, user_gists, user_stars, code_search, settings). Orphans receive a **token sweep** in Phase 0: replace any lingering hand-rolled CSS (e.g. `bg-gray-900`, `text-white`, `border-white/10`) with shadcn tokens so they don't look broken next to redesigned pages. No layout changes.
- No feature-flag gating. Phase PRs merge directly; prod is mixed-state until rollout completes.
- No mockup-tokens-vs-shadcn-tokens migration. Class translation table is the source of truth; mockup-bespoke utility classes get added to `tailwind/input.css` only when they have no shadcn equivalent (`grid-bg`, heatmap scale).
- The static `mockups/` directory is not served at runtime; it stays as design reference.
- No mobile redesign. Desktop-first matches the mockups.
- No i18n.

## 2. Architecture & approach

### Where new code lives

No new top-level directories. Everything fits the existing `Stores → Services → Handlers → Templ` stack.

- Page-specific layouts: `internal/view/pages/`
- New reusable widgets: `internal/view/components/`
- Shared sub-views (repo 9-tab subnav, dashboard 8-tab subnav, etc.): `internal/view/fragments/`

### Class translation, not class replacement

The mockups carry inline `<style>` blocks with tokens like `--fg-2` and utility classes like `fg2 b mono hover-row grid-bg`. The spec ships a canonical translation table:

| Mockup class | shadcn equivalent |
|---|---|
| `mono` | `font-mono` |
| `fg2` | `text-muted-foreground` |
| `fg3` | `text-muted-foreground/70` (or define a third tier token in input.css if needed) |
| `b` | `border-border` |
| `bs` | `border-border` (stronger variant — define `--border-strong` if needed) |
| `surface` | `bg-card` |
| `elevated` | `bg-accent` |
| `hover-row` | `hover:bg-accent` |
| `link` | `text-primary` |
| `tab-active` | `text-foreground border-foreground` |
| `dropdown-*` | already covered by the `components.DropdownMenu` family |
| `skip-link` | already in `tailwind/input.css` |
| `grid-bg` | promoted to `tailwind/input.css` — no shadcn equivalent |
| `heatmap-cell-*` | promoted to `tailwind/input.css` — no shadcn equivalent |
| `avatar` | already covered by `components.Avatar` |

**Inline `<style>` blocks do not survive the port.** Anything inline gets translated to Tailwind utilities or hoisted to globals.

### Theme handling

Codebase already has working light/dark via `cz-theme` localStorage + `html.light/dark`. Mockups assume the same model. No work needed on theming infra; just verify each phase renders cleanly in both modes (light is currently under-tested — Phase 0 sweeps the worst current offenders).

### Component-vs-inline boundary

If mockup HTML matches an existing component's structure, use the component. If it's a one-off page-specific layout, inline the markup in the page. New repeating widgets become new components (see §4 catalog).

### Wiring philosophy

Where the mockup shows data the backend doesn't yet produce:

- **Real implementation, no fakery.** Heatmaps, sparklines, attention lists, dependency graphs, language stats — all real data.
- **Cache eagerly where compute is non-trivial.** Heatmap and contributor stats cache in DB (migrations 054, 055), recomputed on push via the existing webhook-dispatch hook. Last-commit-per-tree-entry and language composition cache in-memory (service-level LRU/TTL).
- **New routes for net-new pages.** `/pulls`, `/issues`, `/activity`, `/stars`, `/changelog`, `/docs` — each gets route + handler + page. Some reuse existing query paths (e.g. `/stars` unscoped from username).

### HTMX vs full page

Default rule: in-page mutations (label add/remove, comment post, reaction, PR merge, file-tree expand, kanban drag) are HTMX. Page navigations (filter chip change, tab change) are full links. Follow the existing pattern in [docs/htmx-patterns.md](../../htmx-patterns.md).

### JS scope

Mockups carry small client-side scripts (dropdown open/close, file-tree expand, kanban drag, heatmap tooltip, slash-command palette). Use Alpine.js (already wired) for ephemeral UI state; use HTMX for anything that hits the server. No new JS framework, no new build step.

## 3. Phase plan

Eleven phases, each = one PR. Phase 0 is foundation work; Phases 1–10 follow the mockup index tiers in order.

| # | Name | Branch | Pages | New routes | Migrations |
|---|---|---|---|---|---|
| 0 | Foundation | `feat/ui-overhaul-phase-0-foundation` | layout + shared chrome | — | — |
| 1 | Flagship | `feat/ui-overhaul-phase-1-flagship` | home, repo, pr | — | 054 |
| 2 | Code browser | `feat/ui-overhaul-phase-2-code-browser` | tree, blob, blame, project | — | — |
| 3 | Repo activity | `feat/ui-overhaul-phase-3-repo-activity` | commits, contributors, pulse, dependencies | `/{owner}/{repo}/network/dependencies` | 055 |
| 4 | Issues/PRs/Discussions/Actions | `feat/ui-overhaul-phase-4-tracker` | issues, issue_detail, issue_new, pulls, pull_new, discussions, actions | `/{owner}/{repo}/actions` (placeholder) | — |
| 5 | PR sub-views | `feat/ui-overhaul-phase-5-pr-subviews` | pr_commits, pr_checks, pr_files | — | — |
| 6 | Distribution & docs | `feat/ui-overhaul-phase-6-distribution` | releases, wiki, topic, repo settings | — | — |
| 7 | Gists | `feat/ui-overhaul-phase-7-gists` | gists, gist_detail, gist_new | — | — |
| 8 | Your dashboard | `feat/ui-overhaul-phase-8-dashboard` | my_repositories, my_pulls, my_issues, activity, stars | `/pulls`, `/issues`, `/activity`, `/stars` | 056 (if needed) |
| 9 | Profile & orgs | `feat/ui-overhaul-phase-9-profile-orgs` | profile, organizations, new_organization, new_repo | — | — |
| 10 | Settings & global | `feat/ui-overhaul-phase-10-global` | account_settings, docs, changelog, auth | `/docs`, `/changelog` | — |

### Phase 0 contents

1. **Class translation table** — committed to `docs/ui-overhaul-class-map.md`.
2. **Promote irreducible utilities** — add `grid-bg`, `.heatmap-cell-{0..4}`, any auth gradient to `tailwind/input.css`.
3. **Layout chrome sync** — update `internal/view/layout/layout.templ` to match the mockup top-nav exactly. Wire the workspace switcher dropdown ("Personal" / org list / "New organization") to real `OrgService.ListForUser`.
4. **Repo subnav fragment** — extract the 9-tab repo sections subnav (Code / Issues / Pulls / Discussions / Actions / Projects / Wiki / Insights / Settings) into `internal/view/fragments/repo_subnav.templ`. Included by every repo-scoped page in phases 1–6.
5. **Dashboard subnav fragment** — same idea for the 8-tab user dashboard subnav. Used by phases 8–9.
6. **Orphan-page token sweep** — pass over orphan templ files (see §1 list), replace `bg-gray-900`, `text-white`, `border-white/10` etc. with shadcn tokens. No layout changes.
7. **Verification** — click every existing page in dev server in both themes, confirm nothing visually regresses.

### Per-phase exit criterion

A phase merges to `main` when:

1. Every page in the phase visually matches its mockup in both light and dark mode (manual verification).
2. Every widget the mockup shows is wired to real data (no `// TODO: wire`).
3. `go test ./...` passes, `make lint` passes, `templ generate` produces no diff.
4. `silent-failure-hunter` agent run on the branch reports no high-confidence findings.

## 4. Per-page deltas

Inventory below; one row per mockup. **VISUAL** = layout-level change vs current templ. **WIRING** = data/widgets the mockup shows that the codebase doesn't yet back. **NEW COMP** = reusable component to add under `internal/view/components/`.

### Flagship (Phase 1)

| Mockup | Templ | VISUAL | WIRING | NEW COMP |
|---|---|---|---|---|
| home | `home.templ` | Add stat strip, attention list, commit heatmap, activity feed alongside repos table | Heatmap data (per-day commit counts), attention list (assigned + review-requests + mentions), cross-repo stats | Heatmap, StatStrip |
| repo | `repo.templ` | File tree + README + About sidebar w/ languages/releases/contributors strip | Language composition, top contributors per repo | LanguagesBar |
| pr | `pull_detail.templ` | Conversation timeline w/ inline review, mergeability box, sidebar metadata | Mergeability box composite (ahead/behind/conflicts/required checks/required reviews) | MergeabilityBox, TimelineEntry |

### Tier 1 · Code browser (Phase 2)

| Mockup | Templ | VISUAL | WIRING | NEW COMP |
|---|---|---|---|---|
| tree | `tree.templ` | GitLab-style IDE sidebar + file viewer split; click swaps right pane via HTMX | Last-commit-per-entry data | FileTreeSidebar |
| blob | `blob.templ` | Line numbers + permalink anchors + Raw button | — | — |
| blame | `blame.templ` | Author/SHA/date columns alongside code | — | BlameRow |
| project | `project_detail.templ` | Kanban columns w/ draggable cards | Card → issue link, drag/drop state persistence | KanbanColumn, KanbanCard |

### Tier 2 · Repo activity (Phase 3)

| Mockup | Templ | VISUAL | WIRING | NEW COMP |
|---|---|---|---|---|
| commits | `commits.templ` | Grouped-by-date list w/ author/SHA/browse-files | — | — |
| contributors | `contributors.templ` | Per-contributor row w/ commits/+−/sparkline | Per-contributor weekly aggregates | Sparkline |
| pulse | `pulse.templ` | 30-day summary w/ mini charts | 30-day rollup counts for issues/PRs/commits | MiniChart |
| dependencies | `dependencies.templ` | Manifest groups w/ version + license columns | Manifest parser (start: `go.mod`; later: `package.json`, `requirements.txt`) | DependencyGroup |

### Tier 3 · Issues / PRs / Discussions / Actions (Phase 4)

| Mockup | Templ | VISUAL | WIRING | NEW COMP |
|---|---|---|---|---|
| issues | `issues.templ` | Pinned section + filter/sort chips + bulk actions | Pinned issue surfacing on list page | BulkActionsBar |
| issue_detail | `issue_detail.templ` | Comment thread w/ assignees/labels/priority/milestone/linked PRs sidebar | Linked-PRs lookup | TimelineEntry (shared w/ PR) |
| issue_new | `issue_new.templ` | Template picker + composer | Template enumeration in form | — |
| pulls | `pulls.templ` | State subnav + CI status + reviewer avatars + label chips | CI status badge per PR (lookup combined status) | PRListRow |
| pull_new | `pull_new.templ` | Compare branches + composer + reviewer picker | Reviewer suggestion (CODEOWNERS or last-touched) | ReviewerPicker |
| discussions | `discussions.templ` | Category chips + Open/Answered/Closed filter + pinned/answered badges | Answered-state surfacing | — |
| actions | NEW (placeholder) | Friendly empty-state + dimmed preview "coming soon" | None; register route | — |

### Tier 4 · PR sub-views (Phase 5)

All share PR chrome from `pull_detail.templ`. Implementation extends the existing page with `?tab=` switching or sub-routes.

| Mockup | VISUAL | WIRING | NEW COMP |
|---|---|---|---|
| pr_commits | Same PR chrome, commit list body | — | — |
| pr_checks | Status checks list (build/lint/templ/tests/integration/docker) | Per-check status rows from combined status | ChecksList |
| pr_files | File-tree sidebar + inline hunk display | Diff stats per file (additions/deletions) | DiffFileTree |

### Tier 5 · Distribution & docs (Phase 6)

| Mockup | Templ | VISUAL | WIRING | NEW COMP |
|---|---|---|---|---|
| releases | `releases.templ` | Published + draft sections w/ assets, changelogs, prerelease tags | Draft vs published surfacing (already supported per commit `420b44b`) | ReleaseAsset |
| wiki | `wiki_page.templ` | Sidebar nav + on-this-page TOC | TOC extraction from rendered markdown headings | WikiTOC |
| topic | `topic.templ` | Repos tagged w/ topic | — | — |
| settings | `repo_settings.templ` | Sidebar nav for General/Access/Collaborators/Branches/Webhooks/Deploy keys/Danger | — | SettingsSidebar |

### Tier 6 · Gists (Phase 7)

| Mockup | Templ | VISUAL | WIRING | NEW COMP |
|---|---|---|---|---|
| gists | `gists.templ` | Public + secret w/ file/star/fork counts | Fork count surfacing | — |
| gist_detail | `gist_detail.templ` | File tabs + clone URL field | — | — |
| gist_new | `gist_new.templ` | Filename + language selector + Write/Preview tabs | Markdown preview endpoint (likely exists) | — |

### Tier 7 · Your dashboard (Phase 8)

| Mockup | Templ | VISUAL | WIRING | NEW COMP |
|---|---|---|---|---|
| my_repositories | reshape `user.templ` tab | Filterable list w/ role badges + language indicators | Role badge composite per repo | — |
| my_pulls | NEW (`/pulls`) | Created/Assigned/Review-requests/Mentioned tabs | New cross-repo PR query | — |
| my_issues | NEW (`/issues`) | Assigned/Created/Mentioned tabs | New cross-repo issue query | — |
| activity | reshape `feed.templ` | Chronological feed of merges/opens/comments/stars/releases | Activity event aggregation across watched repos | ActivityRow |
| stars | NEW (`/stars`) | Cross-platform starred w/ language filter chips | Reuse `user_stars.templ` query but unscoped from username | — |

### Tier 8 · Profile & orgs (Phase 9)

| Mockup | Templ | VISUAL | WIRING | NEW COMP |
|---|---|---|---|---|
| profile | reshape `user.templ` overview | Bio + pinned repos + contribution heatmap + recent activity + orgs + top languages | Pinned repos data, contribution heatmap (52-week), top-languages aggregate | ContributionHeatmap (reuses Heatmap), PinnedRepo |
| organizations | reshape `org.templ` | Org list w/ role badge + Leave/Manage actions | Role-per-org lookup | — |
| new_organization | NEW (`/organizations/new`) | Name + email + owner type + plan selection form | Org create endpoint (likely exists in OrgService) | — |
| new_repo | `repo_new.templ` | Owner/name/visibility/init options (README, .gitignore, license) | License & gitignore template enumeration | — |

### Tier 9 · Settings & global (Phase 10)

| Mockup | Templ | VISUAL | WIRING | NEW COMP |
|---|---|---|---|---|
| account_settings | consolidate `settings.templ` + `security.templ` + `tokens.templ` + `notification_settings.templ` | Single sidebar-nav page w/ Profile / Password+2FA / SSH / PATs / Notifications / Danger | — (just composition) | SettingsSidebar (shared w/ repo settings) |
| docs | NEW (`/docs`) | 3-column: section nav + prose + on-this-page TOC | Static markdown rendered from `docs/` directory at build time | DocsLayout |
| changelog | NEW (`/changelog`) | Vertical release timeline w/ feature/fix/breaking tags | Parsed from `CHANGELOG.md` or release-please output | TimelineEntry (shared) |
| auth | extend `login.templ` + `register.templ` + `totp_verify.templ` + `oauth_authorize.templ` + `invite.templ` + `not_found.templ` + `forbidden.templ` | Shared card layout w/ 6 panel variants | — | AuthCard |

### Aggregated new components

- `Heatmap` — home + profile (52-week commit calendar)
- `Sparkline` — contributors
- `MiniChart` — pulse
- `StatStrip` — home stat row
- `MergeabilityBox` — PR detail
- `TimelineEntry` — issue + PR + changelog
- `KanbanColumn`, `KanbanCard` — project board
- `FileTreeSidebar` — tree
- `DiffFileTree`, `ChecksList` — PR sub-views
- `LanguagesBar` — repo + profile
- `DependencyGroup` — dependencies
- `ContributionHeatmap` — profile (reuses Heatmap)
- `PinnedRepo` — profile
- `BulkActionsBar` — issues
- `PRListRow` — pulls list
- `ReviewerPicker` — pull_new
- `BlameRow` — blame
- `WikiTOC` — wiki
- `SettingsSidebar` — account_settings + repo_settings
- `ActivityRow` — activity feed
- `AuthCard` — auth shell
- `ReleaseAsset` — releases
- `DocsLayout` — docs

## 5. Wiring catalog — placement

Every gap drops into the existing layers. No new top-level directories.

| Wiring gap | Migration | Store | Service | Handler / route | Templ |
|---|---|---|---|---|---|
| Commit heatmap data | `054_commit_day_counts.sql` | new `commit_stats_store.go` | new `commit_stats_service.go`; populate on `PostReceive` in `repo_service.go` | extend `PageHome` + `GetUser` | home.templ + user.templ |
| Attention list | — | reuses `issue_store.go`, `pull_store.go`, `notification_store.go` | new `attention_service.go` | extend `PageHome` | home.templ |
| Contributor weekly stats | `055_contributor_week_stats.sql` | new `contributor_stats_store.go` | new `contributor_stats_service.go`; populate on `PostReceive` | extend `PageContributors` | contributors.templ |
| Last-commit-per-tree-entry | — | — (go-git) | extend `code_service.go` w/ `ListEntriesWithLastCommit` + LRU cache | extend `PageTree` | tree.templ |
| Mergeability composite | — | — | extend `code_service.go` w/ `Mergeability(prID)` | extend `GetPull` page | pull_detail.templ |
| Dependency parsing | — (cache only) | — | new `dependency_service.go` (`go.mod` parser first; in-memory cache keyed by repo+ref) | new `dependency_handler.go` at `GET /{owner}/{repo}/network/dependencies` | dependencies.templ |
| Cross-repo PRs/issues | maybe `056_user_subscription_index.sql` | extend `pull_store.go` + `issue_store.go` w/ `ListForUserAcrossRepos` | extend `pull_service.go` + `issue_service.go` | new handlers + routes `/pulls`, `/issues` | NEW `my_pulls.templ`, `my_issues.templ` |
| Activity feed aggregation | — | extend `feed_store.go` | extend `feed_service.go` | extend existing `/feed`; register `/activity` alias | feed.templ (reshape) |
| Pinned repos | — | extend `user_store.go` — recommend `users.pinned_repo_ids BIGINT[]` column | extend `user_service.go` | extend `GetUser` page | user.templ |
| Top languages / language composition | — | — | new `language_service.go` (extension scan, in-memory cache) | extend `GetRepo` page | repo.templ + user.templ |
| Reviewer suggestion | — | — | extend `pull_service.go` w/ `SuggestReviewers(repo, head)` | extend new-pull handler | pull_new.templ |
| CI status per PR list row | — | reuses `status_store.go` | extend `pull_service.go` to join combined status into `ListPulls` | extend `ListPulls`/`PagePulls` | pulls.templ |
| Wiki TOC extraction | — | — | extend wiki rendering (or `markdown_body.templ` helper) to extract `<h2>/<h3>` server-side | extend wiki handler view model | wiki_page.templ |
| Docs static markdown | — | — | new `docs_service.go` (read `docs/*.md` via `embed.FS`, render to HTML) | new `docs_handler.go` at `GET /docs`, `GET /docs/{slug}` | NEW `docs.templ` |
| Changelog timeline | — | — | new `changelog_service.go` (parse `CHANGELOG.md` at startup) | new `changelog_handler.go` at `GET /changelog` | NEW `changelog.templ` |

### Wiring conventions

1. **No store→handler shortcuts.** Handlers call services which call stores. New services join into `internal/service/services.go` `Services` struct.
2. **Routes register in one place** — `internal/router/router.go`; page names appended to `pageNames` if a new page template.
3. **View models in one place** — `internal/handler/viewmodels.go`.
4. **Caches in service layer** — LRU/TTL caches live as struct fields on the service.
5. **Migrations sequential** — 054 (heatmap), 055 (contributor stats), 056 (cross-repo index if needed), assigned to phases 1, 3, 8.
6. **Webhook/notification extension** follows existing pattern: add const to `model/notification.go`, add `NotifyX` to `notification_service.go`; add event string + `XPayload()` to `webhook_service.go`.

## 6. Verification & risk

### Per-phase verification (gates PR merge)

1. **`templ generate` is clean** — no unintended `_templ.go` diff.
2. **`make build-css` is clean** — no Tailwind warnings.
3. **`go test ./...` passes** — existing tests stay green; new backend services ship with unit tests in the same package.
4. **`make lint` passes.**
5. **Manual visual review in both themes** — every redesigned page in dark, then light. Compare to mockup. Check header chrome, table density, hover states, focus rings, empty states.
6. **HTMX paths exercised manually** — any in-page mutation the phase touches (label add, comment post, reaction, merge, kanban drag, file-tree expand).
7. **`silent-failure-hunter` agent** run on the branch per CLAUDE.md. Fix high-confidence findings before opening the PR.
8. **PR opened, not merged direct to main.**

### Cross-phase regression check

After each phase merges, before starting the next: open home, click through redesigned pages from previous phases, sanity sweep.

### Test data

Seed fixtures must cover: one repo with ≥30 days of commits (heatmap), ≥3 contributors (sparklines), ≥1 PR mergeable + ≥1 unmergeable, ≥1 issue with assignees/labels/comments, ≥1 kanban project with cards in each column, ≥1 wiki page, ≥1 release, ≥1 gist. Phase 0 adds `make seed-ui-fixtures` if existing seed isn't sufficient.

### Risk inventory

| Risk | Likelihood | Impact | Mitigation |
|---|---|---|---|
| Mockup `<style>` block has a utility we miss in translation | High | Medium | Phase 0 publishes full translation table; each phase greps its mockup for any class not in the table |
| Light-mode under-tested today, some shadcn tokens render poorly | Medium | Medium | Each phase verifies both themes; Phase 0 sweeps worst current offenders |
| Last-commit-per-tree-entry causes tree page latency on real repos | Medium | High | Service-level LRU; budget check in phase 2 plan (>500ms p95 → DB-backed) |
| Commit heatmap backfill takes minutes on large existing repos | Medium | Low | Background goroutine on first server start after migration 054; empty heatmap until backfill finishes |
| Pinned-repos schema choice (column vs join table) wrong | Low | Low | Both are tiny migrations to swap |
| Long-lived `feat/ui-overhaul-phase-N` branches conflict with main | Medium | Medium | Per-phase PR cadence; rebase before final review |
| Phase 8 cross-repo queries exceed budget on large installs | Low | High | Phase 8 plan benchmarks against synthetic 1000-repo seed; migration 056 ready |
| Mockup expects functionality not yet planned | Medium | Low | `actions.html` already explicitly "coming soon"; tracked as future work |
| New components break in `forced-colors` / high-contrast | Low | Low | Mockups include `@media (forced-colors)` rules; components copy verbatim |
| Class translation table mis-maps a token | Medium | High | Phase 0 ships unit test (fixture page rendered in both class systems, screenshot diff via `chromedp`) plus human review |

### Out-of-scope (tracked for follow-up)

- Mobile responsive design.
- Actions runner backend (placeholder UI only).
- i18n.
- Wiki sidebar nav generation from page hierarchy (Phase 6 ships flat list).
- Full dependency parsing beyond `go.mod`.
- Real-time updates via SSE/WebSocket.

Open-state tracker: maintain `docs/ui-overhaul-followups.md` as items defer during phase implementation.

### Rollout completeness

Done when all 11 PRs are merged and the running UI matches every mockup.
