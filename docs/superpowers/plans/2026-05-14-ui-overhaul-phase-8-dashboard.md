# UI Overhaul · Phase 8 · Your Dashboard (cross-repo) — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Reshape `user.templ` repositories tab; create new pages at `/pulls`, `/issues`, `/activity`, `/stars` for cross-repo dashboards. Reshape `feed.templ` to match the activity-feed mockup. Wire cross-repo queries.

**Architecture:** One required migration (`057_phase8_dashboard.sql`) introduces the missing schema this phase depends on. It must run before any of the cross-repo store changes compile. New routes `/pulls`, `/issues`, `/activity`, `/stars` all use the existing `fragments.DashboardSubnav` component from Phase 0 (revised subnav keys: `overview`, `repositories`, `gists`, `pull_requests`, `issues`, `activity`, `topics`, `settings`).

**Migration numbering.** 053 is the last existing migration. Phase 1 = 054, Phase 3 = 055, Phase 7 = 056, **Phase 8 = 057.** Phase 8 ships a single migration that bundles three schema gaps the plan depends on:

1. Create the `pull_review_requests` table (does not exist anywhere in 001–053).
2. Add `ref_id BIGINT` and `ref_type TEXT` columns to the existing `mentions` table (migration 039 only stores `comment_id`/`user_id` — it cannot answer cross-repo "mentioned on issue/PR" queries).
3. Add a `primary_language TEXT` column to `repos` so the `/stars` and `?tab=repositories` language-filter chips have a source column. Populated on each push by extending Phase 1's `RepoService.OnPostReceive` hook.

**Prerequisites:** Phase 7 merged.

**Spec:** [2026-05-14-ui-overhaul-design.md](../specs/2026-05-14-ui-overhaul-design.md)

**Branch:** `feat/ui-overhaul-phase-8-dashboard`

---

### Task 1: Branch setup

- [ ] `git checkout main && git pull && git checkout -b feat/ui-overhaul-phase-8-dashboard`

---

### Task 2: Migration 057 — schema gaps required by this phase

**Files:**
- Create: `internal/db/migrations/057_phase8_dashboard.sql`
- Modify: `internal/store/pull_review_request_store.go` (new)
- Modify: `internal/service/pull_service.go` (add `RequestReview` / `ListReviewRequests` wrappers)
- Modify: `internal/store/stores.go` (wire `PullReviewRequestStore` into `Stores`)
- Modify: `internal/service/repo_service.go` (extend `OnPostReceive` to set `primary_language`)
- Modify: `internal/model/repo.go` (add `PrimaryLanguage *string` field, `db:"primary_language"`)

- [ ] **Step 1: Write the migration**

```sql
-- 057_phase8_dashboard.sql

-- 1. Review-request join table (drives /pulls?filter=review_requested).
CREATE TABLE IF NOT EXISTS pull_review_requests (
    id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    pull_id BIGINT NOT NULL REFERENCES pulls(id) ON DELETE CASCADE,
    user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    requested_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (pull_id, user_id)
);
CREATE INDEX IF NOT EXISTS idx_pull_review_requests_user ON pull_review_requests(user_id, pull_id);

-- 2. Generalise mentions so "mentioned" tab can filter by ref kind.
ALTER TABLE mentions ADD COLUMN IF NOT EXISTS ref_id   BIGINT;
ALTER TABLE mentions ADD COLUMN IF NOT EXISTS ref_type TEXT;
CREATE INDEX IF NOT EXISTS idx_mentions_user_ref ON mentions(user_id, ref_type, ref_id);

-- 3. Cached primary language per repo for filter chips on /stars and ?tab=repositories.
ALTER TABLE repos ADD COLUMN IF NOT EXISTS primary_language TEXT;
CREATE INDEX IF NOT EXISTS idx_repos_primary_language ON repos(primary_language);
```

- [ ] **Step 2: PullReviewRequestStore + service**

Minimal store with `Create(ctx, pullID, userID)`, `Delete(ctx, pullID, userID)`, `ListByPull(ctx, pullID)`. Wire into `Stores` struct. Expose `PullService.RequestReview` / `RemoveReviewRequest` / `ListReviewRequests` wrappers.

- [ ] **Step 3: Populate `primary_language` on push**

In Phase 1's `RepoService.OnPostReceive`, after stats refresh, call `LanguageService.Composition(ctx, repoID).TopLanguage` (the helper Phase 1 introduces). Persist via a new `RepoStore.UpdatePrimaryLanguage(ctx, repoID, lang)` method. If the language service is not yet wired when this task is implemented, gate the call behind a nil-check and leave a `// TODO(phase1)` comment — chips fall back to "Unknown".

- [ ] **Step 4: Run migration + commit**

```bash
make migrate
go test ./internal/store/ -run TestPullReviewRequestStore -v
git add internal/db/migrations/057_phase8_dashboard.sql internal/store/ internal/service/ internal/model/repo.go
git commit -m "feat(db): migration 057 — review requests, mention refs, primary_language"
```

---

### Task 3: Cross-repo PR query — TDD

**Files:**
- Modify: `internal/store/pull_store.go`
- Modify: `internal/service/pull_service.go`
- Create: `internal/service/pull_service_crossrepo_test.go`

- [ ] **Step 1: Failing test**

```go
func TestPullStore_ListForUserAcrossRepos(t *testing.T) {
    db := testDB(t)
    alice := seedUser(t, db, "alice")
    bob := seedUser(t, db, "bob")
    r := seedRepo(t, db, bob.ID, "demo")

    pAuthored := seedPull(t, db, r.ID, alice.ID, "by alice")
    _ = pAuthored

    pReviewing := seedPull(t, db, r.ID, bob.ID, "review me")
    addReviewRequest(t, db, pReviewing.ID, alice.ID)

    pAssigned := seedPull(t, db, r.ID, bob.ID, "fix me")
    addAssignee(t, db, pAssigned.ID, alice.ID)

    s := NewPullStore(db)
    rows, err := s.ListForUserAcrossRepos(context.Background(), alice.ID, "created", 50)
    if err != nil { t.Fatalf("created: %v", err) }
    if len(rows) != 1 || rows[0].Title != "by alice" {
        t.Errorf("created filter: %+v", rows)
    }

    rows, _ = s.ListForUserAcrossRepos(context.Background(), alice.ID, "review_requested", 50)
    if len(rows) != 1 || rows[0].Title != "review me" {
        t.Errorf("review_requested filter: %+v", rows)
    }

    rows, _ = s.ListForUserAcrossRepos(context.Background(), alice.ID, "assigned", 50)
    if len(rows) != 1 || rows[0].Title != "fix me" {
        t.Errorf("assigned filter: %+v", rows)
    }
}
```

- [ ] **Step 2: Implement**

```go
// in internal/store/pull_store.go

// ListForUserAcrossRepos returns PRs across every repo where the user
// matches the filter. Filter is one of "created", "assigned",
// "review_requested", "mentioned". Repos the user can't see (private
// without membership) are filtered out at the join.
func (s *PullStore) ListForUserAcrossRepos(ctx context.Context, userID int64, filter string, limit int) ([]PullListItem, error) {
    var q string
    switch filter {
    case "created":
        q = `
            SELECT p.id, p.number, p.title, p.state, p.is_draft, p.updated_at,
                   r.id AS repo_id, owner.username || '/' || r.name AS repo_full_name
            FROM pulls p
            JOIN repos r ON r.id = p.repo_id
            JOIN users owner ON owner.id = r.owner_id
            WHERE p.author_id = $1
              AND r.deleted_at IS NULL
              AND (r.private = false OR r.id IN (SELECT repo_id FROM permissions WHERE user_id = $1))
            ORDER BY p.updated_at DESC
            LIMIT $2
        `
    case "assigned":
        q = `
            SELECT p.id, p.number, p.title, p.state, p.is_draft, p.updated_at,
                   r.id AS repo_id, owner.username || '/' || r.name AS repo_full_name
            FROM pulls p
            JOIN pull_assignees pa ON pa.pull_id = p.id
            JOIN repos r ON r.id = p.repo_id
            JOIN users owner ON owner.id = r.owner_id
            WHERE pa.user_id = $1
              AND r.deleted_at IS NULL
              AND (r.private = false OR r.id IN (SELECT repo_id FROM permissions WHERE user_id = $1))
            ORDER BY p.updated_at DESC
            LIMIT $2
        `
    case "review_requested":
        q = `
            SELECT p.id, p.number, p.title, p.state, p.is_draft, p.updated_at,
                   r.id AS repo_id, owner.username || '/' || r.name AS repo_full_name
            FROM pulls p
            JOIN pull_review_requests pr ON pr.pull_id = p.id
            JOIN repos r ON r.id = p.repo_id
            JOIN users owner ON owner.id = r.owner_id
            WHERE pr.user_id = $1
              AND r.deleted_at IS NULL
              AND (r.private = false OR r.id IN (SELECT repo_id FROM permissions WHERE user_id = $1))
            ORDER BY p.updated_at DESC
            LIMIT $2
        `
    case "mentioned":
        q = `
            SELECT p.id, p.number, p.title, p.state, p.is_draft, p.updated_at,
                   r.id AS repo_id, owner.username || '/' || r.name AS repo_full_name
            FROM pulls p
            JOIN mentions m ON m.ref_id = p.id AND m.ref_type = 'pull'
            JOIN repos r ON r.id = p.repo_id
            JOIN users owner ON owner.id = r.owner_id
            WHERE m.user_id = $1
              AND r.deleted_at IS NULL
              AND (r.private = false OR r.id IN (SELECT repo_id FROM permissions WHERE user_id = $1))
            ORDER BY p.updated_at DESC
            LIMIT $2
        `
    default:
        return nil, fmt.Errorf("unknown filter: %s", filter)
    }
    var out []PullListItem
    if err := s.db.SelectContext(ctx, &out, q, userID, limit); err != nil {
        return nil, err
    }
    return out, nil
}
```

**Schema notes (verified against migrations 001–053 + new 057):**
- `pulls` has **no** `deleted_at`; soft-delete is on `repos` only (migration 051). Guard with `r.deleted_at IS NULL`, not `p.deleted_at IS NULL`.
- The repo-collaborator table is `permissions` (migration 006), not `repo_collaborators`. The visibility sub-query is `(SELECT repo_id FROM permissions WHERE user_id = $1)`.
- `pull_assignees` / `issue_assignees` come from migration 017 — names are correct.
- `pull_review_requests` and `mentions.ref_id` / `mentions.ref_type` are introduced by migration 057 (Task 2 above).

- [ ] **Step 3: Service wrapper + benchmark**

```go
func (s *PullService) ListForUserAcrossRepos(ctx context.Context, userID int64, filter string, limit int) ([]model.PullRequest, error) {
    return s.store.ListForUserAcrossRepos(ctx, userID, filter, limit)
}
```

Run a quick benchmark on the dev seed:

```bash
go test -bench BenchmarkListForUserAcrossRepos -benchtime 10s -count 3 ./internal/store/
```

If p95 > 200ms on the seed, append the following indexes to migration 057 (do NOT create a separate migration — the number is already taken by this phase):

```sql
-- Append to 057_phase8_dashboard.sql if benchmark exceeds threshold.
CREATE INDEX IF NOT EXISTS idx_pull_assignees_user  ON pull_assignees(user_id, pull_id);
CREATE INDEX IF NOT EXISTS idx_pulls_author         ON pulls(author_id, updated_at DESC);
CREATE INDEX IF NOT EXISTS idx_issue_assignees_user ON issue_assignees(user_id, issue_id);
CREATE INDEX IF NOT EXISTS idx_issues_author        ON issues(author_id, updated_at DESC);
```

(No `WHERE deleted_at IS NULL` partial indexes — neither `pulls` nor `issues` has a `deleted_at` column. `idx_pull_review_requests_user` and `idx_mentions_user_ref` are already created in Step 1.)

Skip the index addition if the benchmark passes.

- [ ] **Step 4: Run tests, commit**

```bash
go test ./internal/store/ -run TestPullStore_ListForUserAcrossRepos -v
git add internal/store/pull_store.go internal/service/pull_service.go internal/service/pull_service_crossrepo_test.go
git commit -m "feat(store): add ListForUserAcrossRepos with filter on PullStore"
```

---

### Task 4: Cross-repo issue query — TDD

Same pattern as Task 3, mirroring for `IssueStore.ListForUserAcrossRepos(ctx, userID, filter, limit)` where filter is `created` / `assigned` / `mentioned` (issues have no review-request concept). Test asserts each filter returns the right issues.

**Same schema rules apply:**
- Soft-delete column lives on `repos` only — guard with `r.deleted_at IS NULL`, never `i.deleted_at IS NULL`.
- Visibility join uses `permissions` (not `repo_collaborators`).
- `mentions` rows for issues use `ref_type = 'issue'` (after migration 057).

```bash
go test ./internal/store/ -run TestIssueStore_ListForUserAcrossRepos -v
git add internal/store/issue_store.go internal/service/issue_service.go internal/service/issue_service_crossrepo_test.go
git commit -m "feat(store): add ListForUserAcrossRepos with filter on IssueStore"
```

---

### Task 5: `ActivityRow` component

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

Test, regenerate, commit.

---

### Task 6: New routes — `/pulls`, `/issues`, `/activity`, `/stars`

**Files:**
- Modify: `internal/router/router.go`
- Create: `internal/handler/dashboard_handler.go` (or extend `page_handler.go`)
- Modify: `internal/handler/viewmodels.go` (add view-model structs — see Step 0 below)

- [ ] **Step 0: View-models**

Add the four structs to `internal/handler/viewmodels.go` (this is where every other page in the codebase keeps its data type; `internal/view/view.go` does not exist). Each embeds the standard `BasePage` so the navbar / unread badge / user menu still work:

```go
// in internal/handler/viewmodels.go

type MyPullsData struct {
    BasePage
    Username string
    Filter   string // "created" | "assigned" | "review_requested" | "mentioned"
    Pulls    []store.PullListItem
}

type MyIssuesData struct {
    BasePage
    Username string
    Filter   string // "created" | "assigned" | "mentioned"
    Issues   []store.IssueListItem
}

type ActivityData struct {
    BasePage
    Username string
    Events   []model.Event
    Page     int
    HasMore  bool
}

type MyStarsData struct {
    BasePage
    Username  string
    Stars     []model.Repository
    Language  string   // active language chip (empty == all)
    Languages []string // distinct primary_language values for chip rendering
}
```

- [ ] **Step 1: Register routes**

In `internal/router/router.go`, under the authenticated group (alongside the existing `/{owner}/stars` route at line 101):

```go
r.With(authMW).Get("/pulls",    h.PageMyPulls)
r.With(authMW).Get("/issues",   h.PageMyIssues)
r.With(authMW).Get("/activity", h.PageActivity)
r.With(authMW).Get("/stars",    h.PageMyStars)
```

`/stars` (no `{owner}` segment) and `/{owner}/stars` (existing line 101) coexist without conflict — chi treats them as separate routes because the first segment of `/stars` is a literal, not a `{param}`.

- [ ] **Step 2: Page handlers**

```go
// PageMyPulls renders the cross-repo pulls dashboard.
func (h *Handler) PageMyPulls(w http.ResponseWriter, r *http.Request) {
    claims, _ := middleware.ClaimsFromContext(r.Context())
    filter := r.URL.Query().Get("filter")
    switch filter {
    case "created", "assigned", "review_requested", "mentioned":
    default:
        filter = "created"
    }
    pulls, _ := h.Services.Pull.ListForUserAcrossRepos(r.Context(), claims.UserID, filter, 100)
    data := MyPullsData{
        BasePage: h.basePage(r),
        Username: claims.Username,
        Filter:   filter,
        Pulls:    pulls,
    }
    h.render(r, w, pages.MyPulls(data))
}
```

Same shape for `PageMyIssues`, `PageActivity`, `PageMyStars`. The `authMW` middleware on the route already guarantees `claims != nil`, so no manual redirect is needed.

- [ ] **Step 3: Wire `/stars` to existing star query**

The real signature is `StarService.ListByUser(ctx context.Context, username string) ([]model.Repository, error)` (`internal/service/star_service.go:55`). There is **no** `ListForUser(ctx, userID)` method. Pass `claims.Username`:

```go
stars, _ := h.Services.Star.ListByUser(r.Context(), claims.Username)
```

Then derive the language chip list in the handler by walking `stars` and collecting unique non-empty `PrimaryLanguage` values, sorted alphabetically.

---

### Task 7: Create `my_pulls.templ`

**Files:** Create `internal/view/pages/my_pulls.templ`. Mirror layout from `mockups/my_pulls.html`.

- [ ] **Step 1: Body**

```go
templ MyPulls(data handler.MyPullsData) {
    @layout.Base(data.BasePage, "Your pull requests") {
        @fragments.DashboardSubnav(fragments.DashboardSubnavData{
            Username: data.Username, Active: "pull_requests",
        })
        <div class="max-w-[1100px] mx-auto px-6 py-6 space-y-4">
            <h1 class="text-2xl font-semibold tracking-tight">Your pull requests</h1>
            <nav aria-label="PR filters" class="flex gap-1 border-b border-border">
                @myFilterTab(data.Filter, "created", "Created", "/pulls?filter=created")
                @myFilterTab(data.Filter, "assigned", "Assigned", "/pulls?filter=assigned")
                @myFilterTab(data.Filter, "review_requested", "Review requested", "/pulls?filter=review_requested")
                @myFilterTab(data.Filter, "mentioned", "Mentioned", "/pulls?filter=mentioned")
            </nav>
            <ul class="rounded-md border border-border bg-card divide-y divide-border">
                if len(data.Pulls) == 0 {
                    <li class="p-8">
                        @components.EmptyState("Nothing here yet.")
                    </li>
                } else {
                    for _, p := range data.Pulls {
                        @prListRowForMyPulls(p)
                    }
                }
            </ul>
        </div>
    }
}

templ myFilterTab(active, key, label, href string) {
    if active == key {
        <a href={ templ.SafeURL(href) } aria-current="page" class="px-3 py-2 text-sm font-medium text-foreground border-b-2 border-foreground -mb-px">{ label }</a>
    } else {
        <a href={ templ.SafeURL(href) } class="px-3 py-2 text-sm text-muted-foreground hover:text-foreground border-b-2 border-transparent -mb-px">{ label }</a>
    }
}

templ prListRowForMyPulls(p model.PullRequest) {
    <li>
        <a href={ templ.SafeURL(p.URL()) } class="flex items-center gap-3 px-4 py-3 hover:bg-accent">
            <span class="font-mono text-xs text-muted-foreground w-24 truncate">{ p.RepoFullName }</span>
            <span class="flex-1 text-sm truncate">{ p.Title }</span>
            <span class="text-xs text-muted-foreground font-mono">{ formatRelative(p.UpdatedAt) }</span>
        </a>
    </li>
}
```

View-model `MyPullsData` is added to `internal/handler/viewmodels.go` in Task 6, Step 0 (not `internal/view/view.go`, which doesn't exist).

- [ ] **Step 2: Commit**

```bash
~/go/bin/templ generate && make dev
git add internal/view/pages/my_pulls.templ internal/view/pages/my_pulls_templ.go internal/handler/ internal/router/router.go
git commit -m "feat(ui): add /pulls cross-repo dashboard page"
```

---

### Task 8: Create `my_issues.templ`

Same shape as Task 7 with filter values `created` / `assigned` / `mentioned`, subnav `Active: "issues"`, and an issue-rendering body. Mirror layout from `mockups/my_issues.html`. `MyIssuesData` is added in Task 6, Step 0.

```bash
~/go/bin/templ generate && make dev
git add internal/view/pages/my_issues.templ internal/view/pages/my_issues_templ.go internal/handler/
git commit -m "feat(ui): add /issues cross-repo dashboard page"
```

---

### Task 9: Reshape `feed.templ` to match the `mockups/activity.html` design

**Files:** Modify `internal/view/pages/feed.templ`, `internal/handler/feed_handler.go`.

- [ ] **Step 1: Body**

Page header + `@fragments.DashboardSubnav(..., Active: "activity")`. Body: chronological list using `@components.ActivityRow(...)` for each event. Date headers between days. Mirror layout from `mockups/activity.html`.

- [ ] **Step 2: Reuse existing feed wiring**

There is no `EventService.RecentForUser`. The real method is `EventService.Feed(ctx, userID, page, pageSize)` (`internal/service/event_service.go:47`) and it is already called from `feed_handler.go:29`:

```go
events, err := h.Services.Event.Feed(r.Context(), int(claims.UserID), page, pageSize+1)
```

This task is purely a template + viewmodel reshape; the service layer needs no change. Add `ActivityData` to `internal/handler/viewmodels.go` (already covered in Task 6, Step 0) and route `/activity` to the same handler (or add a thin `PageActivity` wrapper — both URLs `/feed` and `/activity` should render the same page during the migration).

- [ ] **Step 3: Commit**

```bash
~/go/bin/templ generate && make dev
git add internal/view/pages/feed.templ internal/view/pages/feed_templ.go internal/handler/feed_handler.go internal/handler/viewmodels.go
git commit -m "feat(ui): reshape feed page to match activity mockup"
```

---

### Task 10: Reshape user.templ repositories tab + create global `/stars` page

- [ ] **Step 1: Repositories tab in `user.templ`**

When `?tab=repositories`, render a filterable list (mirror `mockups/my_repositories.html`) with: search box, type filter (Sources / Forks / Templates), language filter chips (sourced from `repos.primary_language`), status filter (Public / Private). For each repo show a **role badge** (Owner / Maintainer / Contributor), name, description, language indicator dot, updated time.

The role badge requires a per-repo role lookup. Add the helper:

```go
// internal/service/repo_service.go
//
// RoleForUserOnRepo returns "owner" | "admin" | "writer" | "reader" | ""
// (empty if the user has no row in permissions and isn't the repo owner).
func (s *RepoService) RoleForUserOnRepo(ctx context.Context, userID, repoID int64) (string, error) {
    repo, err := s.repos.GetByID(ctx, repoID)
    if err != nil { return "", err }
    if repo.OwnerID == userID { return "owner", nil }
    return s.perms.GetRole(ctx, userID, repoID) // returns "" + nil if no row
}
```

`PageHome` / `PageUser` should batch-resolve roles by calling `permissions.ListByUser(ctx, userID)` once and zipping into the repo list — avoid N+1.

Subnav for this tab uses `Active: "repositories"`.

- [ ] **Step 2: `/stars` page**

Mirror `mockups/stars.html`. The handler skeleton from Task 6 already calls `StarService.ListByUser(ctx, claims.Username)`. Add language filter chips above the list, sourced from the distinct `PrimaryLanguage` values populated by migration 057 + the `OnPostReceive` hook.

`/stars` does **not** appear in the `DashboardSubnav` keys (`overview`, `repositories`, `gists`, `pull_requests`, `issues`, `activity`, `topics`, `settings`). Render it under separate chrome — page title only, no subnav active state. The existing `/{owner}/stars` route at `router.go:101` continues to serve any user's public stars.

- [ ] **Step 3: Commit**

```bash
~/go/bin/templ generate && make dev
git add internal/view/pages/user.templ internal/view/pages/user_templ.go internal/view/pages/my_stars.templ internal/view/pages/my_stars_templ.go internal/handler/ internal/service/repo_service.go internal/router/router.go
git commit -m "feat(ui): reshape user repositories tab + add /stars cross-repo page"
```

---

### Task 11: Verify and open PR

Tests + lint + templ regen + visual sweep across five dashboard pages (`/pulls`, `/issues`, `/activity`, `/stars`, `/{user}?tab=repositories`). Run `pr-review-toolkit:silent-failure-hunter`. PR title: `feat(ui): UI overhaul phase 8 — cross-repo dashboard`.

---

## Self-review checklist

- [ ] Migration **057** is the only new migration in this phase. Phase 1 = 054, Phase 3 = 055, Phase 7 = 056, Phase 8 = 057.
- [ ] Migration 057 creates `pull_review_requests`, adds `mentions.ref_id` / `mentions.ref_type` columns + composite index, and adds `repos.primary_language`.
- [ ] No SQL anywhere references `pulls.deleted_at` or `issues.deleted_at` — soft-delete lives on `repos` (migration 051) only.
- [ ] No SQL references the non-existent `repo_collaborators` table; visibility joins use `permissions` (migration 006).
- [ ] Each cross-repo query (`ListForUserAcrossRepos`) filters out private repos the user can't see via `(r.private = false OR r.id IN (SELECT repo_id FROM permissions WHERE user_id = $1))`.
- [ ] `/stars` handler calls `StarService.ListByUser(ctx, claims.Username)` (not the non-existent `ListForUser(ctx, userID)`).
- [ ] Feed/activity page reuses the existing `EventService.Feed(ctx, userID, page, pageSize)` — no new service method.
- [ ] DashboardSubnav `Active` keys match the Phase 0 set exactly: `overview`, `repositories`, `gists`, `pull_requests`, `issues`, `activity`, `topics`, `settings`.
  - `/pulls` → `Active: "pull_requests"`
  - `/issues` → `Active: "issues"`
  - `/activity` → `Active: "activity"`
  - `user.templ?tab=repositories` → `Active: "repositories"`
  - `/stars` → not in subnav; renders without subnav chrome.
- [ ] `/stars` (current user, literal path) and `/{owner}/stars` (any user, existing `router.go:101`) coexist; chi distinguishes literal vs `{owner}` segments.
- [ ] View-models `MyPullsData`, `MyIssuesData`, `ActivityData`, `MyStarsData` are declared in `internal/handler/viewmodels.go`, not `internal/view/view.go` (which does not exist).
- [ ] No `pageNames` slice exists in `router.go` — nothing to update there. (Should one be added later, the four names to register are `MyPulls`, `MyIssues`, `Activity`, `MyStars`.)
- [ ] `RepoService.RoleForUserOnRepo(ctx, userID, repoID) (string, error)` exists and is batch-friendly (the repositories tab pre-loads `permissions` for the viewer to avoid N+1).
- [ ] `repos.primary_language` is populated by `RepoService.OnPostReceive` (the Phase 1 hook) — TODO-guarded if Phase 1's `LanguageService` is not yet merged when this task runs.
- [ ] Filter tabs preserve `?filter=` in the URL so back/refresh works; handlers reject unknown filter values and fall back to `created`.
