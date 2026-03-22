# Cloudzilla Feature Roadmap

The following 4 phases extend Cloudzilla toward GitHub parity. Phase 1 is fully implemented. Phases 2–4 are planned — each is a self-contained, deployable unit.

**Cross-cutting rules (all phases):**

- No new Go dependencies needed
- Notification extension pattern: add const to `model/notification.go`, add `NotifyX` method to `notification_service.go` matching existing pattern
- Webhook extension: add event string + `XPayload()` method to `webhook_service.go`; no schema change
- PostgreSQL: `$N` placeholders, `RETURNING id`, `sql.NullInt64` for nullable FK columns (pattern from `repo_store.go`)
- Batch SQL `IN (...)`: build placeholder list with `strings.Join` and numbered `$N` params
- HTMX assignee/label API: POST body, DELETE query param — forms can't dynamically build URL paths without JS

---

## Phase 1 — Labels, Assignees, Stars ✅ IMPLEMENTED

### Labels

**Migrations:** `migrations/016_create_labels.sql` — creates `labels`, `issue_labels`, `pull_labels` tables.

**Files:**

- `internal/model/label.go` — `Label{ID, RepoID, Name, Color, Description, CreatedAt}`
- `internal/store/label_store.go` — `Create`, `ListByRepo`, `GetByID`, `Delete`, `AddToIssue`, `RemoveFromIssue`, `ListByIssue`, `AddToPull`, `RemoveFromPull`, `ListByPull`, `ListByIssueIDs` (batch), `ListByPullIDs` (batch), `scanLabels` helper
- `internal/service/label_service.go` — `NewLabelService(labels, repos, issues, pulls)`. Methods: `Create`, `ListByRepo`, `Delete`, `AddToIssue`, `RemoveFromIssue`, `GetForIssue`, `AddToPull`, `RemoveFromPull`, `GetForPull`, `BatchForIssues`, `BatchForPulls`
- `internal/handler/label_handler.go` — `ListLabels`, `CreateLabel`, `DeleteLabel`, `AddIssueLabel`, `RemoveIssueLabel`, `AddPullLabel`, `RemovePullLabel`, `renderIssueLabelFragment`, `renderPullLabelFragment`

**Routes:**

```
GET/POST   /api/repos/{owner}/{repo}/labels/
DELETE     /api/repos/{owner}/{repo}/labels/{id}
POST/DELETE /api/repos/{owner}/{repo}/issues/{number}/labels/{labelID}
POST/DELETE /api/repos/{owner}/{repo}/pulls/{number}/labels/{labelID}
```

**Templates:**

- `fragments/issue_labels.html` — `fragment-issue-labels` — color pills + remove buttons + `<details>` add-label picker
- `fragments/pull_labels.html` — `fragment-pull-labels` — same for PRs
- `fragments/repo_labels.html` — `fragment-repo-labels` — label CRUD in settings page

**Viewmodel additions:**

- `RepoSettingsData.Labels []model.Label`
- `IssuesData.IssueLabels map[int64][]model.Label`
- `PullsData.PullLabels map[int64][]model.Label`
- `IssueDetailData.Labels, AllLabels []model.Label`
- `PullDetailData.Labels, AllLabels []model.Label`
- New: `IssueLabelSidebarData`, `PullLabelSidebarData`, `RepoLabelsFragData`

**Template pattern for label lookup in list pages:** `{{$labels := index $.IssueLabels .ID}}`

### Assignees

**Migration:** `migrations/017_create_assignees.sql` — creates `issue_assignees`, `pull_assignees` tables.

**Files:**

- `internal/store/assignee_store.go` — `AddToIssue`, `RemoveFromIssue`, `ListByIssue`, `AddToPull`, `RemoveFromPull`, `ListByPull`, `scanUsers` helper (scans all user fields including `is_superadmin`, `is_invited`)
- `internal/service/assignee_service.go` — `NewAssigneeService(assignees, repos, issues, pulls, users)`. Methods: `AddToIssue`, `RemoveFromIssue`, `GetForIssue`, `AddToPull`, `RemoveFromPull`, `GetForPull`
- `internal/handler/assignee_handler.go` — `AddIssueAssignee`, `RemoveIssueAssignee`, `AddPullAssignee`, `RemovePullAssignee`. Uses `assigneeUsername(r)` for POST body, `assigneeUsernameDelete(r)` for DELETE query param

**Routes:**

```
POST/DELETE /api/repos/{owner}/{repo}/issues/{number}/assignees
POST/DELETE /api/repos/{owner}/{repo}/pulls/{number}/assignees
```

**Templates:**

- `fragments/issue_assignees.html` — `fragment-issue-assignees`
- `fragments/pull_assignees.html` — `fragment-pull-assignees`

**Viewmodel additions:**

- `IssueDetailData.Assignees []model.User`, `CanWrite bool`
- `PullDetailData.Assignees []model.User`, `CanWrite bool`
- New: `IssueAssigneeSidebarData`, `PullAssigneeSidebarData`

### Stars

**Migration:** `migrations/018_create_stars.sql` — creates `stars` table + `idx_stars_repo`, `idx_stars_user` indexes.

**Files:**

- `internal/store/star_store.go` — `Star`, `Unstar`, `CountByRepo`, `IsStarred`, `ListByUser`, `ListStargazers`. Uses `sql.NullInt64` for `org_id` in `scanRepos` helper.
- `internal/service/star_service.go` — `NewStarService(stars, repos, users)`. Methods: `Star`, `Unstar`, `GetStarCount`, `IsStarred`, `ListStargazers`, `ListByUser`
- `internal/handler/star_handler.go` — `StarRepo`, `UnstarRepo`, `ListStargazers`, `PageStargazers`, `PageUserStars`, `renderStarButtonFragment`

**Routes:**

```
POST/DELETE /api/repos/{owner}/{repo}/star
GET         /api/repos/{owner}/{repo}/stargazers  (optAuthMW)
GET         /{owner}/{repo}/stargazers             page
GET         /{owner}/stars                         page
```

Note: `/{owner}/stars` is registered **before** `/{owner}/{repo}` in `router.go` so the literal segment `stars` wins over the wildcard.

**Templates:**

- `fragments/star_button.html` — `fragment-star-button` — toggle star/unstar with count
- `pages/stargazers.html` — list of users who starred a repo
- `pages/user_stars.html` — repos a user has starred

**Page names registered in router:** `"stargazers"`, `"user_stars"`

**Viewmodel additions:**

- `RepoData.StarCount int`, `RepoData.IsStarred bool`
- New: `StarButtonData`, `StargazersData`, `UserStarsData`

---

## Phase 2 — Repository Fork ✅ IMPLEMENTED

_Cornerstone of the open-source contribution workflow._

**Migration** (`019_add_fork_columns.sql`):

```sql
ALTER TABLE repositories
    ADD COLUMN IF NOT EXISTS is_fork    BOOLEAN NOT NULL DEFAULT FALSE,
    ADD COLUMN IF NOT EXISTS fork_of_id BIGINT  REFERENCES repositories(id) ON DELETE SET NULL,
    ADD COLUMN IF NOT EXISTS fork_count INTEGER NOT NULL DEFAULT 0;
CREATE INDEX idx_repos_fork_of ON repositories(fork_of_id);
```

**Model changes (`internal/model/repository.go`):** add `IsFork bool`, `ForkOfID *int64`, `ForkOfOwner string`, `ForkOfName string`, `ForkCount int`.

**Store changes (`internal/store/repo_store.go`):**

- Add `Fork(ctx, orig *model.Repository, newOwnerID int64, newOwnerName string) (*model.Repository, error)` — INSERT with `is_fork=true`, `fork_of_id`
- Add `IncrementForkCount(ctx, repoID int64) error`
- Add `DecrementForkCount(ctx, repoID int64) error`
- Add `ListForks(ctx, repoID int64) ([]model.Repository, error)`

**Service changes (`internal/service/repo_service.go`):**

- Add `Fork(ctx, originalOwner, originalName, newOwnerUsername string, actorID int64) (*model.Repository, error)`:
  1. Fetch & validate original repo (must be readable by actor)
  2. Check no name collision under new owner (auto-suffix `-1` if needed)
  3. Create DB record via `store.Fork`
  4. Git-level: `filepath.Walk` copy the bare repo directory from `ReposRoot/originalOwner/originalName.git` → `ReposRoot/newOwner/repoName.git`
  5. Call `IncrementForkCount` on original

**New handler (`internal/handler/fork_handler.go`):**

- `ForkRepo` — POST authMW; on HTMX request returns `HX-Redirect` header to new repo URL; on plain request redirects

**Route:**

```
POST /api/repos/{owner}/{repo}/fork  (authMW)
```

**Templates:**

- `fragments/fork_button.html` — `fragment-fork-button` — fork count + button
- Extend `pages/repo.html` header: fork button + "Forked from `owner/repo`" badge when `IsFork`

**Viewmodel additions:**

- `RepoData.ForkCount int`, `RepoData.IsFork bool`, `RepoData.ForkOfPath string`

**Wire up:**

- `stores.go`: no new store (methods added to existing `RepoStore`)
- `services.go`: no new service (methods added to existing `RepoService`)
- `router.go`: register `POST /api/repos/{owner}/{repo}/fork`

---

## Phase 3 — Releases, Commit Status API, Milestones ✅ IMPLEMENTED

_"Ship something" workflow + CI integration + sprint planning._

### 3.1 Releases

**Migration** (`020_create_releases.sql`):

```sql
CREATE TABLE releases (
    id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    repo_id BIGINT NOT NULL REFERENCES repositories(id) ON DELETE CASCADE,
    tag_name TEXT NOT NULL, name TEXT NOT NULL DEFAULT '', body TEXT NOT NULL DEFAULT '',
    is_prerelease BOOLEAN NOT NULL DEFAULT FALSE, is_draft BOOLEAN NOT NULL DEFAULT FALSE,
    author_id BIGINT NOT NULL REFERENCES users(id),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(), updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    published_at TIMESTAMPTZ,
    UNIQUE(repo_id, tag_name)
);
```

**Files:**

- `internal/model/release.go` — `Release{ID, RepoID, TagName, Name, Body, IsPrerelease, IsDraft, AuthorID, CreatedAt, UpdatedAt, PublishedAt}`
- `internal/store/release_store.go` — `Create`, `ListByRepo`, `GetByTag`, `GetLatest`, `Update`, `Delete`
- `internal/service/release_service.go` — validates tag exists via `CodeService.ListRefs`
- `internal/handler/release_handler.go` — `PageReleases`, `PageReleaseDetail`, `CreateRelease`, `UpdateRelease`, `DeleteRelease`

**Routes:**

```
GET        /{owner}/{repo}/releases                          page
GET        /{owner}/{repo}/releases/tag/{tagName}           page
GET/POST   /api/repos/{owner}/{repo}/releases               (POST: authMW)
GET/PATCH/DELETE /api/repos/{owner}/{repo}/releases/{id}    (authMW)
GET        /api/repos/{owner}/{repo}/releases/latest
```

**Templates:** `pages/releases.html`, `pages/release_detail.html`. Add releases card to `pages/repo.html` grid (showing latest tag). Bodies rendered via `markdown.Render`.

**Page names to register:** `"releases"`, `"release_detail"`

**Webhook:** add `"release"` event type and `ReleasePayload` method to `WebhookService`.

**Wire up:**

- `stores.go`: add `Release *ReleaseStore`
- `services.go`: add `Release *ReleaseService` (depends on stores.Release + services.Code)
- `router.go`: register all release routes + page names

---

### 3.2 Commit Status API

**Migration** (`021_create_commit_statuses.sql`):

```sql
CREATE TABLE commit_statuses (
    id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    repo_id BIGINT NOT NULL REFERENCES repositories(id) ON DELETE CASCADE,
    sha TEXT NOT NULL, context TEXT NOT NULL DEFAULT 'default',
    state TEXT NOT NULL CHECK(state IN ('pending','success','failure','error')),
    target_url TEXT NOT NULL DEFAULT '', description TEXT NOT NULL DEFAULT '',
    creator_id BIGINT NOT NULL REFERENCES users(id),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(), updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE(repo_id, sha, context)
);
CREATE INDEX idx_commit_statuses_repo_sha ON commit_statuses(repo_id, sha);
```

**Files:**

- `internal/model/commit_status.go` — `CommitStatus` struct + `CommitStatusState` type constants (`pending`, `success`, `failure`, `error`)
- `internal/store/commit_status_store.go` — `Upsert` (via `ON CONFLICT DO UPDATE`), `ListBySHA`, `GetCombined` (aggregates to worst state)
- `internal/service/commit_status_service.go`
- `internal/handler/commit_status_handler.go`

**Routes:**

```
POST /api/repos/{owner}/{repo}/statuses/{sha}          (authMW)
GET  /api/repos/{owner}/{repo}/statuses/{sha}          (optAuthMW)
GET  /api/repos/{owner}/{repo}/commits/{sha}/status    (optAuthMW)
```

**Template changes (no new pages):**

- `pages/commit.html`: status bar below commit message; extend `CommitData` with `Statuses []model.CommitStatus`
- `pages/pull_detail.html`: combined status of head branch's latest commit; extend `PullDetailData` with `HeadStatuses []model.CommitStatus`
- `page_handler.go` `PageCommit` and `PagePullDetail`: call `CommitStatusService.List`

**Wire up:**

- `stores.go`: add `CommitStatus *CommitStatusStore`
- `services.go`: add `CommitStatus *CommitStatusService`
- `router.go`: register routes

---

### 3.3 Milestones

**Migration** (`022_create_milestones.sql`):

```sql
CREATE TABLE milestones (
    id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    repo_id BIGINT NOT NULL REFERENCES repositories(id) ON DELETE CASCADE,
    number INTEGER NOT NULL, title TEXT NOT NULL, description TEXT NOT NULL DEFAULT '',
    state TEXT NOT NULL DEFAULT 'open' CHECK(state IN ('open','closed')),
    due_date TIMESTAMPTZ, closed_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(), updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE(repo_id, number)
);
ALTER TABLE issues        ADD COLUMN IF NOT EXISTS milestone_id BIGINT REFERENCES milestones(id) ON DELETE SET NULL;
ALTER TABLE pull_requests ADD COLUMN IF NOT EXISTS milestone_id BIGINT REFERENCES milestones(id) ON DELETE SET NULL;
```

**Files:**

- `internal/model/milestone.go` — `Milestone{ID, RepoID, Number, Title, Description, State, DueDate, ClosedAt, OpenCount, ClosedCount}` (`OpenCount`/`ClosedCount` populated via JOIN)
- `internal/store/milestone_store.go` — `Create`, `ListByRepo`, `GetByNumber`, `Update`, `Delete`, `IncrementOpenCount`, `IncrementClosedCount`
- `internal/service/milestone_service.go`
- `internal/handler/milestone_handler.go`

**Model changes:**

- `internal/model/issue.go`: add `MilestoneID *int64` (use `sql.NullInt64` in store)
- `internal/model/pull_request.go`: add `MilestoneID *int64`

**Routes:**

```
GET              /{owner}/{repo}/milestones                          page
GET/POST         /api/repos/{owner}/{repo}/milestones               (POST: authMW)
GET/PATCH/DELETE /api/repos/{owner}/{repo}/milestones/{number}      (authMW)
PATCH            /api/repos/{owner}/{repo}/issues/{number}          extend UpdateIssue to accept milestone_id
PATCH            /api/repos/{owner}/{repo}/pulls/{number}           extend UpdatePull to accept milestone_id
```

**Templates:**

- `pages/milestones.html` — milestone list with progress bars
- Sidebar fragment in `pages/issue_detail.html` / `pages/pull_detail.html` for milestone assignment
- Filter by `?milestone=N` in issues/pulls list pages

**Page names to register:** `"milestones"`

**Wire up:**

- `stores.go`: add `Milestone *MilestoneStore`
- `services.go`: add `Milestone *MilestoneService`
- `router.go`: register all milestone routes + page name

---

## Phase 4 — PR Reviews, Line Comments, Search ✅ IMPLEMENTED

_Professional code review workflow + content discoverability._

### 4.1 PR Reviews

**Migration** (`023_create_pr_reviews.sql`):

```sql
CREATE TABLE pull_reviews (
    id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    pull_id BIGINT NOT NULL REFERENCES pull_requests(id) ON DELETE CASCADE,
    repo_id BIGINT NOT NULL REFERENCES repositories(id) ON DELETE CASCADE,
    author_id BIGINT NOT NULL REFERENCES users(id), author_name TEXT NOT NULL DEFAULT '',
    state TEXT NOT NULL CHECK(state IN ('approved','changes_requested','commented','pending')),
    body TEXT NOT NULL DEFAULT '', submitted_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(), updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE(pull_id, author_id)
);
```

**Files:**

- `internal/model/pull_review.go` — `PullReview` struct + `PRReviewState` type constants
- `internal/store/pull_review_store.go` — `Upsert` (via ON CONFLICT), `ListByPull`, `CountApprovals`, `HasChangesRequested`
- `internal/service/pull_review_service.go` — `SubmitReview` (validates reviewer ≠ PR author); `CanMerge` (blocks if any `changes_requested`)
- `internal/handler/pull_review_handler.go`

**Routes:**

```
GET/POST /api/repos/{owner}/{repo}/pulls/{number}/reviews  (POST: authMW)
```

**Template changes:**

- `fragments/pr_reviews.html` — `fragment-pr-reviews` — reviewer list with status badges + review submission form (textarea + radio: approved / request changes / comment)
- `pages/pull_detail.html`: add reviews section; gate merge buttons behind `CanMerge`
- `pull_handler.go` `UpdatePull`: call `PullReviewService.CanMerge` before executing merge; return 422 if blocked

**Viewmodel additions:**

- `PullDetailData.Reviews []model.PullReview`, `CanMerge bool`, `MergeBlockReason string`

**Notification:** add `NotifPRReview` notification type; `NotificationService.NotifyPRReview` fires on review submission.

**Wire up:**

- `stores.go`: add `PullReview *PullReviewStore`
- `services.go`: add `PullReview *PullReviewService`
- `router.go`: register route

---

### 4.2 PR Line Comments

**Migration** (`024_create_pull_line_comments.sql`):

```sql
CREATE TABLE pull_line_comments (
    id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    pull_id BIGINT NOT NULL REFERENCES pull_requests(id) ON DELETE CASCADE,
    repo_id BIGINT NOT NULL REFERENCES repositories(id) ON DELETE CASCADE,
    author_id BIGINT NOT NULL REFERENCES users(id), author_name TEXT NOT NULL DEFAULT '',
    path TEXT NOT NULL, diff_side TEXT NOT NULL DEFAULT 'right' CHECK(diff_side IN ('left','right')),
    line INTEGER NOT NULL, body TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(), updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX idx_pull_line_comments_pull      ON pull_line_comments(pull_id);
CREATE INDEX idx_pull_line_comments_pull_path ON pull_line_comments(pull_id, path);
```

**Files:**

- `internal/model/pull_line_comment.go`
- `internal/store/pull_line_comment_store.go` — `Create`, `ListByPull`, `ListByPullAndPath`, `Delete`
- `internal/service/pull_line_comment_service.go`
- `internal/handler/pull_line_comment_handler.go`

**Routes:**

```
GET/POST /api/repos/{owner}/{repo}/pulls/{number}/line_comments           (POST: authMW)
DELETE   /api/repos/{owner}/{repo}/pulls/{number}/line_comments/{id}      (authMW)
```

**Template changes:**

- `pages/pull_detail.html` diff table: add `data-path` + `data-line` attrs to diff rows; "+" click target reveals inline comment form; comments injected as table rows between diff lines
- Viewmodel: add `LineComments map[string][]RenderedLineComment` to `PullDetailData` (keyed by `path`, pre-rendered body HTML)
- `page_handler.go` `PagePullDetail`: call `PullLineCommentService.ListByPull`, group by path

**Wire up:**

- `stores.go`: add `PullLineComment *PullLineCommentStore`
- `services.go`: add `PullLineComment *PullLineCommentService`
- `router.go`: register routes

---

### 4.3 Search

**Migration** (`025_search_indexes.sql`):

```sql
-- Add tsvector columns + GIN indexes + BEFORE INSERT OR UPDATE triggers to:
--   repositories (name + description), issues (title + body), pull_requests (title + body)
-- User search:
CREATE INDEX IF NOT EXISTS idx_users_username_lower ON users(lower(username));
```

**Files:**

- `internal/store/search_store.go` — `SearchRepos` (with private-visibility filter), `SearchIssues`, `SearchPulls`, `SearchUsers`
- `internal/service/search_service.go` — runs 4 queries in parallel via `errgroup`; returns `SearchResults{Repos, Issues, Pulls, Users}`
- `internal/handler/search_handler.go` — `PageSearch`

**Route:**

```
GET /search?q=...&type=repos|issues|pulls|users  (optAuthMW)
```

**Templates:**

- `pages/search.html` — tabbed results (repos / issues / pulls / users)
- Add search input to `templates/layout.html` navbar

**Page names to register:** `"search"`

**Wire up:**

- `stores.go`: add `Search *SearchStore`
- `services.go`: add `Search *SearchService`
- `router.go`: register route + page name

---

## Roadmap Summary

| Phase | Feature           | Status  | Migration(s) |
| ----- | ----------------- | ------- | ------------ |
| 1.1   | Labels            | ✅ Done | 016          |
| 1.2   | Assignees         | ✅ Done | 017          |
| 1.3   | Stars             | ✅ Done | 018          |
| 2     | Repository Fork   | ✅ Done | 019          |
| 3.1   | Releases          | ✅ Done | 020          |
| 3.2   | Commit Status API | ✅ Done | 021          |
| 3.3   | Milestones        | ✅ Done | 022          |
| 4.1   | PR Reviews        | ✅ Done | 023          |
| 4.2   | PR Line Comments  | ✅ Done | 024          |
| 4.3   | Search            | ✅ Done | 025          |

**Critical files touched by every phase:**

- `internal/router/router.go` — register new routes + add page names to `pageNames`
- `internal/handler/viewmodels.go` — add data structs for new pages/fragments
- `internal/service/services.go` — wire new service into `Services` struct + `New()`
- `internal/store/stores.go` — wire new store into `Stores` struct + `New()`
- `internal/handler/page_handler.go` — extend existing page handlers with new data fetches
