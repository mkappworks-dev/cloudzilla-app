# UI Overhaul · Phase 3 · Repo Activity — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Port `commits.templ`, `contributors.templ`, `pulse.templ`, `dependencies.templ` to match mockups. Add `ContributorStatsService` (migration 055). Reuse the existing `DependencyService` (`internal/service/dependency_service.go`, store-backed, populated on push via `ParseAndStore`) — no parser changes here. Add `Sparkline`, `MiniChart`, `DependencyGroup` components.

**Architecture:** Migration 055 adds `contributor_week_stats` populated on push. The dependencies page reads from `DependencyService.ListByRepo(ctx, repoID)` which returns `[]model.RepoDependency` already grouped by package manager via the `PackageMgr` field and flagged `IsDev`.

**Prerequisites:** Phase 1 merged (provides `RepoService.OnPostReceive` shared hook + `CommitStatsService`) and Phase 2 merged.

**Spec:** [2026-05-14-ui-overhaul-design.md](../specs/2026-05-14-ui-overhaul-design.md)

**Branch:** `feat/ui-overhaul-phase-3-repo-activity`

---

### Task 1: Branch setup

- [ ] Create branch: `git checkout main && git pull && git checkout -b feat/ui-overhaul-phase-3-repo-activity`

---

### Task 2: Migration 055 — `contributor_week_stats`

**Files:**
- Create: `internal/db/migrations/055_create_contributor_week_stats.sql`

- [ ] **Step 1: Write migration**

```sql
-- 055_create_contributor_week_stats.sql
-- Per-(repo, user, week) commit/lines aggregate. Backs the per-repo
-- contributors page. Week is the Monday-of-week date (UTC).
-- Populated on each push via RepoService.PostReceive hook.

CREATE TABLE IF NOT EXISTS contributor_week_stats (
    id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    repo_id BIGINT NOT NULL REFERENCES repos(id) ON DELETE CASCADE,
    user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    week DATE NOT NULL,
    commits INT NOT NULL DEFAULT 0,
    additions INT NOT NULL DEFAULT 0,
    deletions INT NOT NULL DEFAULT 0,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (repo_id, user_id, week)
);

CREATE INDEX IF NOT EXISTS idx_contributor_week_stats_repo_user
    ON contributor_week_stats (repo_id, user_id, week DESC);
```

- [ ] **Step 2: Apply + verify**

```bash
make migrate
psql "$CZ_DATABASE_DSN" -c '\d contributor_week_stats'
```

- [ ] **Step 3: Commit**

```bash
git add internal/db/migrations/055_create_contributor_week_stats.sql
git commit -m "feat(db): migration 055 — contributor_week_stats aggregate"
```

---

### Task 3: `ContributorStatsStore` + service — TDD

**Files:**
- Create: `internal/store/contributor_stats_store.go`
- Create: `internal/store/contributor_stats_store_test.go`
- Create: `internal/service/contributor_stats_service.go`
- Create: `internal/service/contributor_stats_service_test.go`
- Modify: `internal/store/stores.go`, `internal/service/services.go`, `internal/service/repo_service.go` (PostReceive hook)

- [ ] **Step 1: Store test**

```go
// internal/store/contributor_stats_store_test.go
package store

import (
    "context"
    "testing"
    "time"
)

func TestContributorStatsStore_UpsertAndList(t *testing.T) {
    db := testDB(t)
    s := NewContributorStatsStore(db)
    ctx := context.Background()
    user := seedUser(t, db, "alice")
    repo := seedRepo(t, db, user.ID, "demo")

    week := time.Date(2026, 5, 11, 0, 0, 0, 0, time.UTC) // Monday
    if err := s.UpsertStats(ctx, repo.ID, user.ID, week, 3, 100, 20); err != nil {
        t.Fatalf("upsert: %v", err)
    }
    // Idempotent re-upsert with new values
    if err := s.UpsertStats(ctx, repo.ID, user.ID, week, 5, 150, 30); err != nil {
        t.Fatalf("upsert: %v", err)
    }

    rows, err := s.ListForRepo(ctx, repo.ID)
    if err != nil {
        t.Fatalf("list: %v", err)
    }
    if len(rows) != 1 || rows[0].Commits != 5 {
        t.Errorf("expected 1 row with 5 commits, got %+v", rows)
    }
}
```

- [ ] **Step 2: Run, verify FAIL, implement store, verify PASS**

```go
// internal/store/contributor_stats_store.go
package store

import (
    "context"
    "time"

    "github.com/jmoiron/sqlx"
)

type ContributorWeekStat struct {
    UserID    int64     `db:"user_id"`
    Username  string    `db:"username"` // joined
    Week      time.Time `db:"week"`
    Commits   int       `db:"commits"`
    Additions int       `db:"additions"`
    Deletions int       `db:"deletions"`
}

type ContributorStatsStore struct{ db *sqlx.DB }

func NewContributorStatsStore(db *sqlx.DB) *ContributorStatsStore {
    return &ContributorStatsStore{db: db}
}

func (s *ContributorStatsStore) UpsertStats(ctx context.Context, repoID, userID int64, week time.Time, commits, additions, deletions int) error {
    const q = `
        INSERT INTO contributor_week_stats (repo_id, user_id, week, commits, additions, deletions, updated_at)
        VALUES ($1, $2, $3, $4, $5, $6, NOW())
        ON CONFLICT (repo_id, user_id, week)
        DO UPDATE SET commits=EXCLUDED.commits, additions=EXCLUDED.additions, deletions=EXCLUDED.deletions, updated_at=NOW()
    `
    _, err := s.db.ExecContext(ctx, q, repoID, userID, mondayUTC(week), commits, additions, deletions)
    return err
}

func (s *ContributorStatsStore) ListForRepo(ctx context.Context, repoID int64) ([]ContributorWeekStat, error) {
    const q = `
        SELECT c.user_id, u.username, c.week, c.commits, c.additions, c.deletions
        FROM contributor_week_stats c JOIN users u ON u.id = c.user_id
        WHERE c.repo_id = $1
        ORDER BY c.week ASC, u.username
    `
    var out []ContributorWeekStat
    err := s.db.SelectContext(ctx, &out, q, repoID)
    return out, err
}

func mondayUTC(t time.Time) time.Time {
    t = t.UTC().Truncate(24 * time.Hour)
    for t.Weekday() != time.Monday {
        t = t.AddDate(0, 0, -1)
    }
    return t
}
```

- [ ] **Step 3: Service**

```go
// internal/service/contributor_stats_service.go
package service

import (
    "context"
    "time"

    "github.com/mkappworks-dev/cloudzilla-app/internal/store"
)

type ContributorStatsService struct {
    stats *store.ContributorStatsStore
    users *store.UserStore
}

func NewContributorStatsService(stats *store.ContributorStatsStore, users *store.UserStore) *ContributorStatsService {
    return &ContributorStatsService{stats: stats, users: users}
}

// ContributorWithTimeline is the per-contributor row used by the
// contributors page: name, total commits/+/-, and a week-by-week
// timeline used by the sparkline.
type ContributorWithTimeline struct {
    Username  string
    Commits   int
    Additions int
    Deletions int
    Timeline  []int // weekly commit counts oldest→newest
}

func (s *ContributorStatsService) ForRepo(ctx context.Context, repoID int64) ([]ContributorWithTimeline, error) {
    rows, err := s.stats.ListForRepo(ctx, repoID)
    if err != nil {
        return nil, err
    }
    // Group by user.
    type bucket struct {
        commits, additions, deletions int
        timeline                       map[time.Time]int
    }
    groups := make(map[int64]*bucket)
    names := make(map[int64]string)
    var minWeek, maxWeek time.Time
    for _, r := range rows {
        g, ok := groups[r.UserID]
        if !ok {
            g = &bucket{timeline: map[time.Time]int{}}
            groups[r.UserID] = g
            names[r.UserID] = r.Username
        }
        g.commits += r.Commits
        g.additions += r.Additions
        g.deletions += r.Deletions
        g.timeline[r.Week] = r.Commits
        if minWeek.IsZero() || r.Week.Before(minWeek) {
            minWeek = r.Week
        }
        if r.Week.After(maxWeek) {
            maxWeek = r.Week
        }
    }
    // Build dense timeline for each user spanning minWeek..maxWeek.
    var out []ContributorWithTimeline
    for uid, g := range groups {
        var tl []int
        if !minWeek.IsZero() {
            for w := minWeek; !w.After(maxWeek); w = w.AddDate(0, 0, 7) {
                tl = append(tl, g.timeline[w])
            }
        }
        out = append(out, ContributorWithTimeline{
            Username: names[uid], Commits: g.commits, Additions: g.additions, Deletions: g.deletions, Timeline: tl,
        })
    }
    // Sort by commits desc.
    for i := 1; i < len(out); i++ {
        for j := i; j > 0 && out[j-1].Commits < out[j].Commits; j-- {
            out[j-1], out[j] = out[j], out[j-1]
        }
    }
    return out, nil
}

// IngestCommit updates the per-week aggregate for one commit. Called
// from the post-receive hook (see RepoService).
func (s *ContributorStatsService) IngestCommit(ctx context.Context, repoID, userID int64, when time.Time, additions, deletions int) error {
    week := store.MondayUTC(when)
    existing, _ := s.stats.GetOne(ctx, repoID, userID, week) // store helper returning zero on miss
    return s.stats.UpsertStats(ctx, repoID, userID, week,
        existing.Commits+1, existing.Additions+additions, existing.Deletions+deletions)
}
```

Add `MondayUTC` (exported) helper to `store` package and `ContributorStatsStore.GetOne(...)` returning zero values on miss.

- [ ] **Step 4: Wire into the shared post-receive hook (Phase 1 prerequisite)**

Phase 1 introduces `RepoService.OnPostReceive(ctx, repoID, commits)` invoked from BOTH `internal/handler/git_http.go` (HTTP smart protocol) and `internal/ssh/server.go` (SSH receive). Extend that same hook here — do NOT introduce a new dispatch site. Inside `OnPostReceive`, after the existing `commitStats.Ingest(...)` call from Phase 1, append the contributor ingest:

```go
for _, c := range commits {
    user, _ := s.User.GetByEmail(ctx, c.AuthorEmail)
    if user == nil {
        continue
    }
    // Reuse existing CodeService.GetCommit, which already returns TotalAdded/TotalDeleted.
    detail, err := s.Code.GetCommit(repo.OwnerName, repo.Name, c.SHA)
    if err != nil {
        continue
    }
    _ = s.ContributorStats.IngestCommit(ctx, repo.ID, user.ID, c.AuthorTime,
        detail.TotalAdded, detail.TotalDeleted)
}
```

Use `commit.AuthorTime` (not `AuthorWhen` — see `internal/service/code_service_commit.go:21`). Use the existing `CommitDetail.TotalAdded` / `TotalDeleted` fields returned by `CodeService.GetCommit` — do NOT add a new `CodeService.LinesChanged` helper.

- [ ] **Step 5: Wire into stores and services structs**

In `internal/store/stores.go`: `ContributorStats *ContributorStatsStore`, wired in `New`.
In `internal/service/services.go`: `ContributorStats *ContributorStatsService`, wired in `New`.

- [ ] **Step 6: Run tests, build, commit**

```bash
go test ./... && go build ./...
git add internal/store/contributor_stats_store.go internal/store/contributor_stats_store_test.go internal/store/stores.go internal/service/contributor_stats_service.go internal/service/contributor_stats_service_test.go internal/service/services.go internal/service/repo_service.go
git commit -m "feat(service): add ContributorStatsService wired into shared post-receive hook"
```

---

### Task 4: `Sparkline` and `MiniChart` components

**Files:**
- Create: `internal/view/components/sparkline.templ`
- Create: `internal/view/components/sparkline_test.go`
- Create: `internal/view/components/mini_chart.templ`
- Create: `internal/view/components/mini_chart_test.go`

- [ ] **Step 1: Sparkline test + impl**

```go
// internal/view/components/sparkline.templ
package components

import (
    "fmt"
    "strings"
)

// Sparkline renders an inline SVG polyline from `values`. `width` and
// `height` default to 80x16 if zero. All default-application logic
// happens inside plain Go helpers because templ does not allow mutating
// expressions inside the template body.
templ Sparkline(values []int, width, height int) {
    if len(values) == 0 {
        <span class="text-xs text-muted-foreground/70">—</span>
    } else {
        <svg width={ sparkW(width) } height={ sparkH(height) } viewBox={ sparkViewBox(width, height) } aria-hidden="true">
            <polyline fill="none" stroke="currentColor" stroke-width="1.5" points={ sparkPoints(values, sparkWInt(width), sparkHInt(height)) }/>
        </svg>
    }
}

// Plain-Go helpers. Defaults match the documented 80x16 fallback.
func sparkWInt(w int) int { if w == 0 { return 80 }; return w }
func sparkHInt(h int) int { if h == 0 { return 16 }; return h }
func sparkW(w int) string  { return fmt.Sprintf("%d", sparkWInt(w)) }
func sparkH(h int) string  { return fmt.Sprintf("%d", sparkHInt(h)) }
func sparkViewBox(w, h int) string {
    return fmt.Sprintf("0 0 %d %d", sparkWInt(w), sparkHInt(h))
}

func sparkPoints(values []int, w, h int) string {
    max := 1
    for _, v := range values {
        if v > max {
            max = v
        }
    }
    n := len(values)
    if n == 1 {
        return fmt.Sprintf("0,%d %d,%d", h/2, w, h/2)
    }
    step := float64(w) / float64(n-1)
    var b strings.Builder
    for i, v := range values {
        x := float64(i) * step
        y := float64(h) - (float64(v)/float64(max))*float64(h)
        fmt.Fprintf(&b, "%.1f,%.1f ", x, y)
    }
    return strings.TrimSpace(b.String())
}
```

Test asserts that rendering 7 sequential values produces a `<polyline>` with 7 point pairs.

- [ ] **Step 2: MiniChart**

Similar shape — bar chart variant for pulse stats. Each bar is a rect; height encodes value normalized to max.

```go
// internal/view/components/mini_chart.templ
package components

import "fmt"

templ MiniChart(values []int, width, height int) {
    if len(values) == 0 {
        <span class="text-xs text-muted-foreground/70">no data</span>
    } else {
        <svg width={ miniW(width) } height={ miniH(height) } viewBox={ miniViewBox(width, height) } aria-hidden="true">
            for _, bar := range miniBars(values, miniWInt(width), miniHInt(height)) {
                <rect x={ fmt.Sprintf("%.1f", bar.X) } y={ fmt.Sprintf("%.1f", bar.Y) } width={ fmt.Sprintf("%.1f", bar.W) } height={ fmt.Sprintf("%.1f", bar.H) } fill="currentColor"/>
            }
        </svg>
    }
}

// Plain-Go defaulting helpers (templ disallows mutating expressions in the body).
func miniWInt(w int) int { if w == 0 { return 120 }; return w }
func miniHInt(h int) int { if h == 0 { return 32 }; return h }
func miniW(w int) string  { return fmt.Sprintf("%d", miniWInt(w)) }
func miniH(h int) string  { return fmt.Sprintf("%d", miniHInt(h)) }
func miniViewBox(w, h int) string {
    return fmt.Sprintf("0 0 %d %d", miniWInt(w), miniHInt(h))
}

type miniBar struct{ X, Y, W, H float64 }

func miniBars(values []int, w, h int) []miniBar {
    max := 1
    for _, v := range values {
        if v > max {
            max = v
        }
    }
    n := len(values)
    bw := float64(w) / float64(n)
    gap := 1.0
    out := make([]miniBar, 0, n)
    for i, v := range values {
        bh := float64(v) / float64(max) * float64(h)
        out = append(out, miniBar{
            X: float64(i) * bw, Y: float64(h) - bh,
            W: bw - gap, H: bh,
        })
    }
    return out
}
```

- [ ] **Step 3: Regenerate + test**

```bash
~/go/bin/templ generate && go test ./internal/view/components/ -run 'TestSparkline|TestMiniChart' -v
```

- [ ] **Step 4: Commit**

```bash
git add internal/view/components/sparkline.templ internal/view/components/sparkline_templ.go internal/view/components/sparkline_test.go internal/view/components/mini_chart.templ internal/view/components/mini_chart_templ.go internal/view/components/mini_chart_test.go
git commit -m "feat(ui): add Sparkline and MiniChart SVG components"
```

---

### Task 5: Reuse the existing `DependencyService` (no parser changes)

`internal/service/dependency_service.go` is already in place from migration 053 / Phase 15.3. It exposes:

- `ParseAndStore(ctx, repo)` — called on push; reads `go.mod`, `package.json`, `requirements.txt`, `Cargo.toml` from the default branch via `CodeService.GetRawBlob`, parses them with `parseGoMod` / `parsePackageJSON` / `parseRequirementsTxt` / `parseCargoToml`, and stores rows via `DependencyStore.Replace`.
- `ListByRepo(ctx, repoID) ([]model.RepoDependency, error)` — used by the dependencies page.

The dependencies page must consume `ListByRepo` directly. Each `model.RepoDependency` row carries `PackageMgr`, `Package`, `Version`, `IsDev`. There is **no license field**, no in-memory `DependencyManifest` / `DependencySnapshot` / `GoModFile` / `GoModRequire` type — do **not** introduce them. Do **not** add a `ParseGoMod` exported helper; the existing unexported `parseGoMod` is sufficient and the parser is already covered by tests.

- [ ] **Step 1: Confirm coverage**

```bash
go test ./internal/service/ -run TestDependency -v
```

Existing tests live in `internal/service/dependency_service_test.go` (verify on first run). If a regression is needed, add it there; otherwise this task is a no-op for service code.

- [ ] **Step 2: Group helper (handler-side, used by Task 10)**

Add a tiny grouping helper next to the dependencies page handler — not in `service` — that pivots `[]model.RepoDependency` into `[]components.DependencyGroupData` (one group per `PackageMgr`, sorted alphabetically):

```go
// internal/handler/page_dependencies.go (or wherever PageDependencies lives)
func groupDependencies(deps []model.RepoDependency) []components.DependencyGroupData {
    byMgr := map[string][]components.DependencyRow{}
    for _, d := range deps {
        byMgr[d.PackageMgr] = append(byMgr[d.PackageMgr], components.DependencyRow{
            Package: d.Package, Version: d.Version, IsDev: d.IsDev,
        })
    }
    out := make([]components.DependencyGroupData, 0, len(byMgr))
    for mgr, rows := range byMgr {
        out = append(out, components.DependencyGroupData{
            PackageMgr: mgr,
            Label:      manifestLabel(mgr), // "go.mod", "package.json", ...
            Rows:       rows,
        })
    }
    sort.Slice(out, func(i, j int) bool { return out[i].PackageMgr < out[j].PackageMgr })
    return out
}

func manifestLabel(mgr string) string {
    switch mgr {
    case "go":    return "go.mod"
    case "npm":   return "package.json"
    case "pip":   return "requirements.txt"
    case "cargo": return "Cargo.toml"
    default:      return mgr
    }
}
```

---

### Task 6: `DependencyGroup` component

**Files:**
- Create: `internal/view/components/dependency_group.templ`
- Create: `internal/view/components/dependency_group_test.go`

The mockup (`mockups/dependencies.html`) shows three columns per manifest table: **Package / Version / Type** (where Type is a `prod` or `dev` pill). There is **no license column**. The row data comes from `model.RepoDependency` rows grouped by `PackageMgr` (see Task 5's `groupDependencies` helper).

```go
// internal/view/components/dependency_group.templ
package components

// DependencyGroupData renders one manifest table.
// PackageMgr is the raw key from model.RepoDependency.PackageMgr ("go",
// "npm", "pip", "cargo"); Label is the human filename ("go.mod", ...).
type DependencyGroupData struct {
    PackageMgr string
    Label      string
    Rows       []DependencyRow
}

type DependencyRow struct {
    Package string
    Version string
    IsDev   bool // -> "dev" pill; otherwise "prod"
}

templ DependencyGroup(d DependencyGroupData) {
    <section class="rounded-md border border-border bg-card" aria-labelledby={ "dep-" + d.PackageMgr }>
        <header class="px-4 py-3 border-b border-border flex items-center justify-between">
            <h3 id={ "dep-" + d.PackageMgr } class="font-medium text-sm">{ d.Label }</h3>
            <span class="text-xs text-muted-foreground font-mono">{ d.PackageMgr }</span>
        </header>
        <table class="w-full text-sm">
            <thead class="sr-only">
                <tr><th>Package</th><th>Version</th><th>Type</th></tr>
            </thead>
            <tbody class="divide-y divide-border">
                for _, r := range d.Rows {
                    <tr class="hover:bg-muted/40">
                        <td class="px-4 py-2 font-mono truncate">{ r.Package }</td>
                        <td class="px-4 py-2 text-xs text-muted-foreground font-mono">{ r.Version }</td>
                        <td class="px-4 py-2">
                            if r.IsDev {
                                <span class="inline-flex items-center px-2 py-0.5 rounded-full border text-[11px] font-medium pill-dev">dev</span>
                            } else {
                                <span class="inline-flex items-center px-2 py-0.5 rounded-full border text-[11px] font-medium pill-prod">prod</span>
                            }
                        </td>
                    </tr>
                }
            </tbody>
        </table>
    </section>
}
```

- [ ] **Step 1: Test + regenerate + commit**

Test asserts that rendering a manifest with two rows (one `IsDev=true`, one `IsDev=false`) renders both package names + versions and produces both a `pill-dev` and a `pill-prod` span.

```bash
~/go/bin/templ generate && go test ./internal/view/components/ -run TestDependencyGroup -v
git add internal/view/components/dependency_group.templ internal/view/components/dependency_group_templ.go internal/view/components/dependency_group_test.go
git commit -m "feat(ui): add DependencyGroup component"
```

---

### Task 7: Port `commits.templ`

**Files:** Modify `internal/view/pages/commits.templ` and the page handler.

- [ ] **Step 1: Apply class translation, group by day**

Translate classes via map. Group the commits returned by `CodeService.GetCommits` by `commit.AuthorTime.Format("2006-01-02")` — the field is `AuthorTime`, not `AuthorWhen` (see `internal/service/code_service_commit.go:21`, `:68`). Render each group with a date header, then a list of commits showing avatar + author + message + short SHA + "Browse files" link.

Include `RepoSubnav` with `Active: "code"`. The corrected subnav tab keys (from revised Phase 0) are `code`, `issues`, `pull_requests`, `actions`, `discussions`, `projects`, `wiki`, `releases`, `settings` — there is no `commits` or `insights` tab, so the Commits page lives under `code`.

- [ ] **Step 2: Regenerate, verify, commit**

```bash
~/go/bin/templ generate && make dev
git add internal/view/pages/commits.templ internal/view/pages/commits_templ.go
git commit -m "feat(ui): port commits page to match mockup"
```

---

### Task 8: Port `contributors.templ` using `Sparkline`

**Files:** Modify `internal/view/pages/contributors.templ`, `internal/handler/page_handler.go`, `internal/view/viewmodels.go` (canonical view-model definitions live there; `internal/handler/viewmodels.go` is just a re-export shim).

- [ ] **Step 1: Extend `ContributorsData` in `internal/view/viewmodels.go`**

```go
type ContributorsData struct {
    BasePage BasePage
    Repo     *model.Repository
    Rows     []service.ContributorWithTimeline
}
```

Include `RepoSubnav` with `Active: "code"` when rendering the page (contributors lives under the Code tab — no `insights`/`contributors` tab in the revised subnav).

- [ ] **Step 2: Populate**

```go
data.Rows, _ = h.Services.ContributorStats.ForRepo(ctx, repo.ID)
```

- [ ] **Step 3: Rewrite body**

For each row: avatar, username, total commits, additions/deletions, and `@components.Sparkline(row.Timeline, 80, 24)`. Use existing `components.Table`.

- [ ] **Step 4: Regenerate, verify, commit**

```bash
~/go/bin/templ generate && make dev
git add internal/view/pages/contributors.templ internal/view/pages/contributors_templ.go internal/handler/
git commit -m "feat(ui): port contributors page to match mockup (with sparklines)"
```

---

### Task 9: Port `pulse.templ` using `MiniChart`

**Files:** Modify `internal/view/pages/pulse.templ`, `internal/handler/page_handler.go`, `internal/view/viewmodels.go` (canonical definitions; `internal/handler/viewmodels.go` is a re-export shim), `internal/service/commit_stats_service.go`.

- [ ] **Step 1: Extend `PulseData` in `internal/view/viewmodels.go`**

```go
type PulseData struct {
    BasePage        BasePage
    Repo            *model.Repository
    IssuesOpened    int
    IssuesClosed    int
    PullsOpened     int
    PullsMerged     int
    CommitsLast30   []int // weekly counts, 5 buckets
    IssuesLast30    []int
    PullsLast30     []int
}
```

- [ ] **Step 2: Populate using EXISTING service methods**

The real method names on the existing services (verified in `internal/service/issue_service.go:181-186` and `internal/service/pull_service.go:144`) are `CountCreatedSince` and `CountClosedSince` (issues) and `CountCreatedSince` plus a `CountMergedSince`-style method on `PullService` (verify; add if absent). There is **no** `Issue.CountForRepo(state, since)` and no `Pull.CountForRepo`.

```go
lastMonth := time.Now().AddDate(0, 0, -30)
data.IssuesOpened, _  = h.Services.Issue.CountCreatedSince(ctx, repo.ID, lastMonth)
data.IssuesClosed, _  = h.Services.Issue.CountClosedSince(ctx, repo.ID, lastMonth)
data.PullsOpened, _   = h.Services.Pull.CountCreatedSince(ctx, repo.ID, lastMonth)
// PullService currently exposes CountCreatedSince but no CountMergedSince —
// add `CountMergedSince(ctx, repoID, since)` to PullService + PullStore as a
// sub-step (mirror IssueService.CountClosedSince: `WHERE state='merged' AND merged_at >= $2`).
data.PullsMerged, _   = h.Services.Pull.CountMergedSince(ctx, repo.ID, lastMonth)
data.CommitsLast30, _ = h.Services.CommitStats.WeeklyForRepo(ctx, repo.ID, 5)
```

**Sub-steps actually required here:**

1. Add `PullService.CountMergedSince(ctx, repoID, since) (int, error)` and the backing `PullStore.CountMergedSince` SQL.
2. Add `CommitStatsService.WeeklyForRepo(ctx, repoID, weeks int) ([]int, error)` returning weekly commit counts oldest→newest. Phase 1 owns `CommitStatsService`; if Phase 1 has not yet added this method when Phase 3 starts, add it here and update Phase 1's changelog. The query buckets `commit_stats.committed_at` by ISO week and returns a dense `[]int` of length `weeks`.
3. Wire similar weekly aggregates for issues/PRs (`IssuesLast30`, `PullsLast30`) — either by adding `IssueStore.WeeklyCreated` / `PullStore.WeeklyCreated`, or by computing in Go from `CountCreatedSince` repeated per week if the cost is acceptable. Prefer the SQL approach.

Do **not** invent `Issue.CountForRepo` or `Pull.CountForRepo` — those signatures do not exist.

- [ ] **Step 3: Rewrite body**

Three stat cards top: Issues / Pull requests / Commits. Each has a number + secondary breakdown + `@components.MiniChart(data.IssuesLast30, 0, 0)`. Include `RepoSubnav` with `Active: "code"`.

- [ ] **Step 4: Regenerate, verify, commit**

```bash
~/go/bin/templ generate && make dev
git add internal/view/pages/pulse.templ internal/view/pages/pulse_templ.go internal/handler/ internal/service/ internal/store/
git commit -m "feat(ui): port pulse page to match mockup (mini charts + 30-day rollup)"
```

---

### Task 10: Port `dependencies.templ` (route already exists)

**Files:**
- Modify: `internal/view/pages/dependencies.templ`
- Modify: `internal/handler/page_handler.go` (or wherever `PageDependencies` is defined)
- Modify: `internal/view/viewmodels.go` (extend `DependenciesData`)

- [ ] **Step 1: Verify existing route — do NOT register a new one**

The route is already registered in `internal/router/router.go` (around line 122):

```go
r.With(optAuthMW).Get("/{owner}/{repo}/network/dependencies", h.PageDependencies)
```

Do **not** add it again, and do **not** add a `/dependencies` alias. Extend the existing `PageDependencies` handler instead.

- [ ] **Step 2: Populate `DependenciesData` using existing service**

```go
deps, err := h.Services.Dependency.ListByRepo(ctx, repo.ID)
if err != nil {
    // log + render empty groups; do not 500 — empty is a valid state.
    deps = nil
}
data.Groups = groupDependencies(deps) // see helper from Task 5 Step 2
```

`groupDependencies` returns `[]components.DependencyGroupData` keyed by `PackageMgr` with rows carrying `Package`, `Version`, `IsDev`. No license field, no `Manifests`/`Requires` wrapper types.

- [ ] **Step 3: Rewrite body**

```go
<div class="space-y-4">
    if len(data.Groups) == 0 {
        @components.EmptyState("No dependency manifests detected at the default branch.")
    } else {
        for _, g := range data.Groups {
            @components.DependencyGroup(g)
        }
    }
</div>
```

Include `RepoSubnav` with `Active: "code"` (no `insights`/`network` tab in the revised subnav; dependencies lives under Code).

- [ ] **Step 4: Regenerate, verify, commit**

```bash
~/go/bin/templ generate && make dev
# Open /<owner>/<repo>/network/dependencies, confirm groups render
git add internal/view/pages/dependencies.templ internal/view/pages/dependencies_templ.go internal/handler/ internal/view/viewmodels.go
git commit -m "feat(ui): port dependencies page to match mockup (Package/Version/Type)"
```

---

### Task 11: Verify and open PR

- [ ] Tests + lint + templ regen + visual sweep + silent-failure-hunter (as Phase 1/2 pattern).
- [ ] Push and open PR with title `feat(ui): UI overhaul phase 3 — repo activity`. PR body summarizes the four pages, new components, and migration 055.

---

## Self-review checklist

- [ ] Migration 055 is sequential after 054.
- [ ] `mondayUTC` always returns Monday regardless of input weekday.
- [ ] `Sparkline` handles single-value and zero-value input without panic.
- [ ] Dependencies page uses `DependencyService.ListByRepo` (existing); no new parser, no `ParseGoMod` export, no `DependencyManifest`/`DependencySnapshot`/`GoModRequire` types introduced.
- [ ] Route `/{owner}/{repo}/network/dependencies` is **not** re-registered — the existing route in `router.go:122` is reused.
- [ ] Dependencies page columns match the mockup: **Package / Version / Type** (`prod`/`dev` pill). No license column.
- [ ] Contributor ingest plugs into `RepoService.OnPostReceive` (Phase 1's shared hook) — no new dispatch site in `git_http.go` or `ssh/server.go`.
- [ ] `commit.AuthorTime` is used (the model field is `AuthorTime`, not `AuthorWhen`).
- [ ] Lines-changed numbers come from `CommitDetail.TotalAdded` / `TotalDeleted` returned by `CodeService.GetCommit`; no new `CodeService.LinesChanged` helper.
- [ ] Issue/PR counts use `CountCreatedSince` / `CountClosedSince` (issues) and `CountCreatedSince` / new `CountMergedSince` (pulls). No `CountForRepo(state, since)` is referenced.
- [ ] `CommitStatsService.WeeklyForRepo` is added (here or earlier in Phase 1) before being called.
- [ ] All four pages include `RepoSubnav` with `Active: "code"` — there is no `commits`, `contributors`, `insights`, or `network` tab in the corrected subnav (`code`, `issues`, `pull_requests`, `actions`, `discussions`, `projects`, `wiki`, `releases`, `settings`).
- [ ] View-model structs are added in `internal/view/viewmodels.go` (the canonical location); `internal/handler/viewmodels.go` is only a re-export shim and does not need new definitions.
- [ ] Templ files compile: no mutating expressions in template bodies (defaults applied via plain-Go helpers `sparkWInt`/`sparkHInt`/`miniWInt`/`miniHInt`); `for` ranges in templ use `_, bar :=`, never `i, bar := ... _ = i`.
