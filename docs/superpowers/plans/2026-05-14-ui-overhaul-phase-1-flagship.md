# UI Overhaul · Phase 1 · Flagship — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Port `home.templ`, `repo.templ`, and `pull_detail.templ` to match `mockups/home.html`, `mockups/repo.html`, `mockups/pr.html`. Wire up the commit heatmap, attention list, language composition, and PR mergeability composite — every widget the mockups show must back to real data.

**Architecture:** Migration 054 adds a `commit_day_counts` aggregate table populated on push and via a background backfill. A new `RepoService.OnPostReceive(ctx, repoID int64, commits []model.Commit) error` method is added (it internally calls `CommitStatsService.Ingest`) and called from BOTH push paths — `internal/handler/git_http.go` (HTTP smart-protocol receive-pack, in `GitReceivePack`) and `internal/ssh/server.go` (SSH receive-pack, in the post-`execGitService` block) — so heatmap ingestion covers both transports. New `CommitStatsService`, `AttentionService`, `LanguageService`, plus a new `CodeService.Mergeability` method that reuses the existing package-level `resolveRef`, `findMergeBase`, `mergeTreesNoConflict`, and `checkFastForward` helpers in `code_service_merge.go`. New view components: `Heatmap`, `StatStrip`, `MergeabilityBox`, `LanguagesBar`, plus a new variant added to the existing `internal/view/components/timeline.templ` (which already exposes `Timeline` / `TimelineItem` / `TimelineIcon`). Pages port using the class translation map from Phase 0.

> **PR sub-routes alignment:** PR detail page chrome is rendered by Phase 5's separate `PullChrome` fragment, NOT `@fragments.RepoSubnav`. Repo overview page uses `@fragments.RepoSubnav(..., Active: "code")`. Phase 0's confirmed subnav tab keys are `code` / `issues` / `pull_requests` / `actions` / `discussions` / `projects` / `wiki` / `releases` / `settings`.

> **Schema dependencies dropped in Phase 1:** The original draft of this plan referenced a `pull_review_requests` table and an unread/read state on `mentions`. Neither exists today (`mentions` schema is migration 039 — no read state). To keep Phase 1 self-contained, `AttentionService` ships with only the issue-assigned path; the PR review-request path and the mention path are deferred to a later phase that adds the necessary schema. See Task 6 for the exact updated test cases.

**Tech Stack:** Go, sqlx, go-git, Templ.

**Prerequisites:** Phase 0 merged. `docs/ui-overhaul-class-map.md` exists.

**Spec:** [2026-05-14-ui-overhaul-design.md](../specs/2026-05-14-ui-overhaul-design.md)

**Branch:** `feat/ui-overhaul-phase-1-flagship`

---

### Task 1: Set up the branch

- [ ] **Step 1: Create the phase branch**

```bash
git checkout main
git pull
git checkout -b feat/ui-overhaul-phase-1-flagship
```

- [ ] **Step 2: Confirm Phase 0 artifacts are present**

```bash
test -f docs/ui-overhaul-class-map.md && echo "translation map: OK"
grep -q "border-strong" tailwind/input.css && echo "border-strong token: OK"
test -f internal/view/fragments/repo_subnav.templ && echo "repo subnav: OK"
```

Expected: three "OK" lines. If any are missing, the branch was cut from the wrong base.

---

### Task 2: Migration 054 — `commit_day_counts`

**Files:**
- Create: `internal/db/migrations/054_create_commit_day_counts.sql`

- [ ] **Step 1: Write the migration**

```sql
-- 054_create_commit_day_counts.sql
-- Per-(repo, user, day) commit-count aggregate. Backs the home and profile
-- contribution heatmaps. Populated on each push (via RepoService.PostReceive
-- hook in 056) and via a background backfill on first server start after this
-- migration runs.

CREATE TABLE IF NOT EXISTS commit_day_counts (
    id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    repo_id BIGINT NOT NULL REFERENCES repos(id) ON DELETE CASCADE,
    user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    day DATE NOT NULL,
    commit_count INT NOT NULL DEFAULT 0,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (repo_id, user_id, day)
);

CREATE INDEX IF NOT EXISTS idx_commit_day_counts_user_day
    ON commit_day_counts (user_id, day DESC);

CREATE INDEX IF NOT EXISTS idx_commit_day_counts_repo_day
    ON commit_day_counts (repo_id, day DESC);
```

- [ ] **Step 2: Run the migration**

```bash
make migrate
```

Expected: "applied migration 054_create_commit_day_counts.sql".

- [ ] **Step 3: Verify in psql**

```bash
psql "$CZ_DATABASE_DSN" -c '\d commit_day_counts'
```

Expected: shows the table with the columns above.

- [ ] **Step 4: Commit**

```bash
git add internal/db/migrations/054_create_commit_day_counts.sql
git commit -m "feat(db): migration 054 — commit_day_counts for contribution heatmap"
```

---

### Task 3: `CommitStatsStore` — TDD

**Files:**
- Create: `internal/store/commit_stats_store.go`
- Create: `internal/store/commit_stats_store_test.go`

- [ ] **Step 1: Write the failing test**

```go
// internal/store/commit_stats_store_test.go
package store

import (
    "context"
    "testing"
    "time"
)

func TestCommitStatsStore_UpsertAndListForUser(t *testing.T) {
    db := testDB(t) // existing test helper used by other stores
    s := NewCommitStatsStore(db)
    ctx := context.Background()

    user := seedUser(t, db, "alice")
    repo := seedRepo(t, db, user.ID, "demo")

    today := time.Now().UTC().Truncate(24 * time.Hour)
    yesterday := today.AddDate(0, 0, -1)

    if err := s.UpsertCount(ctx, repo.ID, user.ID, today, 3); err != nil {
        t.Fatalf("upsert: %v", err)
    }
    if err := s.UpsertCount(ctx, repo.ID, user.ID, yesterday, 5); err != nil {
        t.Fatalf("upsert: %v", err)
    }
    // upsert collision: same key, new count replaces (not adds)
    if err := s.UpsertCount(ctx, repo.ID, user.ID, today, 4); err != nil {
        t.Fatalf("upsert collide: %v", err)
    }

    rows, err := s.ListForUserSince(ctx, user.ID, yesterday)
    if err != nil {
        t.Fatalf("list: %v", err)
    }
    if len(rows) != 2 {
        t.Fatalf("expected 2 rows, got %d", len(rows))
    }
    var found4 bool
    for _, r := range rows {
        if r.Day.Equal(today) && r.CommitCount == 4 {
            found4 = true
        }
    }
    if !found4 {
        t.Errorf("expected today's count to be the upserted value (4), got %+v", rows)
    }
}
```

(The helpers `testDB`, `seedUser`, `seedRepo` should already exist — check `internal/store/store_test.go` or similar; if they don't, use whatever pattern the existing stores' tests use.)

- [ ] **Step 2: Run, verify FAIL**

```bash
go test ./internal/store/ -run TestCommitStatsStore -v
```

Expected: FAIL with "undefined: NewCommitStatsStore".

- [ ] **Step 3: Implement the store**

```go
// internal/store/commit_stats_store.go
package store

import (
    "context"
    "time"

    "github.com/jmoiron/sqlx"
)

type CommitDayCount struct {
    RepoID      int64     `db:"repo_id"`
    UserID      int64     `db:"user_id"`
    Day         time.Time `db:"day"`
    CommitCount int       `db:"commit_count"`
}

type CommitStatsStore struct {
    db *sqlx.DB
}

func NewCommitStatsStore(db *sqlx.DB) *CommitStatsStore {
    return &CommitStatsStore{db: db}
}

// UpsertCount replaces (does not add to) the commit count for the
// (repo, user, day) tuple. Push handlers compute the per-day commit count
// per pushed ref and call this; this is idempotent for the same push being
// retried.
func (s *CommitStatsStore) UpsertCount(ctx context.Context, repoID, userID int64, day time.Time, count int) error {
    const q = `
        INSERT INTO commit_day_counts (repo_id, user_id, day, commit_count, updated_at)
        VALUES ($1, $2, $3, $4, NOW())
        ON CONFLICT (repo_id, user_id, day)
        DO UPDATE SET commit_count = EXCLUDED.commit_count, updated_at = NOW()
    `
    _, err := s.db.ExecContext(ctx, q, repoID, userID, day.UTC().Truncate(24*time.Hour), count)
    return err
}

// ListForUserSince returns the user's per-day commit count across all
// repos they have commits in, since the given day (inclusive). Used by
// the home page contribution heatmap.
func (s *CommitStatsStore) ListForUserSince(ctx context.Context, userID int64, since time.Time) ([]CommitDayCount, error) {
    const q = `
        SELECT repo_id, user_id, day, commit_count
        FROM commit_day_counts
        WHERE user_id = $1 AND day >= $2
        ORDER BY day ASC
    `
    var out []CommitDayCount
    if err := s.db.SelectContext(ctx, &out, q, userID, since.UTC().Truncate(24*time.Hour)); err != nil {
        return nil, err
    }
    return out, nil
}

// ListForRepoSince returns per-day commit counts (summed across users)
// for one repo since the given day. Used by the repo About sidebar.
func (s *CommitStatsStore) ListForRepoSince(ctx context.Context, repoID int64, since time.Time) ([]CommitDayCount, error) {
    const q = `
        SELECT repo_id, 0::bigint AS user_id, day, SUM(commit_count)::int AS commit_count
        FROM commit_day_counts
        WHERE repo_id = $1 AND day >= $2
        GROUP BY day
        ORDER BY day ASC
    `
    var out []CommitDayCount
    if err := s.db.SelectContext(ctx, &out, q, repoID, since.UTC().Truncate(24*time.Hour)); err != nil {
        return nil, err
    }
    return out, nil
}
```

- [ ] **Step 4: Run, verify PASS**

```bash
go test ./internal/store/ -run TestCommitStatsStore -v
```

Expected: PASS.

- [ ] **Step 5: Wire into Stores struct**

In `internal/store/stores.go`, add a field to the `Stores` struct:

```go
CommitStats *CommitStatsStore
```

In the `New` function (or wherever stores are wired), add:

```go
stores.CommitStats = NewCommitStatsStore(db)
```

- [ ] **Step 6: Build**

```bash
go build ./...
```

Expected: clean build.

- [ ] **Step 7: Commit**

```bash
git add internal/store/commit_stats_store.go internal/store/commit_stats_store_test.go internal/store/stores.go
git commit -m "feat(store): add CommitStatsStore for per-day commit aggregates"
```

---

### Task 4: `CommitStatsService` with `OnPostReceive` hook — TDD

**Files:**
- Create: `internal/service/commit_stats_service.go`
- Create: `internal/service/commit_stats_service_test.go`
- Modify: `internal/service/repo_service.go` (add new `OnPostReceive` method)
- Modify: `internal/service/services.go`
- Modify: `internal/handler/git_http.go` (call `OnPostReceive` after webhook dispatch in `GitReceivePack`)
- Modify: `internal/ssh/server.go` (call `OnPostReceive` after webhook dispatch in the post-`execGitService` block)

- [ ] **Step 1: Write the failing service test**

```go
// internal/service/commit_stats_service_test.go
package service

import (
    "context"
    "testing"
    "time"
)

func TestCommitStatsService_Ingest_AggregatesPerDay(t *testing.T) {
    stores, _ := testStores(t) // existing helper
    svc := NewCommitStatsService(stores.CommitStats, stores.User)
    ctx := context.Background()

    user := seedUserSvc(t, stores, "bob")
    repo := seedRepoSvc(t, stores, user.ID, "demo")

    now := time.Now().UTC()
    samples := []CommitSample{
        {AuthorEmail: user.Email, Time: now.AddDate(0, 0, -2)},
        {AuthorEmail: user.Email, Time: now.AddDate(0, 0, -2).Add(2 * time.Hour)},
        {AuthorEmail: user.Email, Time: now.AddDate(0, 0, -1)},
    }
    if err := svc.Ingest(ctx, repo.ID, samples); err != nil {
        t.Fatalf("ingest: %v", err)
    }

    rows, err := stores.CommitStats.ListForUserSince(ctx, user.ID, now.AddDate(0, 0, -3))
    if err != nil {
        t.Fatalf("list: %v", err)
    }
    if len(rows) != 2 {
        t.Fatalf("expected 2 day rows, got %d", len(rows))
    }
    // Day -2 should have count 2, day -1 should have count 1.
    var d2, d1 int
    for _, r := range rows {
        switch {
        case r.Day.Equal(now.AddDate(0, 0, -2).Truncate(24 * time.Hour)):
            d2 = r.CommitCount
        case r.Day.Equal(now.AddDate(0, 0, -1).Truncate(24 * time.Hour)):
            d1 = r.CommitCount
        }
    }
    if d2 != 2 || d1 != 1 {
        t.Errorf("day -2=%d, day -1=%d", d2, d1)
    }
}
```

- [ ] **Step 2: Run, verify FAIL**

```bash
go test ./internal/service/ -run TestCommitStatsService_Ingest -v
```

Expected: FAIL.

- [ ] **Step 3: Implement service**

```go
// internal/service/commit_stats_service.go
package service

import (
    "context"
    "time"

    "github.com/mkappworks-dev/cloudzilla-app/internal/store"
)

// CommitSample is one commit's contribution data for stats ingestion.
type CommitSample struct {
    AuthorEmail string
    Time        time.Time
}

type CommitStatsService struct {
    stats *store.CommitStatsStore
    users *store.UserStore
}

func NewCommitStatsService(stats *store.CommitStatsStore, users *store.UserStore) *CommitStatsService {
    return &CommitStatsService{stats: stats, users: users}
}

// Ingest aggregates the given commit samples by (matched user, day) and
// upserts each bucket. Samples whose author email doesn't match any
// known user are silently skipped (anonymous commits do not count toward
// any user's heatmap).
func (s *CommitStatsService) Ingest(ctx context.Context, repoID int64, samples []CommitSample) error {
    type key struct {
        userID int64
        day    time.Time
    }
    buckets := make(map[key]int)
    cache := make(map[string]int64) // email → userID

    for _, c := range samples {
        userID, ok := cache[c.AuthorEmail]
        if !ok {
            u, err := s.users.GetByEmail(ctx, c.AuthorEmail)
            if err != nil {
                // Treat lookup error as "no matching user" — do not break
                // the whole ingest for one bad email.
                cache[c.AuthorEmail] = 0
                continue
            }
            if u == nil {
                cache[c.AuthorEmail] = 0
                continue
            }
            userID = u.ID
            cache[c.AuthorEmail] = userID
        }
        if userID == 0 {
            continue
        }
        day := c.Time.UTC().Truncate(24 * time.Hour)
        buckets[key{userID, day}]++
    }

    for k, count := range buckets {
        if err := s.stats.UpsertCount(ctx, repoID, k.userID, k.day, count); err != nil {
            return err
        }
    }
    return nil
}

// LookbackForUser returns the per-day commit counts for the user over
// the past `days` days. Returns the full window — days with zero
// commits get an explicit zero entry — so the heatmap can render a
// regular grid.
func (s *CommitStatsService) LookbackForUser(ctx context.Context, userID int64, days int) (map[time.Time]int, error) {
    since := time.Now().UTC().Truncate(24 * time.Hour).AddDate(0, 0, -days+1)
    rows, err := s.stats.ListForUserSince(ctx, userID, since)
    if err != nil {
        return nil, err
    }
    out := make(map[time.Time]int, days)
    for i := 0; i < days; i++ {
        out[since.AddDate(0, 0, i)] = 0
    }
    for _, r := range rows {
        out[r.Day.UTC().Truncate(24*time.Hour)] = r.CommitCount
    }
    return out, nil
}
```

- [ ] **Step 4: Run, verify PASS**

```bash
go test ./internal/service/ -run TestCommitStatsService_Ingest -v
```

Expected: PASS. (`UserStore.GetByEmail` already exists at `internal/store/user_store.go:68` — no need to add it.)

- [ ] **Step 5: Wire into Services struct**

In `internal/service/services.go`, add to the `Services` struct:

```go
CommitStats *CommitStatsService
```

In `New(...)`, after `userSvc` is created:

```go
commitStatsSvc := NewCommitStatsService(stores.CommitStats, stores.User)
```

And set the field on the returned struct: `CommitStats: commitStatsSvc`.

- [ ] **Step 6: Add `RepoService.OnPostReceive` and call it from both push paths**

There is NO existing post-receive hook on `RepoService` (verified via `grep -n "PostReceive" internal/service/repo_service.go` returns nothing). Add a new method and wire `*CommitStatsService` into `RepoService`.

6a. Inject `*CommitStatsService` into `RepoService`. Update the `RepoService` struct, `NewRepoService` signature, and the call site in `internal/service/services.go` so `commitStatsSvc` is passed in (construct `commitStatsSvc` first, then `repoSvc`).

6b. Append to `internal/service/repo_service.go`:

```go
import "github.com/go-git/go-git/v5/plumbing/object"

// OnPostReceive is called by the HTTP and SSH git-receive-pack handlers after
// a successful push. It aggregates commit-day counts into the heatmap. It is
// best-effort: errors are logged and swallowed so a failed aggregation does
// NOT roll back the push.
func (s *RepoService) OnPostReceive(ctx context.Context, repoID int64, commits []*object.Commit) error {
    samples := make([]CommitSample, 0, len(commits))
    for _, c := range commits {
        if c == nil {
            continue
        }
        samples = append(samples, CommitSample{
            AuthorEmail: c.Author.Email,
            Time:        c.Author.When,
        })
    }
    if err := s.commitStats.Ingest(ctx, repoID, samples); err != nil {
        slog.Warn("commit stats ingest failed", "err", err, "repo_id", repoID)
        return err
    }
    return nil
}
```

Use `slog` per the existing logging style in the file (verify with `grep slog internal/service/repo_service.go`).

6c. **HTTP path** — in `internal/handler/git_http.go`, inside `GitReceivePack` after the existing webhook-dispatch loop at lines ~342–354 (and before the index/dependency goroutines), add a goroutine that collects the new commits from each updated ref via `gitRepo.Log({From: cmd.New})` walking back until it reaches `cmd.Old`, accumulates them into a single slice, then calls `h.Services.Repo.OnPostReceive(ctx, repo.ID, commits)`. Run in a goroutine — push response must not block on aggregation.

```go
go func() {
    var commits []*object.Commit
    for _, cmd := range req.Commands {
        if !strings.HasPrefix(cmd.Name.String(), "refs/heads/") {
            continue
        }
        if cmd.Action() == packp.Delete {
            continue
        }
        iter, err := gitRepo.Log(&gogit.LogOptions{From: cmd.New})
        if err != nil {
            continue
        }
        _ = iter.ForEach(func(c *object.Commit) error {
            if c.Hash == cmd.Old {
                return storer.ErrStop
            }
            commits = append(commits, c)
            return nil
        })
        iter.Close()
    }
    _ = h.Services.Repo.OnPostReceive(context.Background(), repo.ID, commits)
}()
```

6d. **SSH path** — in `internal/ssh/server.go`, after the webhook dispatch loop (around lines ~258–268) and before `session.Exit(0)`, add the same collection-and-call pattern using `s.services.Repo.OnPostReceive`.

6e. Build:

```bash
go build ./...
```

Expected: clean build. Imports needed: `github.com/go-git/go-git/v5/plumbing/object`, `github.com/go-git/go-git/v5/plumbing/storer`, plus the existing `gogit` and `packp` aliases already in those files.

- [ ] **Step 7: Build and run all tests**

```bash
go build ./... && go test ./...
```

Expected: PASS.

- [ ] **Step 8: Commit**

```bash
git add internal/service/commit_stats_service.go internal/service/commit_stats_service_test.go internal/service/services.go internal/service/repo_service.go internal/handler/git_http.go internal/ssh/server.go
git commit -m "feat(service): add CommitStatsService and wire OnPostReceive in HTTP+SSH push paths"
```

---

### Task 5: Background backfill on server start

So existing repos populate the heatmap without waiting for new pushes.

**Files:**
- Modify: `cmd/server/main.go` (or wherever the server bootstraps)
- Create: `internal/service/commit_stats_backfill.go`

- [ ] **Step 1: Write the backfill helper**

```go
// internal/service/commit_stats_backfill.go
package service

import (
    "context"
    "time"

    "github.com/mkappworks-dev/cloudzilla-app/internal/model"
)

// BackfillRecentCommits walks each repo's default branch back `days` days,
// re-ingests every commit into commit_day_counts, and returns. Designed
// to run once on server start in a goroutine; safe to re-run because
// UpsertCount is idempotent for the same (repo, user, day, count).
func (s *CommitStatsService) BackfillRecentCommits(ctx context.Context, repos []model.Repository, code *CodeService, days int) error {
    cutoff := time.Now().UTC().AddDate(0, 0, -days)
    for _, r := range repos {
        commits, err := code.LogSince(ctx, r.OwnerName, r.Name, "", cutoff)
        if err != nil {
            continue
        }
        samples := make([]CommitSample, 0, len(commits))
        for _, c := range commits {
            samples = append(samples, CommitSample{AuthorEmail: c.AuthorEmail, Time: c.Time})
        }
        if err := s.Ingest(ctx, r.ID, samples); err != nil {
            // Log via the service's logger if available; otherwise swallow.
            _ = err
        }
    }
    return nil
}
```

- [ ] **Step 1b: Add `CodeService.LogSince` (it does not exist)**

Verified absent — `grep -n "LogSince" internal/service/code_service*.go` returns nothing. Add this method to `internal/service/code_service.go` (or a new `code_service_log.go`):

```go
// LogSinceCommit is one commit from LogSince, flattened so it does not
// require importing go-git into the caller.
type LogSinceCommit struct {
    SHA         string
    AuthorEmail string
    AuthorName  string
    Time        time.Time
}

// LogSince returns every commit reachable from `ref` whose author time is
// at or after `cutoff`. If `ref` is empty, the repository's default branch
// HEAD is used. Used by the commit-stats backfill on server start.
func (s *CodeService) LogSince(ctx context.Context, owner, repoName, ref string, cutoff time.Time) ([]LogSinceCommit, error) {
    repo, err := gogit.PlainOpen(s.repoPath(owner, repoName))
    if err != nil {
        return nil, err
    }
    if ref == "" {
        ref = "HEAD"
    }
    head, _, err := resolveRef(repo, ref)
    if err != nil {
        return nil, err
    }
    iter, err := repo.Log(&gogit.LogOptions{From: head.Hash})
    if err != nil {
        return nil, err
    }
    defer iter.Close()
    var out []LogSinceCommit
    err = iter.ForEach(func(c *object.Commit) error {
        if c.Author.When.Before(cutoff) {
            return storer.ErrStop
        }
        out = append(out, LogSinceCommit{
            SHA: c.Hash.String(),
            AuthorEmail: c.Author.Email,
            AuthorName: c.Author.Name,
            Time: c.Author.When,
        })
        return nil
    })
    return out, err
}
```

Also write a quick test in `internal/service/code_service_log_test.go` that seeds a repo with 3 commits and asserts `LogSince` cuts at the right boundary.

- [ ] **Step 1c: Add `RepoStore.ListAll` (it does not exist)**

Verified absent — `grep -n "ListAll" internal/store/repo_store.go` returns nothing. Add to `internal/store/repo_store.go`:

```go
// ListAll returns every non-soft-deleted repo. Used by background jobs
// (e.g. commit-stats backfill).
func (s *RepoStore) ListAll(ctx context.Context) ([]model.Repository, error) {
    const q = `SELECT * FROM repos WHERE deleted_at IS NULL ORDER BY id ASC`
    rows := []model.Repository{}
    if err := sqlx.SelectContext(ctx, sqlx.NewDb(s.db, "postgres"), &rows, q); err != nil {
        return nil, err
    }
    return rows, nil
}
```

(`RepoStore.db` is `*sql.DB`, not `*sqlx.DB`, per the existing struct — match the pattern used by other `RepoStore` methods rather than the snippet above, which is illustrative. Check how `RepoStore.List` does it and copy that style.)

- [ ] **Step 2: Trigger the backfill on server start**

In `cmd/server/main.go`, after services are constructed and before `http.ListenAndServe`, add:

```go
go func() {
    ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
    defer cancel()
    repos, err := stores.Repo.ListAll(ctx)
    if err != nil {
        log.Warn("backfill: list repos failed", "err", err)
        return
    }
    log.Info("starting commit-stats backfill", "repos", len(repos))
    if err := services.CommitStats.BackfillRecentCommits(ctx, repos, services.Code, 365); err != nil {
        log.Warn("backfill failed", "err", err)
    }
    log.Info("commit-stats backfill complete")
}()
```

(`RepoStore.ListAll` was added in Step 1c above.)

Also update the backfill helper signature to use `LogSinceCommit` (not `model.Commit`):

```go
func (s *CommitStatsService) BackfillRecentCommits(ctx context.Context, repos []model.Repository, code *CodeService, days int) error {
    cutoff := time.Now().UTC().AddDate(0, 0, -days)
    for _, r := range repos {
        commits, err := code.LogSince(ctx, r.OwnerName, r.Name, "", cutoff)
        if err != nil {
            continue
        }
        samples := make([]CommitSample, 0, len(commits))
        for _, c := range commits {
            samples = append(samples, CommitSample{AuthorEmail: c.AuthorEmail, Time: c.Time})
        }
        _ = s.Ingest(ctx, r.ID, samples)
    }
    return nil
}
```

- [ ] **Step 3: Build and start the server**

```bash
go build ./... && make dev
```

Watch the log. Expected: see "starting commit-stats backfill" then "commit-stats backfill complete" within a few seconds (or minutes for large repos).

- [ ] **Step 4: Verify rows in DB**

```bash
psql "$CZ_DATABASE_DSN" -c 'SELECT COUNT(*) FROM commit_day_counts;'
```

Expected: count > 0 if any seeded repo has commits.

- [ ] **Step 5: Commit**

```bash
git add internal/service/commit_stats_backfill.go cmd/server/main.go internal/store/repo_store.go
git commit -m "feat(service): backfill commit-day counts on server start"
```

---

### Task 6: `AttentionService` — TDD

Composite of (issues assigned to user). **Phase 1 scope is intentionally limited:** the PR review-request path requires a `pull_review_requests` table that does NOT exist in the current schema (verified — no migration creates it), and the mention path requires an unread/read state on `mentions` that also does not exist (migration 039 has no read column). Both are deferred. The component contract for `AttentionItem` accommodates the future kinds so the UI does not need a rewrite when Phase 8+ adds the schema.

**Files:**
- Create: `internal/service/attention_service.go`
- Create: `internal/service/attention_service_test.go`
- Modify: `internal/store/issue_store.go` (add `ListOpenAssignedToUser` — verified absent)
- Modify: `internal/service/services.go`

- [ ] **Step 1: Write the failing test**

```go
// internal/service/attention_service_test.go
package service

import (
    "context"
    "testing"
)

func TestAttentionService_ForUser(t *testing.T) {
    stores, _ := testStores(t)
    svc := NewAttentionService(stores.Issue)
    ctx := context.Background()

    alice := seedUserSvc(t, stores, "alice")
    bob := seedUserSvc(t, stores, "bob")
    repo := seedRepoSvc(t, stores, bob.ID, "demo")

    issue := seedIssue(t, stores, repo.ID, bob.ID, "needs alice")
    assignIssue(t, stores, issue.ID, alice.ID)

    // Self-assignment should be filtered out.
    ownIssue := seedIssue(t, stores, repo.ID, alice.ID, "alice's own work")
    assignIssue(t, stores, ownIssue.ID, alice.ID)

    items, err := svc.ForUser(ctx, alice.ID)
    if err != nil {
        t.Fatalf("ForUser: %v", err)
    }
    if len(items) != 1 {
        t.Fatalf("expected exactly 1 attention item (self-assigned filtered), got %d: %+v", len(items), items)
    }
    if items[0].Kind != AttentionIssueAssigned || items[0].RefID != issue.ID {
        t.Errorf("wrong item: got %+v", items[0])
    }
}
```

- [ ] **Step 2: Run, verify FAIL**

```bash
go test ./internal/service/ -run TestAttentionService -v
```

Expected: FAIL with undefined identifiers.

- [ ] **Step 3: Implement service**

```go
// internal/service/attention_service.go
package service

import (
    "context"
    "time"

    "github.com/mkappworks-dev/cloudzilla-app/internal/store"
)

type AttentionKind string

const (
    AttentionIssueAssigned     AttentionKind = "issue_assigned"
    // Reserved for later phases — schema not present in Phase 1.
    AttentionPRReviewRequested AttentionKind = "pr_review_requested"
    AttentionMention           AttentionKind = "mention"
)

type AttentionItem struct {
    Kind      AttentionKind
    RefID     int64     // issue or PR ID
    RepoName  string    // "owner/name"
    Title     string
    Number    int       // issue/PR number for display
    UpdatedAt time.Time
}

type AttentionService struct {
    issues *store.IssueStore
}

func NewAttentionService(issues *store.IssueStore) *AttentionService {
    return &AttentionService{issues: issues}
}

// ForUser returns the attention list for the home page, capped at 20
// items, sorted by most recently updated. Phase 1 only surfaces "open
// issues assigned to the user, where the user is not the author." The
// review-request and mention paths are intentionally omitted pending
// schema work in a later phase.
func (s *AttentionService) ForUser(ctx context.Context, userID int64) ([]AttentionItem, error) {
    issues, err := s.issues.ListOpenAssignedToUser(ctx, userID)
    if err != nil {
        return nil, err
    }

    var out []AttentionItem
    for _, i := range issues {
        if i.AuthorID == userID {
            continue
        }
        out = append(out, AttentionItem{
            Kind: AttentionIssueAssigned, RefID: i.ID, RepoName: i.RepoFullName,
            Title: i.Title, Number: i.Number, UpdatedAt: i.UpdatedAt,
        })
    }
    // Sort by UpdatedAt desc, cap at 20.
    sortByUpdatedDesc(out)
    if len(out) > 20 {
        out = out[:20]
    }
    return out, nil
}

func sortByUpdatedDesc(items []AttentionItem) {
    // Use sort.Slice; kept inline rather than importing for a single sort
    // because the slice is bounded.
    for i := 1; i < len(items); i++ {
        for j := i; j > 0 && items[j-1].UpdatedAt.Before(items[j].UpdatedAt); j-- {
            items[j-1], items[j] = items[j], items[j-1]
        }
    }
}
```

- [ ] **Step 4: Add `IssueStore.ListOpenAssignedToUser` (verified absent)**

In `internal/store/issue_store.go`, add (`issue_assignees` table confirmed in migration 017):

```go
type IssueListItem struct {
    ID           int64     `db:"id"`
    Number       int       `db:"number"`
    Title        string    `db:"title"`
    AuthorID     int64     `db:"author_id"`
    RepoFullName string    `db:"repo_full_name"`
    UpdatedAt    time.Time `db:"updated_at"`
}

func (s *IssueStore) ListOpenAssignedToUser(ctx context.Context, userID int64) ([]IssueListItem, error) {
    const q = `
        SELECT i.id, i.number, i.title, i.author_id,
               u.username || '/' || r.name AS repo_full_name,
               i.updated_at
        FROM issues i
        JOIN issue_assignees a ON a.issue_id = i.id
        JOIN repos r ON r.id = i.repo_id
        JOIN users u ON u.id = r.owner_id
        WHERE a.user_id = $1 AND i.state = 'open' AND i.deleted_at IS NULL
        ORDER BY i.updated_at DESC
        LIMIT 50
    `
    var out []IssueListItem
    if err := s.db.SelectContext(ctx, &out, q, userID); err != nil {
        return nil, err
    }
    return out, nil
}
```

**Do not add `PullStore.ListOpenReviewRequestedFromUser` or `MentionStore.ListUnreadOpenForUser` in this phase.** Both require schema that does not exist yet (no `pull_review_requests` table; `mentions` has no read state). They are deferred.

The `issues.state` column in the live schema may be `state` (TEXT) — verify the column type and the soft-delete column name: `grep -n 'CREATE TABLE issues\|ALTER TABLE issues' internal/db/migrations/*.sql`. The migration 051 family added soft-delete; adjust `i.deleted_at IS NULL` accordingly if the column name differs (e.g. drop the clause if issues are not soft-deletable).

- [ ] **Step 5: Run, verify PASS**

```bash
go test ./internal/service/ -run TestAttentionService -v
```

Expected: PASS.

- [ ] **Step 6: Wire service into `Services` struct**

In `internal/service/services.go`:

```go
// Field
Attention *AttentionService
```

In `New(...)`:

```go
attentionSvc := NewAttentionService(stores.Issue)
```

- [ ] **Step 7: Commit**

```bash
git add internal/service/attention_service.go internal/service/attention_service_test.go internal/service/services.go internal/store/
git commit -m "feat(service): add AttentionService for home page attention list"
```

---

### Task 7: `LanguageService` — TDD

Extension-based scan over the repo tree, cached in-memory per (repo_id, head_sha).

**Files:**
- Create: `internal/service/language_service.go`
- Create: `internal/service/language_service_test.go`
- Modify: `internal/service/services.go`

- [ ] **Step 1: Failing test**

```go
// internal/service/language_service_test.go
package service

import (
    "context"
    "testing"
)

func TestLanguageService_Composition(t *testing.T) {
    code := newTestCodeService(t)
    // makeTestRepo creates the bare repo on disk under the CodeService root
    // and returns (owner, repoName) — match whatever helper the existing
    // code_service tests use (e.g. code_service_merge_test.go).
    owner, repoName := makeTestRepo(t, code, map[string]string{
        "main.go":         "package main\nfunc main(){}\n",
        "util.go":         "package main\nfunc helper(){}\n",
        "README.md":       "# demo",
        "static/index.js": "console.log('hi')\n",
    })

    svc := NewLanguageService(code)
    comp, err := svc.Composition(context.Background(), owner, repoName, "HEAD")
    if err != nil {
        t.Fatalf("Composition: %v", err)
    }
    if comp["Go"] == 0 {
        t.Errorf("expected Go > 0, got %+v", comp)
    }
    if _, ok := comp["JavaScript"]; !ok {
        t.Errorf("expected JavaScript present, got %+v", comp)
    }
    // README is markdown; should be excluded from "code" composition.
    if _, ok := comp["Markdown"]; ok {
        t.Errorf("Markdown should be excluded; got %+v", comp)
    }
}
```

- [ ] **Step 2: Verify FAIL**

```bash
go test ./internal/service/ -run TestLanguageService -v
```

- [ ] **Step 3: Implement**

```go
// internal/service/language_service.go
package service

import (
    "context"
    "path/filepath"
    "strings"
    "sync"
    "time"
)

// Extension → language. Markdown, configs, lockfiles, vendored deps
// excluded — composition is about *code*.
var extToLang = map[string]string{
    ".go":    "Go",
    ".js":    "JavaScript",
    ".jsx":   "JavaScript",
    ".ts":    "TypeScript",
    ".tsx":   "TypeScript",
    ".py":    "Python",
    ".rb":    "Ruby",
    ".rs":    "Rust",
    ".java":  "Java",
    ".kt":    "Kotlin",
    ".swift": "Swift",
    ".c":     "C",
    ".h":     "C",
    ".cpp":   "C++",
    ".hpp":   "C++",
    ".cs":    "C#",
    ".php":   "PHP",
    ".sh":    "Shell",
    ".html":  "HTML",
    ".css":   "CSS",
    ".scss":  "Sass",
    ".templ": "Templ",
    ".sql":   "SQL",
}

// excludedDirs are skipped during the scan.
var excludedDirs = map[string]bool{
    "node_modules": true,
    "vendor":       true,
    ".git":         true,
    "dist":         true,
    "build":        true,
    "target":       true,
}

type LanguageService struct {
    code  *CodeService
    cache sync.Map // key="repoID:sha" -> cacheEntry
}

type cacheEntry struct {
    comp     map[string]int64 // language → bytes
    cachedAt time.Time
}

const langCacheTTL = 10 * time.Minute

func NewLanguageService(code *CodeService) *LanguageService {
    return &LanguageService{code: code}
}

// Composition returns bytes-per-language for the repo at the given ref.
// Cached for 10 minutes per (owner/repo, ref).
func (s *LanguageService) Composition(ctx context.Context, owner, repoName, ref string) (map[string]int64, error) {
    key := owner + "/" + repoName + ":" + ref
    if v, ok := s.cache.Load(key); ok {
        e := v.(cacheEntry)
        if time.Since(e.cachedAt) < langCacheTTL {
            return e.comp, nil
        }
    }
    comp := make(map[string]int64)
    err := s.code.WalkTree(ctx, owner, repoName, ref, func(path string, size int64) error {
        for dir := filepath.Dir(path); dir != "." && dir != "/"; dir = filepath.Dir(dir) {
            base := filepath.Base(dir)
            if excludedDirs[base] {
                return nil
            }
        }
        ext := strings.ToLower(filepath.Ext(path))
        if lang, ok := extToLang[ext]; ok {
            comp[lang] += size
        }
        return nil
    })
    if err != nil {
        return nil, err
    }
    s.cache.Store(key, cacheEntry{comp: comp, cachedAt: time.Now()})
    return comp, nil
}

// Percentages returns the same composition normalized to integer
// percentages summing to ≤ 100 (rounding may drop). Sorted descending.
type LangPercent struct {
    Name    string
    Percent int
}

func (s *LanguageService) Percentages(ctx context.Context, owner, repoName, ref string) ([]LangPercent, error) {
    comp, err := s.Composition(ctx, owner, repoName, ref)
    if err != nil {
        return nil, err
    }
    var total int64
    for _, b := range comp {
        total += b
    }
    if total == 0 {
        return nil, nil
    }
    var out []LangPercent
    for name, b := range comp {
        pct := int(b * 100 / total)
        if pct > 0 {
            out = append(out, LangPercent{Name: name, Percent: pct})
        }
    }
    // Sort descending by Percent.
    for i := 1; i < len(out); i++ {
        for j := i; j > 0 && out[j-1].Percent < out[j].Percent; j-- {
            out[j-1], out[j] = out[j], out[j-1]
        }
    }
    return out, nil
}
```

- [ ] **Step 3b: Add `CodeService.WalkTree` (verified absent)**

`CodeService.WalkTree` does not exist (`grep -n WalkTree internal/service/code_service*.go` returns nothing). Add to `internal/service/code_service_tree.go`:

```go
// WalkTree walks the tree at `ref` and invokes fn for every blob with
// (path, size). Returning an error from fn aborts the walk. Used by
// LanguageService to compute per-extension byte totals.
func (s *CodeService) WalkTree(ctx context.Context, owner, repoName, ref string, fn func(path string, size int64) error) error {
    repo, err := gogit.PlainOpen(s.repoPath(owner, repoName))
    if err != nil {
        return err
    }
    commit, _, err := resolveRef(repo, ref)
    if err != nil {
        return err
    }
    tree, err := commit.Tree()
    if err != nil {
        return err
    }
    return tree.Files().ForEach(func(f *object.File) error {
        return fn(f.Name, f.Size)
    })
}
```

Then update the `LanguageService.Composition` test and implementation to call `s.code.WalkTree(ctx, owner, repoName, ref, ...)` instead of the placeholder `(repoPath, ref, ...)` signature shown above. The `Composition` signature becomes:

```go
func (s *LanguageService) Composition(ctx context.Context, owner, repoName, ref string) (map[string]int64, error)
```

Cache key: `owner + "/" + repoName + ":" + ref`.

Also write `internal/service/code_service_tree_walk_test.go` that seeds a small repo with three files of known sizes and asserts `WalkTree` reports each one.

- [ ] **Step 4: Run, verify PASS**

```bash
go test ./internal/service/ -run TestLanguageService -v
```

- [ ] **Step 5: Wire into services**

In `internal/service/services.go`:

```go
Language *LanguageService
// in New(...)
languageSvc := NewLanguageService(code)
```

- [ ] **Step 6: Commit**

```bash
git add internal/service/language_service.go internal/service/language_service_test.go internal/service/services.go internal/service/code_service.go
git commit -m "feat(service): add LanguageService for per-repo language composition"
```

---

### Task 8: `CodeService.Mergeability` — TDD

Composite of ahead/behind and conflicts. Reuses the existing merge helpers in `internal/service/code_service_merge.go` — DO NOT invent new helpers; verified existing helpers are `resolveRef` (package-level, returns `(*object.Commit, displayRef string, error)`), `findMergeBase(repo, a, b *object.Commit)`, `mergeTreesNoConflict(repo, mergeBase, base, head)`, `checkFastForward(repo, base, head)`.

**Files:**
- Create: `internal/service/code_service_mergeability.go` (keeps `code_service.go` focused; mirrors the per-feature split already used in `code_service_merge.go`, `code_service_blame.go`, etc.)
- Create: `internal/service/code_service_mergeability_test.go`

- [ ] **Step 1: Failing test**

```go
// internal/service/code_service_mergeability_test.go
package service

import (
    "context"
    "testing"
)

func TestMergeability_Clean(t *testing.T) {
    code := newTestCodeService(t)
    owner, repoName := makeTestRepoWithBranches(t, code, "main", "feature")
    addCleanCommit(t, code, owner, repoName, "feature")

    m, err := code.Mergeability(context.Background(), owner, repoName, "main", "feature")
    if err != nil {
        t.Fatalf("Mergeability: %v", err)
    }
    if m.HasConflicts {
        t.Errorf("expected no conflicts, got %+v", m)
    }
    if m.Ahead == 0 {
        t.Errorf("expected ahead > 0, got %+v", m)
    }
}

func TestMergeability_Conflicts(t *testing.T) {
    code := newTestCodeService(t)
    owner, repoName := makeTestRepoWithBranches(t, code, "main", "feature")
    addConflictingCommitsBothSides(t, code, owner, repoName)

    m, err := code.Mergeability(context.Background(), owner, repoName, "main", "feature")
    if err != nil {
        t.Fatalf("Mergeability: %v", err)
    }
    if !m.HasConflicts {
        t.Errorf("expected conflicts, got %+v", m)
    }
}
```

- [ ] **Step 2: Verify FAIL**

```bash
go test ./internal/service/ -run TestMergeability -v
```

- [ ] **Step 3: Implement using the existing merge helpers**

Create `internal/service/code_service_mergeability.go`. Use ONLY the helpers verified to exist in `code_service_merge.go`: `resolveRef` (package-level fn, returns commit + displayRef + error), `findMergeBase(repo, a, b)`, `mergeTreesNoConflict(repo, mb, base, head)`, plus `gogit.PlainOpen(s.repoPath(owner, name))` for opening the repo (this is the pattern the existing merge methods use — there is NO `s.openRepo` method).

```go
// internal/service/code_service_mergeability.go
package service

import (
    "context"
    "errors"

    gogit "github.com/go-git/go-git/v5"
    "github.com/go-git/go-git/v5/plumbing/object"
    "github.com/go-git/go-git/v5/plumbing/storer"
)

// Mergeability is the composite mergeability summary used by the PR detail
// page sidebar. It does NOT consider branch protection rules — only the
// raw git-level merge-base + tree-merge state. The PR handler combines
// this with required-checks counts (Phase-1 helpers added to
// CommitStatusService) and required-reviews counts (helpers added to
// PullReviewService) to render the full sidebar.
type Mergeability struct {
    BaseRef      string
    HeadRef      string
    Ahead        int // commits in head not in base (excludes merge base)
    Behind       int // commits in base not in head (excludes merge base)
    HasConflicts bool
    MergeBase    string // SHA, empty if no common ancestor
}

func (s *CodeService) Mergeability(ctx context.Context, owner, repoName, base, head string) (Mergeability, error) {
    repo, err := gogit.PlainOpen(s.repoPath(owner, repoName))
    if err != nil {
        return Mergeability{}, err
    }
    baseCommit, _, err := resolveRef(repo, base)
    if err != nil {
        return Mergeability{}, err
    }
    headCommit, _, err := resolveRef(repo, head)
    if err != nil {
        return Mergeability{}, err
    }
    mb, err := findMergeBase(repo, baseCommit, headCommit)
    if err != nil {
        // No common ancestor — treat as conflicted, 0 ahead/behind.
        if errors.Is(err, errNoMergeBase) || err.Error() == "no common ancestor" {
            return Mergeability{BaseRef: base, HeadRef: head, HasConflicts: true}, nil
        }
        return Mergeability{}, err
    }
    ahead, err := countCommitsBetween(repo, mb, headCommit)
    if err != nil {
        return Mergeability{}, err
    }
    behind, err := countCommitsBetween(repo, mb, baseCommit)
    if err != nil {
        return Mergeability{}, err
    }
    _, ok, err := mergeTreesNoConflict(repo, mb, baseCommit, headCommit)
    if err != nil {
        return Mergeability{}, err
    }
    return Mergeability{
        BaseRef:      base,
        HeadRef:      head,
        Ahead:        ahead,
        Behind:       behind,
        HasConflicts: !ok,
        MergeBase:    mb.Hash.String(),
    }, nil
}

// countCommitsBetween counts commits reachable from `to` but not from `from`
// (exclusive of `from`). Walks `to`'s history, stopping when it hits the
// `from` commit.
func countCommitsBetween(repo *gogit.Repository, from, to *object.Commit) (int, error) {
    if from.Hash == to.Hash {
        return 0, nil
    }
    iter, err := repo.Log(&gogit.LogOptions{From: to.Hash})
    if err != nil {
        return 0, err
    }
    defer iter.Close()
    n := 0
    err = iter.ForEach(func(c *object.Commit) error {
        if c.Hash == from.Hash {
            return storer.ErrStop
        }
        n++
        return nil
    })
    return n, err
}

var errNoMergeBase = errors.New("no common ancestor")
```

Note: `findMergeBase` in `code_service_merge.go` returns `errors.New("no common ancestor")` literally; the string comparison above handles that without changing the existing helper.

- [ ] **Step 4: Run, verify PASS**

```bash
go test ./internal/service/ -run TestMergeability -v
```

- [ ] **Step 5: Commit**

```bash
git add internal/service/code_service.go internal/service/code_service_mergeability_test.go
git commit -m "feat(service): add CodeService.Mergeability composite"
```

---

### Task 9: `components.Heatmap` — TDD

**Files:**
- Create: `internal/view/components/heatmap.templ`
- Create: `internal/view/components/heatmap_test.go`

- [ ] **Step 1: Failing test**

```go
// internal/view/components/heatmap_test.go
package components

import (
    "bytes"
    "context"
    "strings"
    "testing"
    "time"
)

func TestHeatmap_RendersWeeksAndCells(t *testing.T) {
    today := time.Date(2026, 5, 14, 0, 0, 0, 0, time.UTC)
    cells := map[time.Time]int{
        today:                          5,
        today.AddDate(0, 0, -1):        2,
        today.AddDate(0, 0, -7):        0,
        today.AddDate(0, -1, 0):        4,
    }
    var buf bytes.Buffer
    if err := Heatmap(cells, today, 52).Render(context.Background(), &buf); err != nil {
        t.Fatalf("render: %v", err)
    }
    out := buf.String()
    if !strings.Contains(out, "heatmap-cell-") {
        t.Errorf("expected heatmap-cell-* class, got: %s", out)
    }
    if !strings.Contains(out, `role="img"`) {
        t.Errorf("expected role=img for a11y, got: %s", out)
    }
}
```

- [ ] **Step 2: Verify FAIL**

```bash
go test ./internal/view/components/ -run TestHeatmap -v
```

- [ ] **Step 3: Implement**

```go
// internal/view/components/heatmap.templ
package components

import (
    "fmt"
    "time"
)

// Heatmap renders a 52-week (or `weeks`) contribution calendar.
// `counts` is a day -> commit-count map; missing days render as count 0.
// `today` is the rightmost day. Each cell gets a class
// "heatmap-cell-{0..4}" — defined in tailwind/input.css.
templ Heatmap(counts map[time.Time]int, today time.Time, weeks int) {
    <div role="img" aria-label={ heatmapAriaLabel(counts, weeks) } class="inline-grid grid-flow-col gap-[2px]" style={ fmt.Sprintf("grid-template-rows: repeat(7, 11px); grid-auto-columns: 11px;") }>
        for _, day := range heatmapDays(today, weeks) {
            <div class={ "rounded-[2px] " + heatmapCellClass(counts[day]) } title={ heatmapTitle(day, counts[day]) }></div>
        }
    </div>
}

// heatmapDays returns weeks*7 days ending at today (UTC midnight).
// Order: top-to-bottom = Sun..Sat, left-to-right = oldest..newest week.
func heatmapDays(today time.Time, weeks int) []time.Time {
    today = today.UTC().Truncate(24 * time.Hour)
    // Walk back to the Sunday of the (today minus weeks-1 weeks).
    start := today.AddDate(0, 0, -7*(weeks-1))
    for start.Weekday() != time.Sunday {
        start = start.AddDate(0, 0, -1)
    }
    total := 7 * weeks
    out := make([]time.Time, 0, total)
    for i := 0; i < total; i++ {
        out = append(out, start.AddDate(0, 0, i))
    }
    return out
}

func heatmapCellClass(n int) string {
    switch {
    case n == 0:
        return "heatmap-cell-0"
    case n < 3:
        return "heatmap-cell-1"
    case n < 6:
        return "heatmap-cell-2"
    case n < 10:
        return "heatmap-cell-3"
    default:
        return "heatmap-cell-4"
    }
}

func heatmapTitle(day time.Time, n int) string {
    return fmt.Sprintf("%s — %d commits", day.Format("Mon, Jan 2 2006"), n)
}

func heatmapAriaLabel(counts map[time.Time]int, weeks int) string {
    total := 0
    for _, n := range counts {
        total += n
    }
    return fmt.Sprintf("Contribution heatmap: %d commits in the past %d weeks", total, weeks)
}
```

- [ ] **Step 4: Regenerate and run tests**

```bash
~/go/bin/templ generate
go test ./internal/view/components/ -run TestHeatmap -v
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/view/components/heatmap.templ internal/view/components/heatmap_templ.go internal/view/components/heatmap_test.go
git commit -m "feat(ui): add Heatmap component"
```

---

### Task 10: Other new components for this phase

Create the remaining components used by home/repo/pr ports. Each follows the same TDD pattern as Heatmap; for brevity these are listed with a code skeleton each.

**Files:**
- Create: `internal/view/components/stat_strip.templ`
- Create: `internal/view/components/stat_strip_test.go`
- Create: `internal/view/components/mergeability_box.templ`
- Create: `internal/view/components/mergeability_box_test.go`
- Create: `internal/view/components/languages_bar.templ`
- Create: `internal/view/components/languages_bar_test.go`
- Create: `internal/view/components/timeline_entry.templ` (or extend existing `timeline.templ`)
- Create: `internal/view/components/timeline_entry_test.go`

- [ ] **Step 1: StatStrip — test + implement**

Test asserts that a `StatStrip([]StatItem{{"Repositories",12,""},{"Pull requests",3,"open"}})` renders both label/value pairs.

Component:

```go
// internal/view/components/stat_strip.templ
package components

import "strconv"

type StatItem struct {
    Label    string
    Value    int
    Subtitle string
}

templ StatStrip(items []StatItem) {
    <dl class="grid grid-cols-2 md:grid-cols-4 gap-3">
        for _, it := range items {
            <div class="rounded-md border border-border bg-card p-3">
                <dt class="text-[11px] font-mono text-muted-foreground uppercase tracking-wider">{ it.Label }</dt>
                <dd class="text-2xl font-semibold tracking-tight mt-1">{ strconv.Itoa(it.Value) }</dd>
                if it.Subtitle != "" {
                    <dd class="text-xs text-muted-foreground/70">{ it.Subtitle }</dd>
                }
            </div>
        }
    </dl>
}
```

- [ ] **Step 2: MergeabilityBox — test + implement**

```go
// internal/view/components/mergeability_box.templ
package components

type MergeabilityBoxData struct {
    // PatchURL is the endpoint that the three merge buttons hx-patch against —
    // built by the PR handler as fmt.Sprintf("/api/repos/%s/%s/pulls/%d", owner, repo, number).
    PatchURL         string
    Mergeable        bool
    Ahead            int
    Behind           int
    HasConflicts     bool
    RequiredChecks   int
    PassingChecks    int
    RequiredReviews  int
    ApprovedReviews  int
    CanFastForward   bool
    CanThreeWayMerge bool
    CanSquash        bool
}

templ MergeabilityBox(d MergeabilityBoxData) {
    <section aria-label="Mergeability" class="rounded-md border border-border bg-card p-4 space-y-3">
        if d.HasConflicts {
            <p class="text-sm font-medium text-destructive">This branch has conflicts that must be resolved.</p>
        } else if d.Mergeable {
            <p class="text-sm font-medium text-success">This branch has no conflicts with the base branch.</p>
        } else {
            <p class="text-sm font-medium text-warning">Checking mergeability…</p>
        }
        <ul class="text-xs text-muted-foreground space-y-1">
            <li>{ aheadBehindDescription(d.Ahead, d.Behind) }</li>
            if d.RequiredChecks > 0 {
                <li>{ checksDescription(d.PassingChecks, d.RequiredChecks) }</li>
            }
            if d.RequiredReviews > 0 {
                <li>{ reviewsDescription(d.ApprovedReviews, d.RequiredReviews) }</li>
            }
        </ul>
        if !d.HasConflicts && d.Mergeable {
            <div class="flex flex-wrap gap-2 pt-2">
                if d.CanFastForward {
                    <button type="button"
                        hx-patch={ d.PatchURL }
                        hx-vals='{"state":"merged","merge_strategy":"ff"}'
                        hx-swap="outerHTML"
                        hx-target="closest section"
                        class="h-8 px-3 text-sm font-medium bg-primary text-primary-foreground rounded-md hover:bg-primary/90">
                        Fast-forward
                    </button>
                }
                if d.CanThreeWayMerge {
                    <button type="button"
                        hx-patch={ d.PatchURL }
                        hx-vals='{"state":"merged","merge_strategy":"merge"}'
                        hx-swap="outerHTML"
                        hx-target="closest section"
                        class="h-8 px-3 text-sm font-medium bg-secondary text-secondary-foreground rounded-md hover:bg-accent">
                        Merge
                    </button>
                }
                if d.CanSquash {
                    <button type="button"
                        hx-patch={ d.PatchURL }
                        hx-vals='{"state":"merged","merge_strategy":"squash"}'
                        hx-swap="outerHTML"
                        hx-target="closest section"
                        class="h-8 px-3 text-sm font-medium bg-secondary text-secondary-foreground rounded-md hover:bg-accent">
                        Squash &amp; merge
                    </button>
                }
            </div>
        }
    </section>
}
```

Endpoint reference: `PATCH /api/repos/{owner}/{repo}/pulls/{number}` with body `state=merged&merge_strategy=ff|merge|squash` — see `docs/pr-merge.md`.

The three helper functions (aheadBehindDescription, etc.) live in `internal/view/components/helpers.go` or a new `mergeability_helpers.go`. Format: "3 commits ahead, 0 behind", "All 4 required checks passing", "1 of 2 required reviews approved".

- [ ] **Step 3: LanguagesBar — test + implement**

```go
// internal/view/components/languages_bar.templ
package components

import (
    "fmt"
    "strconv"
)

type LangBarItem struct {
    Name    string
    Percent int
    Color   string // hex; see langColor() lookup
}

templ LanguagesBar(items []LangBarItem) {
    <div aria-label="Language composition" role="img">
        <div class="flex h-2 rounded-full overflow-hidden border border-border">
            for _, it := range items {
                <div title={ fmt.Sprintf("%s — %d%%", it.Name, it.Percent) } style={ fmt.Sprintf("width: %d%%; background-color: %s;", it.Percent, it.Color) }></div>
            }
        </div>
        <ul class="flex flex-wrap gap-3 mt-2 text-xs text-muted-foreground">
            for _, it := range items {
                <li class="flex items-center gap-1.5">
                    <span class="inline-block w-2 h-2 rounded-full" style={ "background-color: " + it.Color } aria-hidden="true"></span>
                    <span class="text-foreground">{ it.Name }</span>
                    <span>{ strconv.Itoa(it.Percent) + "%" }</span>
                </li>
            }
        </ul>
    </div>
}
```

Add a `langColor(name string) string` helper with a small lookup (Go #00ADD8, JavaScript #F1E05A, TypeScript #3178C6, Python #3572A5, etc. — same palette GitHub uses).

- [ ] **Step 4: TimelineEntry — extend existing timeline**

Read `internal/view/components/timeline.templ` first — it already defines `Timeline(ariaLabel)`, `TimelineItem()`, and `TimelineIcon(variant TimelineVariant)`. There is NO `TimelineRoot`. We add ONE new component, `TimelineEntry`, on top of the existing primitives — it composes a `TimelineItem` + `TimelineIcon` for the common case (actor + verb + when + optional body) so call sites in `pull_detail.templ` don't have to spell the layout out per row.

Append to the existing `internal/view/components/timeline.templ`:

```go
// TimelineEntryKind maps an event kind to its icon variant + verb string.
type TimelineEntryKind string

const (
    TimelineEntryComment  TimelineEntryKind = "comment"
    TimelineEntryClosed   TimelineEntryKind = "closed"
    TimelineEntryReopened TimelineEntryKind = "reopened"
    TimelineEntryMerged   TimelineEntryKind = "merged"
    TimelineEntryLabeled  TimelineEntryKind = "labeled"
    TimelineEntryAssigned TimelineEntryKind = "assigned"
    TimelineEntryReviewed TimelineEntryKind = "reviewed"
)

// TimelineEntry renders one row inside a Timeline: actor name + action verb +
// timestamp on the first line, with optional rich `body` children below
// (e.g. a comment card).
templ TimelineEntry(kind TimelineEntryKind, actor string, when string) {
    @TimelineItem() {
        @TimelineIcon(timelineEntryVariant(kind)) {
            @timelineEntryIcon(kind)
        }
        <div class="flex-1 min-w-0">
            <p class="text-sm">
                <a href={ templ.SafeURL("/" + actor) } class="font-medium text-foreground hover:text-primary">{ actor }</a>
                <span class="text-muted-foreground">{ " " + timelineEntryVerb(kind) + " " }</span>
                <time class="text-muted-foreground text-xs">{ when }</time>
            </p>
            <div class="mt-1">
                { children... }
            </div>
        </div>
    }
}

func timelineEntryVariant(k TimelineEntryKind) TimelineVariant {
    switch k {
    case TimelineEntryClosed:
        return TimelineDestructive
    case TimelineEntryReopened, TimelineEntryReviewed:
        return TimelineSuccess
    case TimelineEntryMerged:
        return TimelineMerged
    default:
        return TimelineDefault
    }
}

func timelineEntryVerb(k TimelineEntryKind) string {
    switch k {
    case TimelineEntryComment:
        return "commented"
    case TimelineEntryClosed:
        return "closed this"
    case TimelineEntryReopened:
        return "reopened this"
    case TimelineEntryMerged:
        return "merged this"
    case TimelineEntryLabeled:
        return "added labels"
    case TimelineEntryAssigned:
        return "assigned"
    case TimelineEntryReviewed:
        return "reviewed"
    default:
        return ""
    }
}

// timelineEntryIcon emits the inner 12px SVG for the given kind. Keep
// the body short — one templ block per kind, calling out to per-kind
// fragments if it grows.
templ timelineEntryIcon(k TimelineEntryKind) {
    // intentionally empty; per-kind SVGs added in a follow-up commit. The
    // outer TimelineIcon variant carries the colour state today.
}
```

Markdown rendering for comment bodies uses the real package: `markdown.Render(src string) string` from `github.com/mkappworks-dev/cloudzilla-app/internal/markdown` — wrap with `templ.Raw` per the project convention.

- [ ] **Step 5: Run tests for all four**

```bash
~/go/bin/templ generate
go test ./internal/view/components/ -v -run 'TestStatStrip|TestMergeabilityBox|TestLanguagesBar|TestTimelineEntry'
```

Expected: all PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/view/components/
git commit -m "feat(ui): add StatStrip, MergeabilityBox, LanguagesBar, TimelineEntry components"
```

---

### Task 11: Port `home.templ` to match `mockups/home.html`

**Files:**
- Modify: `internal/view/pages/home.templ`
- Modify: `internal/handler/page_handler.go` (`PageHome`: REMOVE the existing `/feed` redirect — the home mockup IS the logged-in dashboard, so logged-in users should see the home page, not redirect)
- Modify: `internal/view/viewmodels.go` (`HomeData` — extend; keep the existing embedded `BasePage` field — see Phase 0)

- [ ] **Step 1: Inventory the mockup**

```bash
~/go/bin/templ fmt mockups/home.html >/dev/null 2>&1 || true
wc -l mockups/home.html
grep -oE 'class="[^"]*"' mockups/home.html | tr ' ' '\n' | sort -u | grep -vE '^class=|^"$' > /tmp/home_classes.txt
wc -l /tmp/home_classes.txt
```

Review `/tmp/home_classes.txt`. For each entry: if a class is in `docs/ui-overhaul-class-map.md`, OK. If a class is in `components/` (like `dropdown-item`), it's component-replaced. If neither — update the class map first (per Task 2's "Updating this map" section).

- [ ] **Step 2: Extend `HomeData` viewmodel**

`HomeData` lives in `internal/view/viewmodels.go` and is re-exported by `internal/handler/viewmodels.go` as a type alias. It currently embeds `BasePage` (NOT a named `BasePage` field). Keep the embed — call sites construct it with `view.HomeData{BasePage: basePage(...), Repos: ..., Templates: ...}` (see `PageHome` in `page_handler.go`). Add fields without renaming the embed:

```go
// internal/view/viewmodels.go
type HomeData struct {
    BasePage                                // embedded — DO NOT convert to a named field
    Repos      []model.Repository
    Templates  []model.Repository
    Stats      []components.StatItem        // Repositories / Pull requests / Issues / Commits last 7 days
    Heatmap    map[time.Time]int            // 52-week per-day commit counts (current user)
    Attention  []service.AttentionItem      // top 20 attention items (issue-assigned only in Phase 1)
    Activity   []model.Event                // recent activity events — reuses EventService.Feed
}
```

Import note: importing `components` and `service` into `internal/view/viewmodels.go` is allowed (no cycle today; verify with `go build ./...`). If a cycle does arise, move the new fields onto a separate view struct in `internal/handler/viewmodels.go` and pass it alongside `HomeData`.

- [ ] **Step 3: Rewrite `PageHome` to drop the redirect and populate the new fields**

`PageHome` currently redirects logged-in users to `/feed` (`internal/handler/page_handler.go:26-29`). Remove the redirect — the home mockup IS the logged-in dashboard. Logged-out users see the same page with empty dashboards (or the public marketing variant per the mockup; check mockup intent).

After removing the redirect, populate the new fields:

```go
func (h *Handler) PageHome(w http.ResponseWriter, r *http.Request) {
    ctx := r.Context()
    repos, _ := h.Services.Repo.List(ctx)
    if repos == nil { repos = []model.Repository{} }
    templates, _ := h.Services.Repo.ListTemplates(ctx)
    if templates == nil { templates = []model.Repository{} }

    data := view.HomeData{
        BasePage:  basePage(r, h.Services),
        Repos:     repos,
        Templates: templates,
    }

    if claims, ok := middleware.ClaimsFromContext(ctx); ok {
        userID := claims.UserID

        // Stat strip — matches mockups/home.html: Repositories / Pull requests / Issues / Commits last 7 days.
        commitsLast7, _ := h.Services.CommitStats.CommitsForUserSince(ctx, userID, 7)
        countRepos, _ := h.Stores.Repo.CountForUser(ctx, userID)
        countOpenPulls, _ := h.Stores.Pull.CountOpenAuthoredByOrAssignedTo(ctx, userID)
        countOpenIssues, _ := h.Stores.Issue.CountOpenAuthoredByOrAssignedTo(ctx, userID)
        data.Stats = []components.StatItem{
            {Label: "Repositories",   Value: countRepos},
            {Label: "Pull requests",  Value: countOpenPulls,  Subtitle: "open"},
            {Label: "Issues",         Value: countOpenIssues, Subtitle: "open"},
            {Label: "Commits",        Value: commitsLast7,    Subtitle: "last 7 days"},
        }

        if heat, err := h.Services.CommitStats.LookbackForUser(ctx, userID, 365); err == nil {
            data.Heatmap = heat
        }
        if att, err := h.Services.Attention.ForUser(ctx, userID); err == nil {
            data.Attention = att
        }
        // EventService.Feed exists; cap to 10 for the home card.
        if feed, err := h.Services.Event.Feed(ctx, int(userID), 1, 10); err == nil {
            data.Activity = feed
        }
    }

    h.render(w, r, pages.Home(data))
}
```

Add the helper methods that don't yet exist; each is a real sub-task with its own test:

- `CommitStatsService.CommitsForUserSince(ctx, userID, days int) (int, error)` — sums `commit_count` from `commit_day_counts` for that user since `now - days`. Add to `commit_stats_service.go` alongside `LookbackForUser`. Trivial test seeds two days + a stale row and asserts the sum.
- `RepoStore.CountForUser(ctx, userID) (int, error)` — `SELECT COUNT(*) FROM repos WHERE owner_id = $1 AND deleted_at IS NULL`. Verify the column name with `head internal/db/migrations/001_create_users.sql` and the repo migrations; adjust if needed.
- `PullStore.CountOpenAuthoredByOrAssignedTo(ctx, userID) (int, error)` — UNION-style query joining `pull_assignees`. (`PullStore.CountOpen` exists per-repo but not per-user.)
- `IssueStore.CountOpenAuthoredByOrAssignedTo(ctx, userID) (int, error)` — analogous to the pull version, joining `issue_assignees`.

(`EventService.Feed` already exists — `internal/service/event_service.go:47` — signature is `Feed(ctx, userID int64, page, pageSize int)`; the `int(userID)` cast above is wrong, pass `userID` directly. Match the file.)

- [ ] **Step 4: Rewrite `home.templ` body to match the mockup**

Read `mockups/home.html` lines 100–700 (the body content). Translate section by section into `internal/view/pages/home.templ`, replacing the existing body. Use:
- `components.StatStrip(data.Stats)` for the stat row — labels per mockup: Repositories / Pull requests / Issues / Commits (last 7 days). DO NOT add "Stars given" (that was a draft-plan slip; mockup never had it).
- A two-column grid (left: repos table + attention list; right: heatmap card + activity feed)
- The existing `components.Table` family for the repos table (already there; visuals match the mockup with class translation applied)
- A new attention-list section that loops `data.Attention` and renders one row per item with kind-specific icon, repo/full-name link, title link, time. Today the list only contains `AttentionIssueAssigned` items, so the icon switch can be a single-arm. Keep the kind switch in place so adding PR-review-requested + mention in a later phase requires no rewrite.
- `components.Heatmap(data.Heatmap, time.Now(), 52)` for the calendar
- A simple activity-feed list using `@Timeline(...)` + `@TimelineEntry(...)` over `data.Activity` (which is `[]model.Event`)

Keep the existing `repoIcon` helper and other home-page helpers. Add helpers `attentionIcon(kind)`, `eventIcon(eventType)`, `formatRelative(time.Time)` as needed.

- [ ] **Step 5: Regenerate and run**

```bash
~/go/bin/templ generate && make build-css && go build ./... && go test ./... && make dev
```

Open `http://localhost:8080/` while logged in. Compare against `mockups/home.html` open in another browser tab. In dev tools, run a colour-blind / forced-colors check.

- [ ] **Step 6: Toggle theme and re-verify**

Click the moon/sun. Sweep again. Confirm: dropdowns, focus rings, heatmap cells, attention list all render cleanly in both themes.

- [ ] **Step 7: Commit**

```bash
git add internal/view/pages/home.templ internal/view/pages/home_templ.go internal/handler/page_handler.go internal/handler/viewmodels.go internal/store/
git commit -m "feat(ui): port home page to match mockup (heatmap, attention, stats, activity)"
```

---

### Task 12: Port `repo.templ` to match `mockups/repo.html`

**Files:**
- Modify: `internal/view/pages/repo.templ`
- Modify: `internal/handler/page_handler.go` (`PageRepo` or `GetRepo` for the page render path)
- Modify: `internal/handler/viewmodels.go` (`RepoData`)

- [ ] **Step 1: Extend `RepoData` viewmodel**

```go
type RepoData struct {
    // ... existing fields ...
    Languages    []components.LangBarItem
    TopContribs  []service.ContributorStat // returned by CodeService.GetContributors / RepoService.TopContributors
    Releases     []model.Release       // most recent N
    Heatmap      map[time.Time]int     // per-repo, last 90 days
}
```

- [ ] **Step 2: Populate new fields in the repo page handler**

After the repo is loaded and access is authorized:

```go
if percents, err := h.Services.Language.Percentages(ctx, repo.OwnerName, repo.Name, repo.DefaultBranch); err == nil {
    data.Languages = make([]components.LangBarItem, 0, len(percents))
    for _, p := range percents {
        data.Languages = append(data.Languages, components.LangBarItem{
            Name:    p.Name,
            Percent: p.Percent,
            Color:   components.LangColor(p.Name),
        })
    }
}
if top, err := h.Services.Repo.TopContributors(ctx, repo.OwnerName, repo.Name, 10); err == nil {
    data.TopContribs = top
}
if rels, err := h.Services.Release.RecentForRepo(ctx, repo.OwnerName, repo.Name, 5); err == nil {
    data.Releases = rels
}
if heat, err := h.Services.CommitStats.LookbackForRepo(ctx, repo.ID, 90); err == nil {
    data.Heatmap = heat
}
```

Add `LangColor` to `internal/view/components/languages_bar.templ` (the helper from Task 10 step 3). Each of the following is a real sub-task — verified absent — with its own implementation + test:

- [ ] **Step 2a: `RepoService.TopContributors(ctx, owner, name, limit)` — verified absent**

The codebase has `CodeService.GetContributors(owner, repoName)` (returns `[]ContributorStat`) in `code_service_insights.go:47`. Wrap it on `RepoService` so the home/repo handler doesn't reach across into `CodeService` directly:

```go
func (s *RepoService) TopContributors(ctx context.Context, owner, name string, limit int) ([]ContributorStat, error) {
    all, err := s.code.GetContributors(owner, name)
    if err != nil { return nil, err }
    if limit > 0 && len(all) > limit { all = all[:limit] }
    return all, nil
}
```

Requires injecting `*CodeService` into `RepoService` (or accessing via the existing wiring). Test: seed a repo with three authors of varying commit counts, assert ordering and limit.

- [ ] **Step 2b: `ReleaseService.RecentForRepo(ctx, owner, name, limit)` — verified absent**

`ReleaseService.ListByRepo` exists at `release_service.go:68`. Add a thin wrapper that orders by `published_at DESC` (or `created_at DESC`) and applies a limit. Test: seed 6 releases, assert returns 5 in descending order.

- [ ] **Step 2c: `CommitStatsService.LookbackForRepo(ctx, repoID, days)` — verified absent**

Mirror `LookbackForUser`, but call `s.stats.ListForRepoSince` (added in Task 3) and return the same `map[time.Time]int`. Test: insert two per-day rows + a stale row outside the window, assert the window-filled map.

- [ ] **Step 3: Rewrite `repo.templ` body**

Read `mockups/repo.html`. Major sections, top-to-bottom:
- Sub-nav: `@fragments.RepoSubnav(repoSubnavProps{ Active: "code", ... })` — verified key per Phase 0 (`code` / `issues` / `pull_requests` / `actions` / `discussions` / `projects` / `wiki` / `releases` / `settings`)
- Action bar (Watch / Star / Fork buttons + clone box)
- Branch picker + file tree
- README rendered below the file tree
- About sidebar:
  - Description
  - Topics chips
  - Languages bar (`components.LanguagesBar`)
  - Recent releases list
  - Top contributors strip
  - Mini heatmap (`components.Heatmap` with 90 days)

Apply class translation map per row. Use existing `components.Table` for the file tree (existing pattern in `tree.templ`).

- [ ] **Step 4: Regenerate, build, run**

```bash
~/go/bin/templ generate && make build-css && go build ./... && make dev
```

Open `http://localhost:8080/<owner>/<repo>` and compare to `mockups/repo.html`. Both themes.

- [ ] **Step 5: Commit**

```bash
git add internal/view/pages/repo.templ internal/view/pages/repo_templ.go internal/handler/page_handler.go internal/handler/viewmodels.go internal/service/ internal/store/
git commit -m "feat(ui): port repo overview page to match mockup (languages bar, contributors, mini heatmap)"
```

---

### Task 13: Port `pull_detail.templ` to match `mockups/pr.html`

**Files:**
- Modify: `internal/view/pages/pull_detail.templ`
- Modify: `internal/handler/page_handler.go` (`GetPull` page render path)
- Modify: `internal/handler/viewmodels.go` (`PullDetailData`)

- [ ] **Step 1: Extend viewmodel**

```go
type PullDetailData struct {
    // ... existing fields ...
    Mergeability components.MergeabilityBoxData
    Timeline     []TimelineItemView  // pre-rendered timeline view items
}
```

`TimelineItemView` carries (kind, actor, time, body html.SafeString) and is mapped from the existing `Comment`/`Event`/`Review` rows.

- [ ] **Step 2: Populate mergeability**

`Pull` model fields are `HeadBranch` and `BaseBranch` (not `HeadRef`/`BaseRef`) — verified in `internal/model/pull.go`. To resolve the head SHA when the box needs it, call `h.Services.Code.ResolveRef(owner, repoName, pull.HeadBranch)`.

```go
mg, _ := h.Services.Code.Mergeability(ctx, repo.OwnerName, repo.Name, pull.BaseBranch, pull.HeadBranch)
requiredChecks, passingChecks := h.Services.CommitStatus.Counts(ctx, pull.ID)
requiredReviews, approvedReviews := h.Services.PullReview.Counts(ctx, pull.ID)
data.Mergeability = components.MergeabilityBoxData{
    PatchURL:         fmt.Sprintf("/api/repos/%s/%s/pulls/%d", repo.OwnerName, repo.Name, pull.Number),
    Mergeable:        !mg.HasConflicts && mg.Ahead > 0,
    Ahead:            mg.Ahead,
    Behind:           mg.Behind,
    HasConflicts:     mg.HasConflicts,
    RequiredChecks:   requiredChecks,
    PassingChecks:    passingChecks,
    RequiredReviews:  requiredReviews,
    ApprovedReviews:  approvedReviews,
    CanFastForward:   mg.Behind == 0 && !mg.HasConflicts,
    CanThreeWayMerge: !mg.HasConflicts,
    CanSquash:        !mg.HasConflicts,
}
```

- [ ] **Step 2a: Add `CommitStatusService.Counts(ctx, pullID) (required, passing int)`**

Verified absent. The service has `GetCombined(ctx, owner, repoName, sha)` but no required-checks count by PR. Add a method that:
1. Fetches the PR's head SHA: `headCommit, _, _ := s.code.ResolveRef(owner, repoName, pull.HeadBranch)` (or accept the SHA directly to keep the service stateless).
2. Loads required-check rules from `branch_protections` for the PR's base branch (use `BranchProtectionService` as it already has the lookup).
3. Counts how many of the required contexts have a `success` status at the head SHA.

Skeleton:

```go
// internal/service/commit_status_service.go
func (s *CommitStatusService) Counts(ctx context.Context, pullID int64) (required, passing int, _ error) {
    // Implementation: fetch PR → branch protection → required context list →
    // GetCombined for head SHA → count passing matches. Returns (0, 0, nil)
    // if no protection rule is configured (UI hides the line in that case).
    // ...
}
```

Test in `commit_status_service_test.go`: seed a PR + a protection requiring contexts `["ci", "lint"]` + statuses on head SHA where `ci=success, lint=failure`; expect `(2, 1)`.

- [ ] **Step 2b: Add `PullReviewService.Counts(ctx, pullID) (required, approved int)`**

Verified absent (the service has `CanMerge` but not a count). Add:

```go
// internal/service/pull_review_service.go
func (s *PullReviewService) Counts(ctx context.Context, pullID int64) (required, approved int, _ error) {
    // Implementation: required = branch_protections.required_approvals for PR's base branch
    // (0 if no protection); approved = COUNT(DISTINCT reviewer_id) FROM pull_reviews
    // WHERE pull_id = $1 AND state = 'approved'.
}
```

Test: seed protection requiring 2 approvals, two approve, one requests changes → `(2, 2)`.

- [ ] **Step 3: Build timeline items**

Merge comments + review events + status changes into one chronologically-sorted slice. Each item maps to a `TimelineEntry` kind. Comment bodies are rendered through `markdown.Render(src string) string` (from `internal/markdown`) and wrapped with `templ.Raw(...)` — the codebase has no `components.MarkdownBody`; rendered-comment view structs (`view.RenderedComment`) already do this pre-render in their constructor — reuse that pattern.

- [ ] **Step 4: Rewrite `pull_detail.templ`**

Major sections per `mockups/pr.html`:
- Header: title + state badge (Open/Draft/Merged/Closed) + sub-line (author opened on date — use `pull.CreatedAt`, the field is NOT `OpenedAt`).
- Sub-nav: Conversation / Commits / Checks / Files changed — rendered by Phase 5's `PullChrome` fragment, NOT `RepoSubnav`. Pass the active tab key (e.g. `"conversation"`) to that fragment.
- Two columns:
  - Left: `@Timeline("Pull request activity")` wrapping a series of `@TimelineEntry(kind, actor, when) { ...body... }` items + mergeability box (`components.MergeabilityBox`) + composer
  - Right sidebar: reviewers, assignees, labels, milestone, project, linked issues

Use the existing `view.RenderedComment` pattern for any rendered markdown in the timeline; render via `templ.Raw(rc.BodyHTML)`.

- [ ] **Step 5: Regenerate, build, run, verify**

```bash
~/go/bin/templ generate && make build-css && go build ./... && go test ./... && make dev
```

Open a real PR in dev: `/{owner}/{repo}/pulls/{n}`. Compare to `mockups/pr.html`. Both themes. Submit a comment to verify HTMX path still works.

- [ ] **Step 6: Commit**

```bash
git add internal/view/pages/pull_detail.templ internal/view/pages/pull_detail_templ.go internal/handler/page_handler.go internal/handler/viewmodels.go internal/service/
git commit -m "feat(ui): port PR detail page to match mockup (mergeability box, rich timeline)"
```

---

### Task 14: Verify phase exit criteria and open the PR

- [ ] **Step 1: Full test + lint**

```bash
go test ./... && make lint
```

Expected: PASS.

- [ ] **Step 2: Templ regen clean**

```bash
~/go/bin/templ generate
git status
```

Expected: clean diff.

- [ ] **Step 3: Visual sweep on all three pages**

In dev mode, walk through home → repo → PR detail in both themes. Compare each side-by-side with mockups/.

- [ ] **Step 4: Run silent-failure-hunter**

Per `CLAUDE.md`.

- [ ] **Step 5: Push and open PR**

```bash
git push -u origin feat/ui-overhaul-phase-1-flagship
gh pr create --title "feat(ui): UI overhaul phase 1 — flagship pages" --body "$(cat <<'EOF'
## Summary
- Ports `home.templ`, `repo.templ`, and `pull_detail.templ` to match the mockup designs.
- Migration 054: `commit_day_counts` aggregate table for the contribution heatmap.
- New services: `CommitStatsService`, `AttentionService`, `LanguageService`.
- Extends `CodeService` with `Mergeability` composite (ahead/behind/conflicts).
- New components: `Heatmap`, `StatStrip`, `MergeabilityBox`, `LanguagesBar`, `TimelineEntry`.
- Post-receive hook ingests per-commit data into the heatmap aggregate; first-start backfill catches up existing repos.

Spec: `docs/superpowers/specs/2026-05-14-ui-overhaul-design.md`
Plan: `docs/superpowers/plans/2026-05-14-ui-overhaul-phase-1-flagship.md`

## Test plan
- [x] `go test ./...` passes (incl. new service tests for heatmap/attention/languages/mergeability)
- [x] `make lint` passes
- [x] Migration 054 applied cleanly
- [x] Backfill runs and populates `commit_day_counts` on server start
- [x] Manual visual sweep: home / repo / pr in both themes
- [x] silent-failure-hunter agent run, no high-confidence findings

🤖 Generated with [Claude Code](https://claude.com/claude-code)
EOF
)"
```

---

## Self-review checklist

- [ ] Migration number 054 matches spec.
- [ ] No store→handler shortcut: handler→service→store enforced.
- [ ] `CommitStatsService.Ingest` is idempotent (verified by upsert + test).
- [ ] `RepoService.OnPostReceive` is called from BOTH `internal/handler/git_http.go` (HTTP) AND `internal/ssh/server.go` (SSH).
- [ ] `AttentionService.ForUser` filters out self-actions and is scoped to issue-assigned only (PR review-request and mention paths deferred — schema not yet present).
- [ ] `CodeService.Mergeability` reuses `findMergeBase` / `mergeTreesNoConflict` / `resolveRef` from `code_service_merge.go` — no new helpers invented.
- [ ] `LanguageService.Percentages` returns descending percentages summing ≤ 100 and is called with `(owner, repoName, ref)` not a disk path.
- [ ] `Mergeability` returns separate flags for conflict vs behind so the UI can disable the right buttons.
- [ ] `MergeabilityBox` buttons use `hx-patch` against `/api/repos/{owner}/{repo}/pulls/{number}` with `state=merged&merge_strategy=ff|merge|squash` (per `docs/pr-merge.md`).
- [ ] `Heatmap` cell class names match the promoted utilities in Phase 0.
- [ ] `HomeData` keeps the embedded `BasePage` (not a named field).
- [ ] `PageHome`'s `/feed` redirect is removed; logged-in users see the home dashboard.
- [ ] Stat strip labels match the mockup: Repositories / Pull requests / Issues / Commits (last 7 days). No "Stars given".
- [ ] Pull-related code uses `pull.BaseBranch` / `pull.HeadBranch` / `pull.CreatedAt` / `pull.AuthorName` (no `BaseRef`/`HeadRef`/`OpenedAt`/`AuthorUsername`).
- [ ] Timeline uses existing `Timeline` / `TimelineItem` / `TimelineIcon` plus the new `TimelineEntry` variant — no `TimelineRoot`.
- [ ] Repo page sub-nav uses `@fragments.RepoSubnav(Active: "code", ...)`; PR page uses Phase 5's `PullChrome`.
- [ ] All three ports check both light and dark themes.
