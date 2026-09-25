# UI Overhaul · Phase 8 · Your Dashboard (cross-repo) — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

> ⚠️ **Revised 2026-05-18.** The **account-navigation feature** (built on branch
> `feat/ui-overhaul-phase-5-pr-subviews`) absorbed roughly two-thirds of this
> phase's original scope — the `/pulls` and `/issues` cross-repo pages, their
> store/service queries, and the account subnav. See **"Already delivered"**
> below. This plan now covers only the *residual* scope: the activity feed
> reshape, the `/stars` page, the `user.templ` repositories tab, and the
> `repositories.primary_language` column.

**Goal:** Reshape `feed.templ` to match the activity-feed mockup and serve it at `/activity`; create `/stars` (cross-repo starred-repos page); reshape `user.templ`'s repositories tab; add `repositories.primary_language` so language-filter chips have a source column.

**Architecture:** One migration (`066_add_repo_primary_language.sql`) adds `repositories.primary_language`, populated on push by extending Phase 1's `RepoService.OnPostReceive`. The account-level pages built by the account-navigation feature render under **`AccountSubnav`** (5 pill tabs: Overview / Repositories / Gists / Pull requests / Issues). `/activity` and `/stars` are **not** among those five tabs — they render under the standard global header without an active subnav tab. Adding Activity/Stars as `AccountSubnav` tabs is a deliberate follow-up, out of scope here.

**Prerequisites:** Account-navigation feature merged (PR #41) — provides `AccountSubnav`, the `/repos` / `/pulls` / `/issues` routes, and the cross-repo store/service methods this plan references. Phase 7 (gists) is soft-sequenced after this phase but has **no code dependency** with Phase 8; the two branches touch disjoint files (only [internal/router/router.go](internal/router/router.go) sees additive edits in both) and can be developed in parallel worktrees.

**Spec:** [2026-05-14-ui-overhaul-design.md](../specs/2026-05-14-ui-overhaul-design.md)

**Branch:** `feat/ui-overhaul-phase-8-dashboard`

---

## Already delivered by the account-navigation feature

Do **not** re-implement any of the following — they exist on the merged feature:

- **`AccountSubnav`** fragment (`internal/view/fragments/account_subnav.templ`) — 5 pill tabs, wired through `BasePage.AccountSubnav *view.AccountSubnavInfo`, rendered by `layout.templ`. This **replaces** the `DashboardSubnav` component the original plan assumed; the unused `dashboard_subnav.templ` stub was deleted.
- **Routes `/repos`, `/pulls`, `/issues`** — registered with `authMW`, before the `/{owner}` catch-all. Handlers `PageAccountRepos` / `PageAccountPulls` / `PageAccountIssues` in `internal/handler/account_handler.go`.
- **Pages** `account_repos.templ`, `account_pulls.templ`, `account_issues.templ`; view-models `AccountReposData` / `AccountPullsData` / `AccountIssuesData` in `internal/view/viewmodels.go`.
- **Cross-repo queries** — `PullStore.ListForUser` + `ListByIDs` (modes `created` / `assigned` / `review_requested` / `mentioned`, plus `open`/`closed` state) and `IssueStore.ListForUser` + `ListByIDs` (modes `assigned` / `created` / `mentioned`). Row types `store.PullListItem` / `store.IssueListItem`. Service wrappers `PullService.ListForUser` / `IssueService.ListForUser`.
- **Review-requests** resolve via the **existing `pull_reviews` table** (`PullReviewStore.ListPullIDsAwaitingReviewer`, filtering `state = 'pending'`, reviewer = `author_id`). The originally-planned `pull_review_requests` table is **not needed** and is dropped from this plan.
- **"Mentioned" filter** resolves via the **existing `mentions` + `comments` tables** (`MentionStore.ListPullIDsMentioning` / `ListIssueIDsMentioning`). The originally-planned `mentions.ref_id` / `ref_type` columns are **not needed** and are dropped from this plan.
- **Topbar repo-switcher** dropdown; `withRepoSubnav` is now an `h.withRepoSubnav(ctx, …)` method.

## Migration numbering

> **Reverified 2026-05-20 against `main`.** `main` has migrations through **064**; slots 056–064 are taken by unrelated features (`add_website_license_to_repos`, `add_project_status`, `add_repo_feature_toggles`, `create_pull_events`, `create_pull_issue_links`, `add_priority_to_issues`, `global_discussion_categories`, `create_discussion_labels`, `polymorphic_reactions`). The original plan's "054 = Phase 1, 056 = Phase 7, 057 = Phase 8" mapping is **stale** — those slots have been used by other work.
>
> **Phase 7's branch** (`feat/ui-overhaul-phase-7-gists`) has already claimed **065** (`gist_stars`). **Phase 8** therefore claims **066** (`add_repo_primary_language`). The account-navigation feature added no migration (it reused existing tables), so it didn't consume a slot.
>
> Re-verify with `ls internal/db/migrations/` immediately before authoring Task 2 — if any other migration has landed on `main` in the interim, bump to the next free slot and update every reference in this plan.

---

### Task 1: Branch setup

- [ ] `git checkout main && git pull && git checkout -b feat/ui-overhaul-phase-8-dashboard`

---

### Task 2: Migration 066 — `repositories.primary_language`

**Files:**
- Create: `internal/db/migrations/066_add_repo_primary_language.sql`
- Modify: `internal/model/repo.go` (add `PrimaryLanguage *string` field, `db:"primary_language"`)
- Modify: `internal/store/repo_store.go` (add `UpdatePrimaryLanguage`)
- Modify: `internal/service/repo_service.go` (extend `OnPostReceive`)

- [ ] **Step 1: Write the migration**

```sql
-- 066_add_repo_primary_language.sql
-- Cached primary language per repo, for the language-filter chips on /stars
-- and the user repositories tab. Populated on push by RepoService.OnPostReceive.

ALTER TABLE repositories ADD COLUMN IF NOT EXISTS primary_language TEXT;
CREATE INDEX IF NOT EXISTS idx_repositories_primary_language ON repositories(primary_language);
```

Note: the table is `repositories` (verify with `\d repositories`). The original plan said `repos` — that was an error.

- [ ] **Step 2: Apply + verify**

```bash
make migrate
psql "$CZ_DATABASE_DSN" -c '\d repositories' | grep primary_language
```

- [ ] **Step 3: Model + store**

Add `PrimaryLanguage *string` (`db:"primary_language"`, `json:"primary_language"`) to `model.Repository`. Add `RepoStore.UpdatePrimaryLanguage(ctx, repoID int64, lang string) error` — a single `UPDATE repositories SET primary_language = $2 WHERE id = $1`.

- [ ] **Step 4: Populate `primary_language` on push**

In Phase 1's `RepoService.OnPostReceive`, after the stats refresh, call the language service for the default branch's top language and persist it via `RepoStore.UpdatePrimaryLanguage`. If Phase 1's `LanguageService` is not yet wired when this task is implemented, gate the call behind a nil-check and leave a `// TODO(phase1): wire LanguageService` comment — chips fall back to "Unknown".

- [ ] **Step 5: Run migration + commit**

```bash
make migrate
go build ./...
git add internal/db/migrations/066_add_repo_primary_language.sql internal/model/repo.go internal/store/repo_store.go internal/service/repo_service.go
git commit -m "feat(db): migration 066 — repositories.primary_language + push-hook population"
```

---

### Task 3: `ActivityRow` component

**Files:**
- Create: `internal/view/components/activity_row.templ`
- Create: `internal/view/components/activity_row_test.go`

```go
// internal/view/components/activity_row.templ
package components

import "fmt"

type ActivityRowData struct {
    Kind       string // "pr_opened", "pr_merged", "issue_opened", "issue_closed", "comment", "star", "release"
    Actor      string
    RepoName   string
    Subject    string // "PR #341: refactor X" or "Issue #12: ..."
    SubjectURL string
    When       string // pre-formatted relative
}

templ ActivityRow(a ActivityRowData) {
    <li class="flex items-center gap-3 px-3 py-2 hover:bg-accent rounded">
        @activityIcon(a.Kind)
        <p class="flex-1 text-sm">
            <a href={ templ.SafeURL("/" + a.Actor) } class="font-medium hover:text-primary">{ a.Actor }</a>
            <span class="text-muted-foreground">{ " " + activityVerb(a.Kind) + " " }</span>
            <a href={ templ.SafeURL(a.SubjectURL) } class="text-foreground hover:text-primary">{ a.Subject }</a>
            <span class="text-muted-foreground">{ " in " }</span>
            <a href={ templ.SafeURL("/" + a.RepoName) } class="text-foreground hover:text-primary">{ a.RepoName }</a>
        </p>
        <time class="text-xs text-muted-foreground/70 font-mono">{ a.When }</time>
    </li>
}

func activityVerb(kind string) string {
    switch kind {
    case "pr_opened": return "opened"
    case "pr_merged": return "merged"
    case "issue_opened": return "opened"
    case "issue_closed": return "closed"
    case "comment": return "commented on"
    case "star": return "starred"
    case "release": return "released"
    }
    return "updated"
}

templ activityIcon(kind string) {
    // ... minimal SVG per kind
    _ = fmt.Sprintf // keep import
}
```

- [ ] Test (renders actor/subject/verb for a sample kind), regenerate (`~/go/bin/templ generate`), commit.

```bash
git add internal/view/components/activity_row.templ internal/view/components/activity_row_templ.go internal/view/components/activity_row_test.go
git commit -m "feat(ui): add ActivityRow component"
```

---

### Task 4: `/activity` page — reshape `feed.templ`

**Files:**
- Modify: `internal/view/pages/feed.templ`, `internal/handler/feed_handler.go`
- Modify: `internal/view/viewmodels.go` (add `ActivityData`)
- Modify: `internal/router/router.go`

- [ ] **Step 1: View-model**

Add to `internal/view/viewmodels.go` (alongside `AccountReposData` etc. — that is where the account-navigation feature placed page view-models):

```go
type ActivityData struct {
    BasePage
    Username string
    Events   []model.Event
    Page     int
    HasMore  bool
}
```

- [ ] **Step 2: Reuse the existing feed wiring**

There is no `EventService.RecentForUser`. The real method is `EventService.Feed(ctx, userID, page, pageSize)` (`internal/service/event_service.go:47`), already called from `feed_handler.go:29`:

```go
events, err := h.Services.Event.Feed(r.Context(), int(claims.UserID), page, pageSize+1)
```

This task is a template + view-model reshape; the service layer needs no change.

- [ ] **Step 3: Route `/activity`**

Register `/activity` alongside the existing `/feed` route, both behind `authMW`, pointing at the same handler (or a thin `PageActivity` wrapper). Both URLs render the same page during the migration.

```go
r.With(authMW).Get("/activity", h.PageActivity)
```

- [ ] **Step 4: Body**

Page header + a chronological list using `@components.ActivityRow(...)` per event, with date headers between days. Mirror `mockups/activity.html`. The page renders under the **standard global header** — it is not an `AccountSubnav` tab, so do not attach `AccountSubnav`.

- [ ] **Step 5: Commit**

```bash
~/go/bin/templ generate && make dev
git add internal/view/pages/feed.templ internal/view/pages/feed_templ.go internal/handler/feed_handler.go internal/view/viewmodels.go internal/router/router.go
git commit -m "feat(ui): reshape feed page to match activity mockup; serve at /activity"
```

---

### Task 5: `/stars` cross-repo page

**Files:**
- Create: `internal/view/pages/account_stars.templ`
- Modify: `internal/handler/account_handler.go` (add `PageAccountStars`)
- Modify: `internal/view/viewmodels.go` (add `AccountStarsData`)
- Modify: `internal/router/router.go`

- [ ] **Step 1: View-model**

```go
type AccountStarsData struct {
    BasePage
    Username  string
    Stars     []model.Repository
    Language  string   // active language chip (empty == all)
    Languages []string // distinct primary_language values for chip rendering
}
```

- [ ] **Step 2: Handler**

Append `PageAccountStars` to `internal/handler/account_handler.go`, following the shape of the existing `PageAccountRepos` (login redirect, `slog.Error` + `http.Error` on failure — there is no `h.serverError` helper).

The real star query is `StarService.ListByUser(ctx context.Context, username string) ([]model.Repository, error)` (`internal/service/star_service.go:55`) — there is **no** `ListForUser(ctx, userID)`. Pass `claims.Username`:

```go
stars, err := h.Services.Star.ListByUser(r.Context(), claims.Username)
```

Derive the language-chip list in the handler by collecting unique non-empty `PrimaryLanguage` values from `stars`, sorted alphabetically. Apply the `?language=` filter (if set) in the handler.

- [ ] **Step 3: Route**

```go
r.With(authMW).Get("/stars", h.PageAccountStars)
```

`/stars` (literal first segment) and the existing `/{owner}/stars` (`router.go:101`) coexist — chi distinguishes a literal segment from a `{param}`.

- [ ] **Step 4: Body**

Mirror `mockups/stars.html`: page title, language-filter chips above the list (sourced from `data.Languages`), then the repo list. Renders under the **standard global header** — not an `AccountSubnav` tab.

- [ ] **Step 5: Commit**

```bash
~/go/bin/templ generate && make dev
git add internal/view/pages/account_stars.templ internal/view/pages/account_stars_templ.go internal/handler/account_handler.go internal/view/viewmodels.go internal/router/router.go
git commit -m "feat(ui): add /stars cross-repo starred-repos page"
```

---

### Task 6: Reshape `user.templ` repositories tab

**Files:** Modify `internal/view/pages/user.templ`, its handler, `internal/service/repo_service.go`.

- [ ] **Step 1: Body**

When `?tab=repositories`, render a filterable list (mirror `mockups/my_repositories.html`): search box, type filter (Sources / Forks / Templates), language-filter chips (sourced from `repositories.primary_language`), status filter (Public / Private). For each repo: a **role badge** (Owner / Maintainer / Contributor), name, description, language dot, updated time.

The repositories tab renders under the **user profile's own in-page tab strip** (`user.templ` already has Overview / Repositories / … tabs) — NOT `AccountSubnav`.

- [ ] **Step 2: Role lookup**

The role badge needs a per-repo role. Add:

```go
// internal/service/repo_service.go
//
// RoleForUserOnRepo returns "owner" | "admin" | "writer" | "reader" | ""
// (empty when the user has no permissions row and isn't the repo owner).
func (s *RepoService) RoleForUserOnRepo(ctx context.Context, userID, repoID int64) (string, error) {
    repo, err := s.repos.GetByID(ctx, repoID)
    if err != nil { return "", err }
    if repo.OwnerID == userID { return "owner", nil }
    return s.perms.GetRole(ctx, userID, repoID) // "" + nil if no row
}
```

The handler should batch-resolve roles — call `permissions.ListByUser(ctx, userID)` once and zip into the repo list — to avoid an N+1.

- [ ] **Step 3: Commit**

```bash
~/go/bin/templ generate && make dev
git add internal/view/pages/user.templ internal/view/pages/user_templ.go internal/handler/ internal/service/repo_service.go
git commit -m "feat(ui): reshape user repositories tab with role badges + language filter"
```

---

### Task 7: Verify and open PR

Tests + lint + templ regen + visual sweep across `/activity`, `/stars`, and `/{user}?tab=repositories` in both themes. Run `pr-review-toolkit:silent-failure-hunter`. PR title: `feat(ui): UI overhaul phase 8 — activity, stars, repositories tab`.

---

## Self-review checklist

- [ ] Migration **066** adds only `repositories.primary_language` (table name `repositories`, not `repos`). The `pull_review_requests` table and `mentions.ref_id`/`ref_type` columns from the original plan are **not** created — review-requests use `pull_reviews`, mentions use `mentions`+`comments` (both delivered by the account-navigation feature).
- [ ] No SQL references the non-existent `repo_collaborators` table; visibility/role joins use `permissions` (migration 006).
- [ ] `repositories.primary_language` is populated by `RepoService.OnPostReceive` — TODO-guarded if Phase 1's `LanguageService` is not yet merged.
- [ ] `/activity` and `/stars` render under the standard global header — they are NOT `AccountSubnav` tabs (`AccountSubnav` has exactly: Overview, Repositories, Gists, Pull requests, Issues). Adding them as tabs is a noted follow-up, not done here.
- [ ] `/stars` handler calls `StarService.ListByUser(ctx, claims.Username)` — not the non-existent `ListForUser(ctx, userID)`.
- [ ] `/stars` (literal path) and `/{owner}/stars` (`router.go:101`) coexist.
- [ ] Feed/activity page reuses `EventService.Feed(ctx, userID, page, pageSize)` — no new service method.
- [ ] `/pulls` and `/issues` are NOT re-registered or re-built — they ship on the account-navigation feature (`PageAccountPulls` / `PageAccountIssues`).
- [ ] View-models `ActivityData`, `AccountStarsData` live in `internal/view/viewmodels.go` (alongside `AccountReposData` etc.).
- [ ] `RepoService.RoleForUserOnRepo(ctx, userID, repoID) (string, error)` exists; the repositories tab batch-loads `permissions` for the viewer to avoid N+1.
- [ ] The user-profile repositories tab renders under the profile's own in-page tab strip, not `AccountSubnav`.
