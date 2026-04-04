# Cloudzilla Feature Roadmap

**Cross-cutting rules (all phases):**

- No new Go dependencies needed
- Notification extension pattern: add const to `model/notification.go`, add `NotifyX` method to `notification_service.go` matching existing pattern
- Webhook extension: add event string + `XPayload()` method to `webhook_service.go`; no schema change
- PostgreSQL: `$N` placeholders, `RETURNING id`, `sql.NullInt64` for nullable FK columns (pattern from `repo_store.go`)
- Batch SQL `IN (...)`: build placeholder list with `strings.Join` and numbered `$N` params
- HTMX assignee/label API: POST body, DELETE query param — forms can't dynamically build URL paths without JS

---

## Phase 0 — Core Platform ✅ IMPLEMENTED

_The foundational layer: identity, repositories, collaboration primitives, and instance management. Migrations 001–015._

### 0.1 — Users, Repositories, Issues, PRs, Comments, Permissions, SSH Keys

_Migrations 001–007 — the minimum viable git hosting platform._

**Migration 001 — Users:**

```sql
CREATE TABLE users (
    id            BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    username      TEXT NOT NULL UNIQUE,
    email         TEXT NOT NULL UNIQUE,
    password_hash TEXT NOT NULL,
    bio           TEXT NOT NULL DEFAULT '',
    avatar_url    TEXT NOT NULL DEFAULT '',
    created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX idx_users_username ON users(username);
CREATE INDEX idx_users_email    ON users(email);
```

**Model:** `internal/model/user.go` — `User{ID, Username, Email, PasswordHash, Bio, AvatarURL, CreatedAt, UpdatedAt}`. `Authenticate(password)` compares bcrypt hash.

**Store:** `internal/store/user_store.go` — `Create`, `GetByID`, `GetByUsername`, `GetByEmail`, `Update`, `List`.

**Service:** `internal/service/user_service.go` — `Create` (validates uniqueness, hashes password); `Authenticate` (fetches by username/email, compares hash, returns JWT); `Update`.

**Handler:** `internal/handler/auth_handler.go` — `Register` (form POST), `Login` (form POST + API JSON), `Logout`. Sets `cz_token` httpOnly cookie on login.

---

**Migration 002 — Repositories:**

```sql
CREATE TABLE repositories (
    id             BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    owner_id       BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    name           TEXT NOT NULL,
    description    TEXT NOT NULL DEFAULT '',
    private        BOOLEAN NOT NULL DEFAULT FALSE,
    default_branch TEXT NOT NULL DEFAULT 'main',
    created_at     TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at     TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE(owner_id, name)
);
CREATE INDEX idx_repos_owner ON repositories(owner_id);
```

**Model:** `internal/model/repository.go` — `Repository{ID, OwnerID, Name, Description, Private, DefaultBranch}`.

**Store:** `internal/store/repo_store.go` — `Create`, `GetByOwnerAndName`, `ListByOwner`, `Update`, `Delete`. Bare git repo created on disk via `go-git.PlainInit()`.

**Service:** `internal/service/repo_service.go` — `Create` (validates name, inits bare repo); `CanRead`, `CanWrite`, `CanManage`; `TransferRepo`.

**Git HTTP:** `internal/handler/git_handler.go` — `InfoRefs`, `UploadPack`, `ReceivePack`. Implements Git smart HTTP protocol via `go-git`. Auth: Basic or JWT cookie.

---

**Migration 003 — Issues:**

```sql
CREATE TABLE issues (
    id        BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    repo_id   BIGINT NOT NULL REFERENCES repositories(id) ON DELETE CASCADE,
    number    INTEGER NOT NULL,
    author_id BIGINT NOT NULL REFERENCES users(id),
    title     TEXT NOT NULL,
    body      TEXT NOT NULL DEFAULT '',
    state     TEXT NOT NULL DEFAULT 'open' CHECK(state IN ('open','closed')),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    closed_at  TIMESTAMPTZ,
    UNIQUE(repo_id, number)
);
CREATE INDEX idx_issues_repo ON issues(repo_id);
CREATE INDEX idx_issues_state ON issues(state);
```

**Model:** `internal/model/issue.go`.

**Store:** `internal/store/issue_store.go` — `Create`, `GetByNumber`, `ListByRepo(state, page)`, `Update`, `Delete`, `NextNumber`.

**Service:** `internal/service/issue_service.go`.

**Handler:** `internal/handler/issue_handler.go` — `PageIssues`, `PageIssueDetail`, `CreateIssue`, `UpdateIssue` (open/close toggle via HTMX), `DeleteIssue`.

**Routes:**

```
GET      /{owner}/{repo}/issues               page
GET      /{owner}/{repo}/issues/{number}      page
GET/POST /api/repos/{owner}/{repo}/issues     (POST: authMW)
PATCH    /api/repos/{owner}/{repo}/issues/{number}  (authMW)
```

---

**Migration 004 — Pull Requests:**

```sql
CREATE TABLE pull_requests (
    id          BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    repo_id     BIGINT NOT NULL REFERENCES repositories(id) ON DELETE CASCADE,
    number      INTEGER NOT NULL,
    author_id   BIGINT NOT NULL REFERENCES users(id),
    title       TEXT NOT NULL,
    body        TEXT NOT NULL DEFAULT '',
    state       TEXT NOT NULL DEFAULT 'open' CHECK(state IN ('open','closed','merged')),
    head_branch TEXT NOT NULL,
    base_branch TEXT NOT NULL DEFAULT 'main',
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    merged_at   TIMESTAMPTZ,
    closed_at   TIMESTAMPTZ,
    UNIQUE(repo_id, number)
);
CREATE INDEX idx_prs_repo  ON pull_requests(repo_id);
CREATE INDEX idx_prs_state ON pull_requests(state);
```

**Store:** `internal/store/pull_store.go` — `Create`, `GetByNumber`, `ListByRepo`, `Update`, `NextNumber`.

**Service:** `internal/service/pull_service.go` — `Create`; `SetState` (open/close/merge); merge strategies wired here (see Phase 2+).

**Handler:** `internal/handler/pull_handler.go` — `PagePulls`, `PagePullDetail`, `CreatePull`, `UpdatePull`.

**Routes:**

```
GET      /{owner}/{repo}/pulls               page
GET      /{owner}/{repo}/pulls/{number}      page
GET/POST /api/repos/{owner}/{repo}/pulls     (POST: authMW)
PATCH    /api/repos/{owner}/{repo}/pulls/{number}  (authMW)
```

---

**Migration 005 — Comments:**

```sql
CREATE TABLE comments (
    id        BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    repo_id   BIGINT NOT NULL REFERENCES repositories(id) ON DELETE CASCADE,
    issue_id  BIGINT REFERENCES issues(id) ON DELETE CASCADE,
    pull_id   BIGINT REFERENCES pull_requests(id) ON DELETE CASCADE,
    author_id BIGINT NOT NULL REFERENCES users(id),
    body      TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CHECK (
        (issue_id IS NOT NULL AND pull_id IS NULL) OR
        (issue_id IS NULL  AND pull_id IS NOT NULL)
    )
);
CREATE INDEX idx_comments_issue ON comments(issue_id);
CREATE INDEX idx_comments_pull  ON comments(pull_id);
```

Single `comments` table serves both issue and PR comments. `body` rendered as markdown via `markdown.Render` in templates.

**Handler:** `internal/handler/comment_handler.go` — `CreateIssueComment`, `CreatePRComment`, `UpdateComment`, `DeleteComment`.

---

**Migration 006 — Repository Permissions (Collaborators):**

```sql
CREATE TABLE permissions (
    id      BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    repo_id BIGINT NOT NULL REFERENCES repositories(id) ON DELETE CASCADE,
    role    TEXT NOT NULL CHECK(role IN ('owner','admin','writer','reader')),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE(user_id, repo_id)
);
```

Roles: `reader` (read private repos), `writer` (push), `admin` (write + see settings; cannot manage collaborators).

**Store:** `internal/store/permission_store.go` — `Set`, `Get`, `ListByRepo` (`ListPermissionsWithUsername` via JOIN), `Delete`.

**Service:** `internal/service/permission_service.go`.

**Handler:** `internal/handler/settings_handler.go` — collaborator CRUD; HTMX-aware; renders `fragment-repo-collaborators`.

---

**Migration 007 — SSH Keys:**

```sql
CREATE TABLE ssh_keys (
    id          BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    user_id     BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    title       TEXT NOT NULL,
    public_key  TEXT NOT NULL,
    fingerprint TEXT NOT NULL UNIQUE,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX idx_ssh_keys_fingerprint ON ssh_keys(fingerprint);
```

MD5 fingerprint computed at insert time; used for fast SSH handshake lookup.

**SSH server:** `internal/ssh/` — `gliderlabs/ssh`-based server on port 2222; dispatches `git-upload-pack` / `git-receive-pack` after public key auth via fingerprint lookup.

---

### 0.2 — Google OAuth, Organizations

_Migrations 008–010 — extended identity and shared namespaces._

**Migration 008 — Google OAuth:**

```sql
ALTER TABLE users ADD COLUMN oauth_provider TEXT NOT NULL DEFAULT '';
ALTER TABLE users ADD COLUMN oauth_id       TEXT NOT NULL DEFAULT '';
CREATE UNIQUE INDEX users_oauth_idx ON users(oauth_provider, oauth_id)
    WHERE oauth_provider != '';
```

**Flow:** `GET /auth/google` → OAuth2 redirect → `GET /auth/google/callback` → exchange code → fetch userinfo → upsert user (link by `oauth_id`, or by email, or create new) → set `cz_token` cookie.

**Handler:** `internal/handler/auth_handler.go` — `GoogleOAuth`, `GoogleOAuthCallback`.

---

**Migration 009 — Organizations:**

```sql
CREATE TABLE organizations (
    id           BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    name         TEXT NOT NULL UNIQUE,
    display_name TEXT NOT NULL DEFAULT '',
    description  TEXT NOT NULL DEFAULT '',
    avatar_url   TEXT NOT NULL DEFAULT '',
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE TABLE org_members (
    id      BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    org_id  BIGINT NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    role    TEXT NOT NULL CHECK(role IN ('owner','member')),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE(org_id, user_id)
);
```

**Service:** `internal/service/org_service.go` — `Create` (auto-adds creator as owner); `AddMember`, `RemoveMember` (blocks removing last owner); `IsOwner`, `IsMember`; `CreateRepo`; `TransferOrg`.

**Handler:** `internal/handler/org_handler.go` — `PageOrg`, `PageOrgSettings`; org CRUD + member management.

**Routes:**

```
GET    /{org}                                    page (fallback after user lookup)
GET    /orgs/{org}/settings                      page (owner only)
POST   /api/orgs/
GET    /api/orgs/{org}
POST/DELETE /api/orgs/{org}/members
POST   /api/orgs/{org}/repos
POST   /api/orgs/{org}/transfer
```

---

**Migration 010 — Repo `owner_name` + Org FK:**

```sql
ALTER TABLE repositories ADD COLUMN owner_name TEXT NOT NULL DEFAULT '';
ALTER TABLE repositories ADD COLUMN org_id     INTEGER REFERENCES organizations(id) ON DELETE CASCADE;
UPDATE repositories SET owner_name = (SELECT username FROM users WHERE users.id = repositories.owner_id);
CREATE INDEX idx_repos_owner_name ON repositories(owner_name);
CREATE INDEX idx_repos_org        ON repositories(org_id);
```

`owner_name` is a denormalized column updated on ownership transfer; enables URL pattern `/{owner}/{repo}` without a JOIN.

---

### 0.3 — Webhooks, Notifications, Superadmin, Site Settings, Invitations

_Migrations 011–015 — instance management and event system._

**Migration 011 — Webhooks:**

```sql
CREATE TABLE webhooks (
    id      BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    repo_id BIGINT NOT NULL REFERENCES repositories(id) ON DELETE CASCADE,
    url     TEXT NOT NULL,
    secret  TEXT NOT NULL DEFAULT '',
    events  TEXT NOT NULL DEFAULT 'push,issues,pull_request',
    active  BOOLEAN NOT NULL DEFAULT TRUE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE TABLE webhook_deliveries (
    id            BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    webhook_id    BIGINT NOT NULL REFERENCES webhooks(id) ON DELETE CASCADE,
    event         TEXT NOT NULL,
    payload       TEXT NOT NULL,
    response_code INTEGER,
    response_body TEXT NOT NULL DEFAULT '',
    error         TEXT NOT NULL DEFAULT '',
    delivered_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
```

Fire-and-forget delivery via `go s.Dispatch(...)`. HMAC-SHA256 signature in `X-Hub-Signature-256` header when secret is set. Supported events: `push`, `issues`, `pull_request`.

**Service:** `internal/service/webhook_service.go` — `Create`, `ListByRepo`, `Delete`, `ListDeliveries`, `Dispatch`, `PushPayload`, `IssuePayload`, `PullPayload`.

---

**Migration 012 — Notifications:**

```sql
CREATE TABLE notifications (
    id          BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    user_id     BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    actor_id    BIGINT NOT NULL REFERENCES users(id),
    actor_name  TEXT NOT NULL DEFAULT '',
    type        TEXT NOT NULL CHECK(type IN (
                    'issue_comment','pr_comment',
                    'issue_closed','issue_reopened',
                    'pr_merged','pr_closed','pr_opened'
                )),
    repo_id     BIGINT NOT NULL REFERENCES repositories(id) ON DELETE CASCADE,
    repo_name   TEXT NOT NULL DEFAULT '',
    owner_name  TEXT NOT NULL DEFAULT '',
    subject_id  BIGINT NOT NULL,
    subject_url TEXT NOT NULL DEFAULT '',
    read        BOOLEAN NOT NULL DEFAULT FALSE,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX idx_notifications_user_read ON notifications(user_id, read);
```

Self-actions (`actorID == authorID`) never produce notifications. Unread count shown in navbar badge via `basePage()`.

**Service:** `internal/service/notification_service.go` — `List`, `CountUnread`, `MarkRead`, `MarkAllRead`, `NotifyIssueComment`, `NotifyPRComment`, `NotifyIssueStateChange`, `NotifyPRStateChange`.

---

**Migration 013 — Superadmin flag:**

```sql
ALTER TABLE users ADD COLUMN is_superadmin BOOLEAN NOT NULL DEFAULT FALSE;
```

First user created via `/setup` wizard gets `is_superadmin = TRUE`. Superadmins bypass `allow_login` checks and can access `/admin/settings`.

---

**Migration 014 — Site Settings:**

```sql
CREATE TABLE site_settings (key TEXT PRIMARY KEY, value TEXT NOT NULL);
INSERT INTO site_settings VALUES ('allow_registration', 'true') ON CONFLICT DO NOTHING;
INSERT INTO site_settings VALUES ('allow_login',        'true') ON CONFLICT DO NOTHING;
```

`SiteSettingService` caches values in a `sync.RWMutex`-protected map. `IsSetupComplete()` cached via `sync/atomic.Bool` — never re-queries once true.

**Setup wizard:** `GET /setup` — only accessible when no users exist (enforced by `RequireSetup` middleware). Creates first superadmin account.

---

**Migration 015 — Invitations:**

```sql
CREATE TABLE invitations (
    id            BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    token         TEXT NOT NULL UNIQUE,
    email         TEXT NOT NULL,
    invited_by_id BIGINT NOT NULL REFERENCES users(id),
    expires_at    TIMESTAMPTZ NOT NULL,
    accepted_at   TIMESTAMPTZ,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
ALTER TABLE users ADD COLUMN is_invited BOOLEAN NOT NULL DEFAULT FALSE;
```

32-byte hex token; 7-day expiry. Invited users bypass `allow_registration` + `allow_login` locks. Superadmin creates invites via `/admin/settings`; shares `/invite/{token}` link manually — no SMTP required.

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

---

## Phase 5 — Personal Access Tokens, Deploy Keys, Draft Pull Requests ✅ IMPLEMENTED

_Programmatic API access, CI/CD key isolation, and work-in-progress PRs._

### 5.1 Personal Access Tokens (PAT)

_Programmatic API access without exposing the user's password._

**Migration** (`027_create_access_tokens.sql`):

```sql
CREATE TABLE access_tokens (
    id           BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    user_id      BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    name         TEXT NOT NULL,
    token_hash   TEXT NOT NULL UNIQUE,   -- SHA-256 hex of the raw token
    last_eight   TEXT NOT NULL,          -- last 8 chars for display
    scopes       TEXT[] NOT NULL DEFAULT '{}',
    last_used_at TIMESTAMPTZ,
    expires_at   TIMESTAMPTZ,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX idx_access_tokens_user   ON access_tokens(user_id);
CREATE INDEX idx_access_tokens_hash   ON access_tokens(token_hash);
```

Raw token format: `czp_<32-byte hex>` (prefix `czp_` for Cloudzilla PAT). Only the raw token is shown once at creation time; only its SHA-256 hash is stored.

**Scopes:** `repo:read`, `repo:write`, `issues:write`, `pulls:write`, `admin` (reserved for future)

**Files:**

- `internal/model/access_token.go` — `AccessToken{ID, UserID, Name, TokenHash, LastEight, Scopes, LastUsedAt, ExpiresAt, CreatedAt}`
- `internal/store/access_token_store.go` — `Create`, `ListByUser`, `GetByHash`, `UpdateLastUsed`, `Delete`
- `internal/service/access_token_service.go` — `Generate(ctx, userID, name, scopes, expiresAt)` → `(rawToken string, token *AccessToken, error)`; `Validate(ctx, rawToken)` → `(*AccessToken, *model.User, error)`; `List`, `Delete`
- `internal/handler/access_token_handler.go` — `PageTokens`, `CreateToken`, `DeleteToken`; `PageTokens` passes newly-created raw token via query param (one-time display via `?new_token=...` after redirect)

**Auth middleware extension (`internal/middleware/auth.go`):**

Check `Authorization: Bearer czp_*` prefix first; if matched, call `AccessTokenService.Validate` instead of JWT decode. Populate `context` with user claims the same way JWT does. Update `last_used_at` asynchronously (`go service.UpdateLastUsed(...)`).

**Routes:**

```
GET    /settings/tokens             (authMW)   page — list + create form
POST   /api/user/tokens             (authMW)   — create PAT
DELETE /api/user/tokens/{id}        (authMW)   — revoke PAT
```

**Templates:**

- `pages/tokens.html` — token list (name, scopes, last used, expiry, revoke button) + create form (name, scope checkboxes, optional expiry date)
- One-time raw token displayed in a highlighted box when `?new_token=...` present in URL

**Navbar:** add "Settings" link to user dropdown leading to `/settings/tokens`.

**Page names to register:** `"tokens"`

**Wire up:**

- `stores.go`: add `AccessToken *AccessTokenStore`
- `services.go`: add `AccessToken *AccessTokenService`
- `router.go`: register routes + page name; pass `AccessTokenService` into auth middleware

---

### 5.2 Deploy Keys

_Per-repository SSH keys with read-only or read-write access; designed for CI/CD pipelines._

**Migration** (`028_create_deploy_keys.sql`):

```sql
CREATE TABLE deploy_keys (
    id          BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    repo_id     BIGINT NOT NULL REFERENCES repositories(id) ON DELETE CASCADE,
    title       TEXT NOT NULL,
    fingerprint TEXT NOT NULL,
    public_key  TEXT NOT NULL,
    read_only   BOOLEAN NOT NULL DEFAULT TRUE,
    last_used_at TIMESTAMPTZ,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE(repo_id, fingerprint)
);
CREATE INDEX idx_deploy_keys_fingerprint ON deploy_keys(fingerprint);
```

**Files:**

- `internal/model/deploy_key.go` — `DeployKey{ID, RepoID, Title, Fingerprint, PublicKey, ReadOnly, LastUsedAt, CreatedAt}`
- `internal/store/deploy_key_store.go` — `Create`, `ListByRepo`, `GetByFingerprint`, `UpdateLastUsed`, `Delete`
- `internal/service/deploy_key_service.go` — `Add(ctx, repoID, managerID, title, publicKey, readOnly)` (validates key format, computes MD5 fingerprint, checks uniqueness across `deploy_keys` + `ssh_keys`); `List`, `Delete`
- `internal/handler/deploy_key_handler.go` — `ListDeployKeys`, `AddDeployKey`, `DeleteDeployKey`; HTMX-aware; renders `fragment-deploy-keys`

**SSH auth change (`internal/ssh/`):**

During SSH public key lookup, check `deploy_keys` table in addition to `ssh_keys`. If a deploy key is found, populate context with `deployKeyID` + associated `repoID`. In `git-upload-pack` and `git-receive-pack` dispatch: if `deployKeyID` is set, enforce that the accessed repo matches the key's `repo_id`; block `receive-pack` for `read_only = TRUE` keys.

**Routes:**

```
GET/POST   /api/repos/{owner}/{repo}/keys        (authMW + CanManage)
DELETE     /api/repos/{owner}/{repo}/keys/{id}   (authMW + CanManage)
```

**Template:** add "Deploy Keys" section to `pages/repo_settings.html`; swaps `fragment-deploy-keys` into `#deploy-keys`.

**Wire up:**

- `stores.go`: add `DeployKey *DeployKeyStore`
- `services.go`: add `DeployKey *DeployKeyService`
- `router.go`: register routes

---

### 5.3 Draft Pull Requests

_Work-in-progress PRs that block merging until explicitly marked ready for review._

**Migration** (`029_add_draft_to_pulls.sql`):

```sql
ALTER TABLE pull_requests
    ADD COLUMN IF NOT EXISTS is_draft    BOOLEAN NOT NULL DEFAULT FALSE,
    ADD COLUMN IF NOT EXISTS draft_at    TIMESTAMPTZ;
```

**Model change (`internal/model/pull_request.go`):** add `IsDraft bool`, `DraftAt *time.Time`.

**Service change (`internal/service/pull_service.go`):**

- `CreatePull`: accept `is_draft` flag; draft PRs are excluded from review-requirement checks until converted.
- `SetDraft(ctx, pullID, userID, isDraft bool) error` — toggle; only PR author or repo manager can toggle. Sets `draft_at` on first draft.
- `CanMerge`: always return false (with reason `"draft"`) when `IsDraft == true`.

**Handler change (`internal/handler/pull_handler.go`):**

- `CreatePull`: read `is_draft` from form.
- `UpdatePull`: accept `is_draft` field in PATCH body; dispatch to `SetDraft`; return updated PR fragment.

**Template changes:**

- `pages/pulls.html`: show "Draft" badge on draft PRs in the list; add "Drafts" filter tab alongside Open/Closed.
- `pages/pull_detail.html`: show yellow "Draft" banner; replace merge buttons with "Convert to ready for review" button (HTMX PATCH with `is_draft=false`); hide review request form for drafts.
- `fragments/pull_detail.html`: update `fragment-pull-detail` to reflect draft state.

**Viewmodel:** add `IsDraft bool` to `PullDetailData` and `PullsData` item.

**Wire up:** no new store/service; changes to existing `PullService` + handler + templates only.

---

## Phase 6 — Protected Branches, CODEOWNERS Support, Code Review Suggestions ✅ IMPLEMENTED

_Workflow quality gates, automated review routing, and inline suggested changes._

### 6.1 Protected Branches

**Migration** (`030_create_branch_protections.sql`):

```sql
CREATE TABLE branch_protections (
    id                    BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    repo_id               BIGINT NOT NULL REFERENCES repositories(id) ON DELETE CASCADE,
    pattern               TEXT NOT NULL,
    require_review_count  INTEGER NOT NULL DEFAULT 0,
    require_status_checks TEXT[] NOT NULL DEFAULT '{}',
    block_force_push      BOOLEAN NOT NULL DEFAULT TRUE,
    created_at            TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at            TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE(repo_id, pattern)
);
```

**Files:**

- `internal/model/branch_protection.go` — `BranchProtection{ID, RepoID, Pattern, RequireReviewCount, RequireStatusChecks, BlockForcePush}`
- `internal/store/branch_protection_store.go` — `Create`, `ListByRepo`, `GetByRepoAndPattern`, `Update`, `Delete`, `MatchForBranch(ctx, repoID, branchName)` (returns first matching rule or nil)
- `internal/service/branch_protection_service.go` — `Create`, `List`, `Update`, `Delete`; `CheckPush(ctx, repo, branchName, pusherID)` → `error` (returns sentinel `ErrForcePushBlocked`, `ErrPushRequiresPR`); `CheckMerge(ctx, repo, pr, actorID)` → `error` (returns `ErrInsufficientReviews`, `ErrStatusCheckFailed`)
- `internal/handler/branch_protection_handler.go` — CRUD handlers; HTMX-aware; renders `fragment-branch-protections`

**Pattern matching:** Simple glob — `*` matches any single path segment, `**` is not needed. `fnmatch`-style: `main` matches exactly, `release/*` matches `release/v1.0`. Implement as `strings.HasPrefix` / `filepath.Match` (stdlib, no new deps).

**Routes:**

```
GET/POST   /api/repos/{owner}/{repo}/branches/protections            (authMW + CanManage)
PATCH/DELETE /api/repos/{owner}/{repo}/branches/protections/{id}     (authMW + CanManage)
```

**Integration:**

- SSH `git-receive-pack`: call `BranchProtectionService.CheckPush` after auth; return git-protocol error message on block
- HTTP `git-receive-pack` handler: same check
- `UpdatePull` merge handler: call `BranchProtectionService.CheckMerge` before dispatching merge; return 422 on block

**Template changes:**

- `pages/repo_settings.html`: new "Branch Protection" section; HTMX form to add/edit/delete rules; swaps `fragment-branch-protections` into `#branch-protections`
- `fragments/branch_protections.html` — `fragment-branch-protections`

**Wire up:**

- `stores.go`: add `BranchProtection *BranchProtectionStore`
- `services.go`: add `BranchProtection *BranchProtectionService`
- `router.go`: register routes

---

### 6.2 CODEOWNERS Support

_Automatically request reviews from code owners when a PR touches their files._

**No migration.** CODEOWNERS file is read from the repo on PR creation.

**Convention (GitHub-compatible):**

```
# CODEOWNERS file at repo root or .github/CODEOWNERS on the default branch
*.go    @backend-team @alice
docs/   @bob
```

**`CodeService` additions:**

- `GetCodeOwners(owner, repo, defaultBranch string) ([]CodeOwnerRule, error)` — reads `CODEOWNERS` or `.github/CODEOWNERS`; parses lines into `{Pattern string, Owners []string}`. Returns empty slice (not error) if file absent.
- `MatchCodeOwners(rules []CodeOwnerRule, changedFiles []string) ([]string, error)` — returns de-duped owner usernames whose patterns match any changed file. Uses `filepath.Match` for glob matching.

**Service change (`internal/service/pull_service.go`):**

- `CreatePull`: after creation, call `CodeService.GetCodeOwners` + `CodeService.MatchCodeOwners` on the PR's changed files (from `CodeService.GetPullDiff`). For each matched username that exists in DB, call `AssigneeService.AddToPull` automatically.

**Template change (`pages/pull_detail.html`):** Assignees added via CODEOWNERS show a "code owner" tooltip badge.

**No new handler, store, or service files required.** Logic lives in `CodeService` + `PullService.CreatePull`.

---

### 6.3 Code Review Suggestions

_Inline "suggested change" blocks in PR line comments that can be applied with one click._

**Migration** (`031_add_suggestion_to_line_comments.sql`):

```sql
ALTER TABLE pull_line_comments
    ADD COLUMN IF NOT EXISTS is_suggestion  BOOLEAN NOT NULL DEFAULT FALSE,
    ADD COLUMN IF NOT EXISTS suggestion_body TEXT NOT NULL DEFAULT '';
```

**Convention (GitHub-compatible):** A line comment body containing a fenced code block tagged ` ```suggestion ` sets `is_suggestion = TRUE` and `suggestion_body` = the block content.

**Parse logic (`internal/service/pull_line_comment_service.go`):**

`Create`: after saving, if body contains ` ```suggestion\n...\n``` `, extract `suggestion_body` and set `is_suggestion = TRUE` (regex or simple string scan — no new deps).

**Apply suggestion flow:**

1. Button click → `POST /api/repos/{owner}/{repo}/pulls/{number}/line_comments/{id}/apply` (authMW + write)
2. `ApplySuggestion` handler: fetch comment, verify `is_suggestion`, read current file blob at `head_sha`, replace lines `[line..line+suggestion_lines-1]` with `suggestion_body`, commit directly to head branch (signed as the applier), return `HX-Redirect` to updated PR diff.

**`CodeService` addition:**

- `ApplySuggestion(owner, repo, branch, path string, startLine, endLine int, replacement, authorName, authorEmail, commitMsg string) error` — reads blob, replaces line range, writes new tree + commit, updates branch ref.

**Handler:** `internal/handler/pull_line_comment_handler.go` — add `ApplySuggestion`.

**Route:**

```
POST /api/repos/{owner}/{repo}/pulls/{number}/line_comments/{id}/apply  (authMW + write)
```

**Template change (`pages/pull_detail.html`):**

- Render ` ```suggestion ` blocks as a GitHub-style green diff preview (`-` old line, `+` suggested line).
- Show "Apply suggestion" button below the block (disabled when PR is merged/closed).

---

## Phase 7 — Auto-merge, Issue & PR Templates, Comment Reactions ✅ IMPLEMENTED

_Automation, workflow quality-of-life, and expressiveness in comment threads. Migrations 032–033._

### 7.1 Auto-merge

_Merge a PR automatically when all required status checks pass and the required number of reviews are approved._

**Migration** (`032_add_auto_merge_to_pulls.sql`):

```sql
ALTER TABLE pull_requests
    ADD COLUMN IF NOT EXISTS auto_merge_enabled   BOOLEAN NOT NULL DEFAULT FALSE,
    ADD COLUMN IF NOT EXISTS auto_merge_strategy  TEXT CHECK(auto_merge_strategy IN ('ff','merge','squash'));
```

**Service change (`internal/service/pull_service.go`):**

- `EnableAutoMerge(ctx, pullID, userID, strategy string) error` — sets `auto_merge_enabled = TRUE`, `auto_merge_strategy`; requires write access.
- `DisableAutoMerge(ctx, pullID, userID) error`.
- `TryAutoMerge(ctx, repo *model.Repository, pullID int64)` — called (as goroutine) whenever a review is submitted or a commit status changes; calls `CanMerge`; if true, dispatches the chosen merge strategy. Must be idempotent (check `auto_merge_enabled` and `state == "open"` first).

**Trigger points:**

- `pull_review_handler.go` `SubmitReview`: after saving review, call `go services.Pull.TryAutoMerge(...)`.
- `commit_status_handler.go` `CreateStatus`: after upserting status, call `go services.Pull.TryAutoMerge(...)` for any open PRs whose `head_sha` matches.

**Handler change:**

- `UpdatePull` PATCH: when `auto_merge=enable|disable` field present, call `EnableAutoMerge` or `DisableAutoMerge`.

**Template changes:**

- `pages/pull_detail.html`: "Enable auto-merge" dropdown button (select strategy, then confirm) shown when PR is open + not draft + no blocking reviews; shows "Auto-merge enabled" badge when active; "Disable" link to cancel.

**Viewmodel:** add `AutoMergeEnabled bool`, `AutoMergeStrategy string` to `PullDetailData`.

---

### 7.2 Issue & PR Templates

_Zero-migration — reads template files from the repository via `CodeService`._

**Convention (GitHub-compatible):**

- Single issue template: `.github/ISSUE_TEMPLATE.md` on default branch
- Multiple issue templates: `.github/ISSUE_TEMPLATE/<name>.md` (each file = one template option)
- PR template: `.github/PULL_REQUEST_TEMPLATE.md` on default branch

**`CodeService` additions:**

- `GetIssueTemplates(owner, repo, defaultBranch string) ([]IssueTemplate, error)` — reads `.github/ISSUE_TEMPLATE/` tree; falls back to single `.github/ISSUE_TEMPLATE.md`; returns `[]IssueTemplate{Name, Body}`.
- `GetPRTemplate(owner, repo, defaultBranch string) (string, error)` — reads `.github/PULL_REQUEST_TEMPLATE.md`; returns raw markdown or `""`.

**Handler changes:**

- `PageNewIssue` (new page handler): calls `GetIssueTemplates`; if multiple, renders template chooser; if single, pre-fills body textarea.
- `PageNewPull` (new page handler): calls `GetPRTemplate`; pre-fills body textarea with template.

**Routes:**

```
GET /{owner}/{repo}/issues/new    (optAuthMW)  page
GET /{owner}/{repo}/pulls/new     (optAuthMW)  page
```

**Templates:**

- `pages/issue_new.html` — template chooser (cards per template) or direct form pre-filled with template body.
- `pages/pull_new.html` — PR creation form (base/head branch selectors, title, body pre-filled from template).

**Page names to register:** `"issue_new"`, `"pull_new"`

---

### 7.3 Comment Reactions

_Emoji reactions on issue comments and PR comments (thumbs-up, heart, laugh, etc.)._

**Migration** (`033_create_reactions.sql`):

```sql
CREATE TABLE reactions (
    id         BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    user_id    BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    comment_id BIGINT NOT NULL REFERENCES comments(id) ON DELETE CASCADE,
    emoji      TEXT NOT NULL CHECK(emoji IN ('+1','-1','laugh','hooray','confused','heart','rocket','eyes')),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE(user_id, comment_id, emoji)
);
CREATE INDEX idx_reactions_comment ON reactions(comment_id);
```

**Files:**

- `internal/model/reaction.go` — `Reaction{ID, UserID, CommentID, Emoji, CreatedAt}`; `ReactionSummary{Emoji, Count, UserReacted bool}`
- `internal/store/reaction_store.go` — `Toggle(ctx, userID, commentID, emoji)` → `(added bool, error)` (INSERT ON CONFLICT DO DELETE pattern); `ListByComment(ctx, commentID, callerID)` → `[]ReactionSummary`
- `internal/service/reaction_service.go` — `Toggle`, `List`
- `internal/handler/reaction_handler.go` — `ToggleReaction` (POST/DELETE, HTMX-aware, swaps `fragment-reactions` into `#reactions-{commentID}`)

**Routes:**

```
POST   /api/repos/{owner}/{repo}/comments/{id}/reactions   (authMW)  — toggle reaction
```

**Templates:**

- `fragments/reactions.html` — `fragment-reactions` — row of emoji buttons with counts; highlighted when user reacted
- Extend `fragments/issue_comments.html` and PR comment fragments to include reactions row per comment

---

## Phase 8 — Two-Factor Auth (TOTP), Audit Log, LDAP / SAML SSO (8.1 ✅ IMPLEMENTED)

_Instance security hardening: strong authentication, accountability, and enterprise identity integration. 8.1 implemented (migration 034); 8.2–8.3 planned._

### 8.1 Two-Factor Authentication (TOTP)

_Time-based one-time password (RFC 6238) for login hardening; no external service required._

**Migration** (`034_add_2fa_to_users.sql`):

```sql
ALTER TABLE users
    ADD COLUMN IF NOT EXISTS totp_secret      TEXT,       -- encrypted AES-GCM with server secret
    ADD COLUMN IF NOT EXISTS totp_enabled     BOOLEAN NOT NULL DEFAULT FALSE,
    ADD COLUMN IF NOT EXISTS totp_backup_codes TEXT[];    -- hashed backup codes (bcrypt)
```

**TOTP library:** use `crypto/hmac` + `encoding/base32` from stdlib to implement RFC 6238 TOTP directly — no new dependency. Algorithm: `HOTP(secret, T)` where `T = floor(unix_ts / 30)`, HMAC-SHA1, 6-digit code.

**Files:**

- `internal/service/totp_service.go` — `Generate() (secret, otpAuthURL string, qrPNG []byte)` (QR encoded as base64 data URL using `image/png` + `encoding/base64`); `Verify(secret, code string) bool`; `Enable(ctx, userID, secret, code string) error` (verifies code before storing); `Disable(ctx, userID, code string) error`; `GenerateBackupCodes() ([]string, error)` (10 codes, 8 chars each, hashed with `bcrypt`)
- `internal/handler/totp_handler.go` — `PageTOTPSetup`, `EnableTOTP`, `DisableTOTP`, `VerifyTOTP` (second step of login)

**Login flow change (`internal/handler/auth_handler.go`):**

After successful password/OAuth verification, if `totp_enabled == true`:

1. Issue a short-lived (5 min) `cz_totp_pending` cookie containing the user ID (signed JWT).
2. Redirect to `/auth/2fa`.
3. `VerifyTOTP` handler validates the TOTP code, clears `cz_totp_pending`, issues normal `cz_token`.

**Routes:**

```
GET/POST /settings/security            (authMW)  — enable/disable 2FA page
GET      /auth/2fa                     — TOTP verification step during login
POST     /api/user/totp/enable         (authMW)
POST     /api/user/totp/disable        (authMW)
```

**Templates:**

- `pages/security.html` — 2FA setup: QR code image + manual secret + verification input; backup codes display (shown once after enable)
- `pages/totp_verify.html` — 6-digit code entry page shown mid-login

**Page names to register:** `"security"`, `"totp_verify"`

**Wire up:**

- No new store (columns on `users`); add `TOTP *TOTPService` to `services.go`
- `router.go`: register routes + page names

---

### 8.2 Audit Log

_Instance-wide immutable log of security-relevant actions (logins, permission changes, repo deletions, etc.)._

**Migration** (`035_create_audit_log.sql`):

```sql
CREATE TABLE audit_log (
    id          BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    actor_id    BIGINT REFERENCES users(id) ON DELETE SET NULL,
    actor_name  TEXT NOT NULL DEFAULT '',
    action      TEXT NOT NULL,
    target_type TEXT NOT NULL DEFAULT '',   -- 'user', 'repo', 'org', 'system'
    target_id   BIGINT,
    target_name TEXT NOT NULL DEFAULT '',
    ip_address  TEXT NOT NULL DEFAULT '',
    user_agent  TEXT NOT NULL DEFAULT '',
    metadata    JSONB NOT NULL DEFAULT '{}',
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX idx_audit_log_actor   ON audit_log(actor_id, created_at DESC);
CREATE INDEX idx_audit_log_created ON audit_log(created_at DESC);
```

**Actions logged:** `user.login`, `user.login_failed`, `user.logout`, `user.register`, `repo.create`, `repo.delete`, `repo.transfer`, `repo.archive`, `permission.add`, `permission.remove`, `org.create`, `org.transfer`, `invite.create`, `invite.accept`, `settings.update`, `admin.impersonate`

**Files:**

- `internal/model/audit_log.go` — `AuditEntry{ID, ActorID, ActorName, Action, TargetType, TargetID, TargetName, IPAddress, UserAgent, Metadata, CreatedAt}`
- `internal/store/audit_log_store.go` — `Create`, `List(ctx, filters AuditFilter, page, pageSize)`; `AuditFilter{ActorID, Action, TargetType, Since, Until}`
- `internal/service/audit_service.go` — `Record(ctx, r *http.Request, actorID int64, actorName, action, targetType string, targetID int64, targetName string, metadata map[string]any)` (fire-and-forget `go`)
- `internal/handler/audit_handler.go` — `PageAuditLog` (superadmin only)

**Routes:**

```
GET /admin/audit    (authMW + superadmin)   page
```

**Templates:**

- `pages/audit_log.html` — paginated table of audit entries; filter by action type and date range

**Page names to register:** `"audit_log"`

**Wire up:**

- `stores.go`: add `AuditLog *AuditLogStore`
- `services.go`: add `AuditLog *AuditLogService`
- `router.go`: register route + page name
- Call `go services.AuditLog.Record(...)` in: `Login`, `Register`, `GoogleOAuthCallback`, `CreateRepo`, `DeleteRepo`, `TransferRepo`, `AddCollaborator`, `RemoveCollaborator`, `UpdateSiteSettings`, `CreateInvitation`

---

### 8.3 LDAP / SAML SSO

_Enterprise identity provider integration allowing employees to sign in with corporate credentials._

**Migration** (`036_create_sso_config.sql`):

```sql
CREATE TABLE sso_configs (
    id           BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    provider     TEXT NOT NULL CHECK(provider IN ('ldap','saml')),
    config       JSONB NOT NULL DEFAULT '{}',   -- provider-specific settings
    enabled      BOOLEAN NOT NULL DEFAULT FALSE,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
ALTER TABLE users
    ADD COLUMN IF NOT EXISTS sso_provider TEXT,
    ADD COLUMN IF NOT EXISTS sso_id       TEXT,
    ADD CONSTRAINT uq_users_sso UNIQUE (sso_provider, sso_id);
```

**LDAP flow:** bind → search for user DN → verify password via secondary bind → upsert user (same account-linking priority as OAuth: `sso_id` match → email match → new user). Config keys: `host`, `port`, `bind_dn`, `bind_password`, `base_dn`, `user_filter`, `attr_username`, `attr_email`, `attr_display_name`, `use_tls`.

**SAML flow:** SP-initiated redirect → identity provider → ACS `POST /auth/saml/callback` → parse assertion → upsert user. Uses `encoding/xml` from stdlib for assertion parsing; SP metadata served at `GET /auth/saml/metadata`.

**Files:**

- `internal/service/sso_service.go` — `AuthenticateLDAP(ctx, username, password)`, `HandleSAMLCallback(ctx, samlResponse)`, `GetConfig(ctx, provider)`, `SetConfig(ctx, provider, config, enabled)`
- `internal/handler/sso_handler.go` — `PageSSOSettings` (superadmin), `SaveSSOConfig`, `InitiateSAML`, `SAMLCallback`, `SAMLMetadata`

**Routes:**

```
GET/POST /admin/sso              (authMW + superadmin)  — configure SSO
GET      /auth/saml              — initiate SAML redirect
POST     /auth/saml/callback     — SAML ACS endpoint
GET      /auth/saml/metadata     — SP metadata XML
POST     /auth/ldap/login        — LDAP credential login (JSON body)
```

**Templates:**

- Extend `pages/login.html` to show "Sign in with SSO" button when LDAP or SAML is enabled.
- `pages/sso_settings.html` — provider toggle + config form per provider (LDAP fields / SAML entity ID + IdP metadata URL).

**Page names to register:** `"sso_settings"`

**Wire up:**

- `stores.go`: add `SSOConfig *SSOConfigStore`
- `services.go`: add `SSO *SSOService`
- `router.go`: register routes + page name

---

## Phase 9 — Project Boards / Kanban, Wiki, Issue Pinning & Locking

_Project management tools, collaborative documentation, and issue triage controls._

### 9.1 Project Boards (Kanban)

**Migration** (`037_create_projects.sql`):

```sql
CREATE TABLE projects (
    id          BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    repo_id     BIGINT NOT NULL REFERENCES repositories(id) ON DELETE CASCADE,
    name        TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE TABLE project_columns (
    id         BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    project_id BIGINT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    name       TEXT NOT NULL,
    position   INTEGER NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE TABLE project_cards (
    id         BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    column_id  BIGINT NOT NULL REFERENCES project_columns(id) ON DELETE CASCADE,
    issue_id   BIGINT REFERENCES issues(id) ON DELETE CASCADE,
    pull_id    BIGINT REFERENCES pull_requests(id) ON DELETE CASCADE,
    note       TEXT NOT NULL DEFAULT '',
    position   INTEGER NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CHECK (
        (issue_id IS NOT NULL AND pull_id IS NULL AND note = '')
        OR (pull_id IS NOT NULL AND issue_id IS NULL AND note = '')
        OR (issue_id IS NULL AND pull_id IS NULL AND note <> '')
    )
);
CREATE INDEX idx_project_cards_column ON project_cards(column_id, position);
CREATE INDEX idx_project_cards_issue  ON project_cards(issue_id);
CREATE INDEX idx_project_cards_pull   ON project_cards(pull_id);
```

**Files:**

- `internal/model/project.go` — `Project`, `ProjectColumn`, `ProjectCard` structs; `ProjectCard` has helper fields `IssueTitle`, `IssueState`, `PullTitle`, `PullState` (populated via JOIN in store)
- `internal/store/project_store.go` — `CreateProject`, `ListByRepo`, `GetProject`, `DeleteProject`; `CreateColumn`, `ListColumns`, `UpdateColumnPosition`, `DeleteColumn`; `CreateCard`, `ListCardsByColumn`, `MoveCard(ctx, cardID, newColumnID, newPosition)`, `DeleteCard`
- `internal/service/project_service.go`
- `internal/handler/project_handler.go` — `PageProjects`, `PageProjectDetail`, `CreateProject`, `DeleteProject`, `CreateColumn`, `DeleteColumn`, `CreateCard`, `MoveCard`, `DeleteCard`; HTMX-aware card move uses `hx-patch` with `column_id` + `position`

**Routes:**

```
GET        /{owner}/{repo}/projects                           page
GET        /{owner}/{repo}/projects/{id}                     page
GET/POST   /api/repos/{owner}/{repo}/projects                (POST: authMW)
DELETE     /api/repos/{owner}/{repo}/projects/{id}           (authMW)
POST       /api/repos/{owner}/{repo}/projects/{id}/columns   (authMW)
DELETE     /api/repos/{owner}/{repo}/projects/{id}/columns/{colID}  (authMW)
POST       /api/repos/{owner}/{repo}/projects/{id}/cards     (authMW)
PATCH      /api/repos/{owner}/{repo}/projects/{id}/cards/{cardID}   (authMW) — move
DELETE     /api/repos/{owner}/{repo}/projects/{id}/cards/{cardID}   (authMW)
```

**Templates:**

- `pages/projects.html` — list of boards for the repo
- `pages/project_detail.html` — Kanban board: columns side by side, cards per column; drag-and-drop via HTMX + `hx-on:dragend` firing a `PATCH` to move card; no extra JS lib needed (native HTML5 drag events + HTMX `hx-trigger="dragend"`)

**Page names to register:** `"projects"`, `"project_detail"`

**Wire up:**

- `stores.go`: add `Project *ProjectStore`
- `services.go`: add `Project *ProjectService`
- `router.go`: register all project routes + page names

---

### 9.2 Wiki

_Per-repository wiki backed by a bare git repo on disk._

**No migration needed.** Wiki content lives in a separate bare git repo at `<ReposRoot>/<owner>/<repo>.wiki.git`. Created on first wiki page save.

**`CodeService` additions:**

- `WikiPageList(owner, repo string)` → `([]string, error)` — reads root tree of default branch, returns `.md` filenames (stripped of extension)
- `WikiPageGet(owner, repo, slug string)` → `(string, bool, error)` — reads `<slug>.md` blob, returns raw markdown and found=true; returns `("", false, nil)` if absent
- `WikiPageSave(owner, repo, slug, content, authorName, authorEmail, message string)` → `error` — creates or updates `<slug>.md` in wiki repo; commits directly to `main` branch (creates branch if first commit)
- `WikiPageDelete(owner, repo, slug string)` → `error`

Wiki repo path helper: `filepath.Join(cfg.ReposRoot, owner, repo+".wiki.git")`. Init with `go-git PlainInit(path, true)` on first save.

**Handler:** `internal/handler/wiki_handler.go` — `PageWikiHome` (redirects to `Home` page or shows page list), `PageWikiPage`, `PageWikiEdit`, `CreateOrUpdateWikiPage`, `DeleteWikiPage`

**Routes:**

```
GET    /{owner}/{repo}/wiki                      (redirects to /{owner}/{repo}/wiki/Home)
GET    /{owner}/{repo}/wiki/{slug}               page  (optAuthMW)
GET    /{owner}/{repo}/wiki/{slug}/edit          page  (authMW + write access)
POST   /api/repos/{owner}/{repo}/wiki/{slug}     (authMW + write access) — create/update
DELETE /api/repos/{owner}/{repo}/wiki/{slug}     (authMW + write access)
```

**Templates:**

- `pages/wiki_page.html` — rendered markdown + Edit button (if write access) + sidebar page list
- `pages/wiki_edit.html` — textarea editor + commit message field + Save/Cancel buttons

**Page names to register:** `"wiki_page"`, `"wiki_edit"`

---

### 9.3 Issue Pinning & Locking

_Pin up to 3 issues to the top of the issue list; lock issues to prevent further comments._

**Migration** (`038_add_pin_lock_to_issues.sql`):

```sql
ALTER TABLE issues
    ADD COLUMN IF NOT EXISTS is_pinned   BOOLEAN NOT NULL DEFAULT FALSE,
    ADD COLUMN IF NOT EXISTS is_locked   BOOLEAN NOT NULL DEFAULT FALSE,
    ADD COLUMN IF NOT EXISTS locked_at   TIMESTAMPTZ;
CREATE INDEX idx_issues_pinned ON issues(repo_id, is_pinned) WHERE is_pinned = TRUE;
```

**Service changes (`internal/service/issue_service.go`):**

- `PinIssue(ctx, issueID, repoID, userID) error` — CanManage required; checks that fewer than 3 issues are already pinned in the repo; sets `is_pinned = TRUE`.
- `UnpinIssue(ctx, issueID, repoID, userID) error`.
- `LockIssue(ctx, issueID, repoID, userID) error` — CanManage required; sets `is_locked = TRUE`, `locked_at = NOW()`.
- `UnlockIssue(ctx, issueID, repoID, userID) error`.
- `CreateIssueComment`: check `issue.IsLocked`; if locked and caller is not CanManage, return `ErrIssueLocked`.

**Routes:**

```
PATCH /api/repos/{owner}/{repo}/issues/{number}/pin    (authMW + CanManage)
PATCH /api/repos/{owner}/{repo}/issues/{number}/lock   (authMW + CanManage)
```

**Template changes:**

- `pages/issues.html`: pinned issues rendered in a separate "Pinned" section at the top of the list.
- `pages/issue_detail.html`: show "Locked" banner; hide comment form for non-maintainers when locked; show "Unpin" / "Unlock" buttons in the issue sidebar for maintainers.

---

## Phase 10 — Repository Insights & Stats, @Mentions in Comments, Saved Replies

_Visibility into repository activity, better comment discoverability, and productivity shortcuts._

### 10.1 Repository Insights & Stats

_Zero-migration — derives data from git history and existing tables._

**`CodeService` additions:**

- `GetContributors(owner, repo string)` → `([]ContributorStat, error)` — walks all commits, aggregates `{Name, Email, Commits, Additions, Deletions}` per author; top 100 by commit count
- `GetCommitActivity(owner, repo string)` → `([]WeeklyActivity, error)` — returns last 52 weeks of `{Week (Unix ts), Total, Days [7]int}` commit counts
- `GetCodeFrequency(owner, repo string)` → `([]CodeFrequencyWeek, error)` — weekly `{Week, Additions, Deletions}` for last 52 weeks (walks diffs)

**Handler:** `internal/handler/insights_handler.go` — `PageInsights`, `PageContributors`, `PagePulse`

**Routes:**

```
GET /{owner}/{repo}/pulse                        page
GET /{owner}/{repo}/graphs/contributors          page
```

**Templates:**

- `pages/pulse.html` — summary: new issues, closed issues, new PRs, merged PRs, open PRs, commits in last 30 days; contributor avatars; no charts (prose + counts)
- `pages/contributors.html` — table of contributors sorted by commit count with bars rendered as inline `<div>` width percentage (pure CSS, no JS)

**Page names to register:** `"pulse"`, `"contributors"`

**Wire up:** no store/service wiring (pure `CodeService` + existing stores); register routes + page names in `router.go`

---

### 10.2 @Mentions in Comments

_Notify users when they are mentioned by `@username` in issue or PR comments._

**Migration** (`039_create_mentions.sql`):

```sql
CREATE TABLE mentions (
    id         BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    comment_id BIGINT NOT NULL REFERENCES comments(id) ON DELETE CASCADE,
    user_id    BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE(comment_id, user_id)
);
CREATE INDEX idx_mentions_user ON mentions(user_id);
```

**Parse logic (`internal/service/comment_service.go`):**

After saving a comment body, scan for `@username` tokens using a simple regex (`@[A-Za-z0-9_-]+`). For each unique match, look up the user by username; if found and user differs from author, insert a `mentions` row and call `NotificationService.NotifyMention(ctx, repo, issue/pr, actorID, actorName, mentionedUserID)`.

**Notification type:** add `NotifMention` const to `model/notification.go`; add `NotifyMention` to `notification_service.go`.

**Template changes:**

- Render `@username` tokens in comment bodies as links to `/{username}` (via a `renderMentions` template helper or server-side text transformation before passing to template).

**No new store or service files required** — mention lookup and notification are inline in the existing comment create path.

---

### 10.3 Saved Replies

_Reusable canned responses that a user can insert into any comment form with one click._

**Migration** (`040_create_saved_replies.sql`):

```sql
CREATE TABLE saved_replies (
    id         BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    user_id    BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    title      TEXT NOT NULL,
    body       TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX idx_saved_replies_user ON saved_replies(user_id);
```

**Files:**

- `internal/model/saved_reply.go` — `SavedReply{ID, UserID, Title, Body, CreatedAt, UpdatedAt}`
- `internal/store/saved_reply_store.go` — `Create`, `ListByUser`, `GetByID`, `Update`, `Delete`
- `internal/service/saved_reply_service.go` — `Create`, `List`, `Update`, `Delete`
- `internal/handler/saved_reply_handler.go` — `PageSavedReplies`, `CreateSavedReply`, `UpdateSavedReply`, `DeleteSavedReply`, `ListSavedRepliesFragment`

**Routes:**

```
GET    /settings/replies                 (authMW)   page — manage saved replies
POST   /api/user/replies                 (authMW)   — create
PATCH  /api/user/replies/{id}            (authMW)   — update
DELETE /api/user/replies/{id}            (authMW)   — delete
GET    /api/user/replies                 (authMW)   — list (HTMX fragment for comment picker)
```

**Templates:**

- `pages/saved_replies.html` — list + inline edit form; HTMX swaps `fragment-saved-replies` into `#saved-replies`
- `fragments/saved_replies.html` — `fragment-saved-replies`
- Extend comment textareas with a small "Saved replies" picker button (`hx-get` to load `fragment-saved-replies` into a dropdown, then `hx-on:click` fills textarea)

**Page names to register:** `"saved_replies"`

**Wire up:**

- `stores.go`: add `SavedReply *SavedReplyStore`
- `services.go`: add `SavedReply *SavedReplyService`
- `router.go`: register routes + page name

---

## Phase 11 — Email Notifications, OAuth Apps / Third-party Clients, Webhook Improvements

_Outbound alerting, programmatic third-party integrations, and more reliable webhook delivery._

### 11.1 Email Notifications (SMTP)

_Send email digests and immediate alerts for in-app notifications; no new Go deps — uses `net/smtp` from stdlib._

**Migration** (`041_add_email_prefs.sql`):

```sql
ALTER TABLE users
    ADD COLUMN IF NOT EXISTS email_notifications BOOLEAN NOT NULL DEFAULT TRUE,
    ADD COLUMN IF NOT EXISTS email_digest        TEXT NOT NULL DEFAULT 'immediate'
        CHECK(email_digest IN ('immediate','daily','weekly','never'));
```

**Config addition (`internal/config/config.go`):**

```go
type SMTPConfig struct {
    Host     string
    Port     int
    Username string
    Password string
    From     string
    TLS      bool
}
```

Viper keys: `smtp.host`, `smtp.port`, `smtp.username`, `smtp.password`, `smtp.from`, `smtp.tls`. When `smtp.host` is empty, email sending is silently skipped.

**Files:**

- `internal/service/email_service.go` — `Send(to, subject, htmlBody string) error` (uses `net/smtp`; builds `MIME` multipart message with plain-text fallback); `SendNotification(ctx, userID int64, notif *model.Notification)` — checks user's `email_notifications` preference before sending; renders subject/body from notification type
- `internal/handler/settings_handler.go` — `PageEmailSettings`, `UpdateEmailSettings`

**Integration:** Extend each `NotificationService.NotifyX` method to call `go services.Email.SendNotification(...)` after creating the DB notification record.

**Daily/weekly digest:** `CronService` (or a goroutine started in `cmd/server/main.go`) runs at 08:00 UTC; calls `NotificationService.ListUnread` for each user with `email_digest=daily|weekly`; batches into one email; marks notifications as emailed.

**Routes:**

```
GET/POST /settings/notifications    (authMW)   — email preference page
```

**Templates:**

- `pages/notification_settings.html` — toggle "Email notifications" + digest frequency radio; HTMX form POST

**Page names to register:** `"notification_settings"`

---

### 11.2 OAuth Apps / Third-party Clients ✅ IMPLEMENTED

_Allow third-party applications to request access to user accounts via a standard OAuth 2.0 authorization code flow._

**Migration** (`042_create_oauth_apps.sql`):

```sql
CREATE TABLE oauth_apps (
    id            BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    owner_id      BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    name          TEXT NOT NULL,
    client_id     TEXT NOT NULL UNIQUE,     -- random 20-char hex
    client_secret TEXT NOT NULL,            -- bcrypt-hashed; shown once on create
    redirect_uris TEXT[] NOT NULL DEFAULT '{}',
    homepage_url  TEXT NOT NULL DEFAULT '',
    description   TEXT NOT NULL DEFAULT '',
    created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE TABLE oauth_authorizations (
    id          BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    app_id      BIGINT NOT NULL REFERENCES oauth_apps(id) ON DELETE CASCADE,
    user_id     BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    code        TEXT UNIQUE,                -- authorization code (5-min TTL); NULL after exchange
    token_hash  TEXT UNIQUE,               -- SHA-256 of bearer token; NULL until exchanged
    scopes      TEXT[] NOT NULL DEFAULT '{}',
    code_expires_at TIMESTAMPTZ,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE(app_id, user_id)
);
```

**OAuth 2.0 flow:**

1. Third-party app redirects user to `GET /oauth/authorize?client_id=...&redirect_uri=...&scope=...&state=...`
2. User approves on the authorization page
3. Cloudzilla redirects to `redirect_uri?code=...&state=...`
4. App exchanges code for token: `POST /oauth/token` (client credentials + code)
5. App uses `Authorization: Bearer <token>` — auth middleware validates via `oauth_authorizations` table

**Files:**

- `internal/model/oauth_app.go` — `OAuthApp`, `OAuthAuthorization`
- `internal/store/oauth_app_store.go` — `CreateApp`, `GetByClientID`, `ListByOwner`, `DeleteApp`; `CreateAuthorization`, `ExchangeCode`, `GetByToken`, `RevokeAuthorization`
- `internal/service/oauth_app_service.go` — `CreateApp`, `Authorize(ctx, appID, userID, scopes)`, `ExchangeCode(ctx, clientID, secret, code)`, `RevokeAccess`
- `internal/handler/oauth_handler.go` — `PageAuthorize`, `ConfirmAuthorize`, `TokenEndpoint`, `PageOAuthApps`, `CreateApp`, `DeleteApp`

**Routes:**

```
GET    /oauth/authorize          (optAuthMW)   — authorization consent page
POST   /oauth/authorize          (authMW)      — user approves/denies
POST   /oauth/token              —             — code → token exchange (no auth required; client credentials in body)
GET    /settings/oauth-apps      (authMW)      — manage registered apps
POST   /api/oauth/apps           (authMW)      — create app
DELETE /api/oauth/apps/{id}      (authMW)      — delete app
DELETE /api/oauth/authorizations/{id} (authMW) — revoke a granted authorization
```

**Page names to register:** `"oauth_authorize"`, `"oauth_apps"`

---

### 11.3 Webhook Improvements (retry, filter)

_Make webhooks more reliable by adding automatic retry with exponential backoff and per-event filtering._

**No migration.** Uses existing `webhook_deliveries` table; adds `next_retry_at` and `attempt_count` columns.

**Migration** (`—` — no new table; alter `webhook_deliveries`):

```sql
ALTER TABLE webhook_deliveries
    ADD COLUMN IF NOT EXISTS attempt_count INTEGER NOT NULL DEFAULT 1,
    ADD COLUMN IF NOT EXISTS next_retry_at TIMESTAMPTZ;
```

**Retry logic (`internal/service/webhook_service.go`):**

- `Dispatch`: on non-2xx response or network error, set `next_retry_at = NOW() + backoff(attempt_count)` (backoff: 1 min, 5 min, 30 min, 2 h, 8 h — capped at 5 retries).
- Add `RetryPending(ctx)` — queries `webhook_deliveries WHERE next_retry_at <= NOW() AND attempt_count <= 5`; re-dispatches each; called from a background goroutine every 60 seconds.

**Per-event filtering:** `webhooks.events` column already exists as `TEXT[]`. The `Dispatch` method already checks event membership. Add UI to edit event subscriptions for existing hooks.

**New routes:**

```
PATCH /api/repos/{owner}/{repo}/hooks/{id}    (authMW + write)   — update events list
POST  /api/repos/{owner}/{repo}/hooks/{id}/redeliver (authMW + write) — manually redeliver a delivery
```

**Template change (`pages/repo_settings.html`):** Show retry status in delivery history; add "Redeliver" button per delivery; add event filter checkboxes to the webhook edit form.

---

## Phase 12 — Watching, Activity Feed, Discussions

_Staying informed, personalized discovery, and threaded community forums._

### Phase 12.1 — Watching

**Migration** (`043_create_watches.sql`):

```sql
CREATE TABLE watches (
    id         BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    user_id    BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    repo_id    BIGINT NOT NULL REFERENCES repositories(id) ON DELETE CASCADE,
    level      TEXT NOT NULL DEFAULT 'watching' CHECK(level IN ('watching','releases_only','ignoring')),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE(user_id, repo_id)
);
CREATE INDEX idx_watches_repo ON watches(repo_id);
CREATE INDEX idx_watches_user ON watches(user_id);
```

Users can watch a repo at three levels: `watching` (all events), `releases_only` (new releases only), or `ignoring` (suppress all notifications). The watch button appears next to the star button on repo pages. `NotificationService.NotifyX` methods fan out notifications to all watchers at the appropriate level. Watch/unwatch via `PUT/DELETE /api/repos/{owner}/{repo}/watch` (authMW); HTMX swaps `fragment-watch-button`.

---

### Phase 12.2 — Activity Feed

**Migration** (`044_create_events.sql`):

```sql
CREATE TABLE events (
    id         BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    actor_id   BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    actor_name TEXT NOT NULL DEFAULT '',
    repo_id    BIGINT REFERENCES repositories(id) ON DELETE CASCADE,
    repo_name  TEXT NOT NULL DEFAULT '',
    owner_name TEXT NOT NULL DEFAULT '',
    event_type TEXT NOT NULL,
    payload    JSONB NOT NULL DEFAULT '{}',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX idx_events_actor ON events(actor_id, created_at DESC);
CREATE INDEX idx_events_repo  ON events(repo_id,  created_at DESC);
```

Event types: `push`, `issue_opened`, `issue_closed`, `pr_opened`, `pr_merged`, `pr_closed`, `fork`, `star`, `release_published`, `member_added`. `EventService.Record(...)` is called fire-and-forget (`go`) at each action point. `GET /feed` (authMW) renders a personal timeline of events from repos the user watches or owns. Public per-user activity is shown on the profile page. When a logged-in user hits `/`, redirect to `/feed`; unauthenticated users see the current landing page.

---

### Phase 12.3 — Discussions

_Threaded forum separate from the issue tracker — for Q&A, RFCs, and announcements._

**Migration** (`045_create_discussions.sql`):

```sql
CREATE TABLE discussion_categories (
    id      BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    repo_id BIGINT NOT NULL REFERENCES repositories(id) ON DELETE CASCADE,
    name    TEXT NOT NULL,
    emoji   TEXT NOT NULL DEFAULT '',
    UNIQUE(repo_id, name)
);
CREATE TABLE discussions (
    id           BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    repo_id      BIGINT NOT NULL REFERENCES repositories(id) ON DELETE CASCADE,
    category_id  BIGINT NOT NULL REFERENCES discussion_categories(id) ON DELETE CASCADE,
    number       INTEGER NOT NULL,
    title        TEXT NOT NULL,
    body         TEXT NOT NULL DEFAULT '',
    author_id    BIGINT NOT NULL REFERENCES users(id),
    author_name  TEXT NOT NULL DEFAULT '',
    is_locked    BOOLEAN NOT NULL DEFAULT FALSE,
    is_answered  BOOLEAN NOT NULL DEFAULT FALSE,
    answer_id    BIGINT,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE(repo_id, number)
);
CREATE TABLE discussion_replies (
    id            BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    discussion_id BIGINT NOT NULL REFERENCES discussions(id) ON DELETE CASCADE,
    parent_id     BIGINT REFERENCES discussion_replies(id) ON DELETE CASCADE,
    author_id     BIGINT NOT NULL REFERENCES users(id),
    author_name   TEXT NOT NULL DEFAULT '',
    body          TEXT NOT NULL,
    is_answer     BOOLEAN NOT NULL DEFAULT FALSE,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
ALTER TABLE discussions ADD CONSTRAINT fk_answer FOREIGN KEY (answer_id) REFERENCES discussion_replies(id) ON DELETE SET NULL DEFERRABLE INITIALLY DEFERRED;
CREATE INDEX idx_discussions_repo ON discussions(repo_id, created_at DESC);
CREATE INDEX idx_discussion_replies_discussion ON discussion_replies(discussion_id);
```

Discussions provide a categorised threaded Q&A forum per repo. Maintainers can mark a reply as the accepted answer. Notification type `NotifDiscussionReply` fires when someone replies to your discussion. Routes follow the pattern `/{owner}/{repo}/discussions` and `/api/repos/{owner}/{repo}/discussions/...`; reply submission is HTMX-aware.

---

## Phase 13 — Gists, Profile README, Repository Topics / Tags

_Standalone code snippets, profile customisation, and repo discoverability._

### Phase 13.1 — Gists

**Migration** (`046_create_gists.sql`):

```sql
CREATE TABLE gists (
    id          TEXT PRIMARY KEY,   -- 32-char hex (crypto/rand)
    owner_id    BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    owner_name  TEXT NOT NULL DEFAULT '',
    description TEXT NOT NULL DEFAULT '',
    public      BOOLEAN NOT NULL DEFAULT TRUE,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE TABLE gist_files (
    id       BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    gist_id  TEXT NOT NULL REFERENCES gists(id) ON DELETE CASCADE,
    filename TEXT NOT NULL,
    content  TEXT NOT NULL DEFAULT '',
    UNIQUE(gist_id, filename)
);
CREATE INDEX idx_gists_owner ON gists(owner_id, created_at DESC);
```

Gist content is stored in DB (not git-backed), max 10 files per gist, 1 MB per file. Routes: `GET /gists` (explore), `GET/POST /gists/new`, `GET /gists/{id}`, `GET /gists/{id}/edit`, `GET /{owner}/gists`, and API endpoints. A "Add file" HTMX button fetches `fragment-gist-file-row` to add filename+textarea pairs dynamically.

---

### Phase 13.2 — Profile README

_Zero-migration — reads from an existing special-named public repo._

If a user named `alice` owns a public repo named `alice` with a `README.md` at the root of the default branch, that file is rendered as Markdown HTML on the `/{alice}` profile page. `CodeService.GetProfileReadme(ownerName, repoName, defaultBranch)` returns pre-rendered HTML or empty string. `PageUser` calls this and sets `UserData.ProfileReadme`. Template renders it above the repo list when non-empty.

---

### Phase 13.3 — Repository Topics / Tags

**Migration** (`047_create_repo_topics.sql`):

```sql
CREATE TABLE topics (
    id   BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    name TEXT NOT NULL UNIQUE
);
CREATE TABLE repo_topics (
    repo_id  BIGINT NOT NULL REFERENCES repositories(id) ON DELETE CASCADE,
    topic_id BIGINT NOT NULL REFERENCES topics(id) ON DELETE CASCADE,
    PRIMARY KEY (repo_id, topic_id)
);
CREATE INDEX idx_repo_topics_topic ON repo_topics(topic_id);
```

Repo owners can tag their repositories with short lowercase topic strings (e.g. `go`, `web`, `devops`). Topics are displayed as clickable pills on repo pages and link to a `GET /topic/{name}` explore page listing all public repos with that topic. API: `PUT /api/repos/{owner}/{repo}/topics` (JSON array of strings, authMW + CanManage). HTMX swaps `fragment-repo-topics` into `#repo-topics`.

---

## Phase 14 — Private Issues, Archive & Templates, Repository Soft-delete & Recovery

_Confidential triage, repo lifecycle management, and a safety net for accidental deletions._

### Phase 14.1 — Private Issues

**Migration** (`048_add_visibility_to_issues.sql`):

```sql
ALTER TABLE issues
    ADD COLUMN IF NOT EXISTS visibility TEXT NOT NULL DEFAULT 'public'
        CHECK(visibility IN ('public','private'));
CREATE INDEX idx_issues_visibility ON issues(repo_id, visibility);
```

Private issues are visible only to the repo owner, collaborators with `writer`/`admin` role, and the issue author. All `ListByRepo` and `GetByNumber` store queries gain a `visibleToUserID *int64` parameter; when the caller lacks access the row is treated as not found. A "Private issue" checkbox appears on the new-issue form when the caller has write access. Private issues show a lock badge in lists and a "Private" header badge on detail pages.

---

### Phase 14.2 — Archive & Templates

**Migration** (`049_add_archive_template_to_repos.sql`):

```sql
ALTER TABLE repositories
    ADD COLUMN IF NOT EXISTS is_archived BOOLEAN NOT NULL DEFAULT FALSE,
    ADD COLUMN IF NOT EXISTS archived_at TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS is_template BOOLEAN NOT NULL DEFAULT FALSE;
CREATE INDEX idx_repos_template ON repositories(is_template) WHERE is_template = TRUE;
```

**Archive:** `RepoService.Archive/Unarchive` (CanManage). Archived repos block all pushes with "repository is archived". UI shows a yellow banner and hides write-action buttons. **Templates:** `RepoService.SetTemplate` (CanManage) marks a repo as a template. `CreateFromTemplate` copies the bare git directory and inserts a new `repositories` row. `PageNewRepo` shows a template selector dropdown.

---

### Phase 14.3 — Repository Soft-delete & Recovery

**Migration** (`050_add_soft_delete_to_repos.sql`):

```sql
ALTER TABLE repositories
    ADD COLUMN IF NOT EXISTS deleted_at   TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS deleted_by   BIGINT REFERENCES users(id);
CREATE INDEX idx_repos_deleted ON repositories(deleted_at) WHERE deleted_at IS NOT NULL;
```

Instead of a hard `DELETE`, `RepoService.Delete` sets `deleted_at = NOW()` and renames the on-disk bare repo to `<name>.deleted.<unix_ts>.git`. A 30-day recovery window allows superadmins or the original owner to restore: `POST /api/repos/{owner}/{repo}/restore` renames the directory back and clears `deleted_at`. After 30 days a background cleanup goroutine permanently removes soft-deleted repos. All `ListByOwner` and repo lookup queries add `WHERE deleted_at IS NULL`.

---

## Phase 15 — Advanced Code Search, Explore / Trending, Dependency Graph

_Deep code discovery across the instance, surfacing popular repos, and understanding project dependencies._

### Phase 15.1 — Advanced Code Search

**Migration** (`051_create_code_search_index.sql`):

```sql
CREATE TABLE code_search_index (
    id         BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    repo_id    BIGINT NOT NULL REFERENCES repositories(id) ON DELETE CASCADE,
    ref        TEXT NOT NULL DEFAULT 'HEAD',
    file_path  TEXT NOT NULL,
    content    TEXT NOT NULL,
    tsv        TSVECTOR GENERATED ALWAYS AS (to_tsvector('simple', content)) STORED,
    indexed_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE(repo_id, file_path)
);
CREATE INDEX idx_code_search_tsv ON code_search_index USING GIN(tsv);
```

An `IndexService.IndexRepo(ctx, repo)` method walks all blobs on the default branch and upserts rows. Indexing is triggered fire-and-forget after every push. `GET /search/code?q=...&repo=...&lang=...` returns paginated results using PostgreSQL full-text search. Complements the existing Phase 4.3 keyword search which covers issues/PRs/repos only.

---

### Phase 15.2 — Explore / Trending

_Zero-migration — aggregates from existing tables._

`ExploreStore.TrendingRepos(ctx, since, limit)` counts stars gained since `since` per public repo. `GET /explore?tab=trending|newest|forked&period=daily|weekly|monthly` (optAuthMW) renders tab bar with repo cards showing star count, fork count, and language. Period selector pills control the trending window. No new tables needed.

---

### Phase 15.3 — Dependency Graph

**Migration** (`052_create_dependency_graph.sql`):

```sql
CREATE TABLE repo_dependencies (
    id          BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    repo_id     BIGINT NOT NULL REFERENCES repositories(id) ON DELETE CASCADE,
    package_mgr TEXT NOT NULL,   -- 'go', 'npm', 'pip', 'cargo', etc.
    package     TEXT NOT NULL,
    version     TEXT NOT NULL DEFAULT '',
    is_dev      BOOLEAN NOT NULL DEFAULT FALSE,
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE(repo_id, package_mgr, package)
);
```

After each push, a `DependencyService.ParseAndStore(ctx, repo)` goroutine reads manifest files (`go.mod`, `package.json`, `requirements.txt`, `Cargo.toml`) from the default branch tip and upserts rows. `GET /{owner}/{repo}/network/dependencies` renders a table of parsed dependencies. No external package-registry API calls needed; purely local parsing.

---

## Phase 16 — Container Registry, Generic Package Registry, Release Asset Enhancements

_First-class artifact hosting alongside source code._

### Phase 16.1 — Container Registry (Docker)

**Migration** (`053_create_container_registry.sql`):

```sql
CREATE TABLE container_images (
    id         BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    repo_id    BIGINT NOT NULL REFERENCES repositories(id) ON DELETE CASCADE,
    name       TEXT NOT NULL,   -- image name (e.g. 'myapp')
    tag        TEXT NOT NULL,
    digest     TEXT NOT NULL,   -- sha256 manifest digest
    size_bytes BIGINT NOT NULL DEFAULT 0,
    media_type TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE(repo_id, name, tag)
);
```

Implements the OCI Distribution Specification v1.1 API (`/v2/...`) for push and pull. Blob storage is on-disk under `<ReposRoot>/../registry/blobs/sha256/`. Authentication uses the existing JWT/PAT bearer tokens. `GET /{owner}/{repo}/packages/container` lists images and tags. Container images pushed via `docker push` are stored and served without any external registry.

---

### Phase 16.2 — Generic Package Registry

**Migration** (`054_create_packages.sql`):

```sql
CREATE TABLE packages (
    id          BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    repo_id     BIGINT NOT NULL REFERENCES repositories(id) ON DELETE CASCADE,
    type        TEXT NOT NULL,   -- 'generic', 'npm', 'pypi', 'maven'
    name        TEXT NOT NULL,
    version     TEXT NOT NULL,
    filename    TEXT NOT NULL,
    size_bytes  BIGINT NOT NULL DEFAULT 0,
    checksum    TEXT NOT NULL DEFAULT '',
    created_by  BIGINT REFERENCES users(id),
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE(repo_id, type, name, version)
);
```

Supports generic file uploads for any package type. Provides upload (`PUT /api/packages/{owner}/{repo}/{name}/{version}/{filename}`) and download (`GET /packages/{owner}/{repo}/{name}/{version}/{filename}`) endpoints. Package files stored on disk under `<ReposRoot>/../packages/`. `GET /{owner}/{repo}/packages` lists all packages. Future sub-phases can add type-specific protocol support (npm registry proxy, PyPI simple API, etc.).

---

### Phase 16.3 — Release Asset Enhancements

_Zero-migration for core enhancements; extends the existing `release_assets` table._

Adds checksum verification (`sha256` column on `release_assets`), download count tracking (`download_count BIGINT DEFAULT 0`), and a direct `GET /{owner}/{repo}/releases/latest` redirect. Release pages show total download counts per asset. Asset deletion via `DELETE /api/repos/{owner}/{repo}/releases/{id}/assets/{assetID}` already in Phase 3.1 routes; this phase adds the checksum + download-count columns and updates the upload handler to compute SHA-256 at upload time.

---

## Phase 17 — Git LFS Support, Signed Commit Verification, Secret Scanning

_Large file storage, cryptographic commit trust, and security hygiene._

### Phase 17.1 — Git LFS Support

**Migration** (`055_create_lfs_objects.sql`):

```sql
CREATE TABLE lfs_objects (
    id         BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    repo_id    BIGINT NOT NULL REFERENCES repositories(id) ON DELETE CASCADE,
    oid        TEXT NOT NULL,   -- sha256 hex
    size       BIGINT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE(repo_id, oid)
);
```

Implements the Git LFS Batch API (`POST /{owner}/{repo}.git/info/lfs/objects/batch`) and transfer endpoints (`GET/PUT /{owner}/{repo}.git/lfs/objects/{oid}`). LFS objects stored on disk under `<ReposRoot>/../lfs/`. Auth follows the same Basic/JWT rules as git HTTP. Standard `git lfs` clients work without modification. No new Go dependencies — uses stdlib `crypto/sha256` for OID verification.

---

### Phase 17.2 — Signed Commit Verification (GPG/SSH)

_Zero-migration for display; one column added to cache verification results._

```sql
ALTER TABLE repositories
    ADD COLUMN IF NOT EXISTS verify_commits BOOLEAN NOT NULL DEFAULT FALSE;
```

On commit display, the server attempts to verify the commit signature using the committer's stored SSH keys (already in `ssh_keys` table) or a stored GPG public key (`gpg_keys` table added in a companion migration). Verified commits show a green "Verified" badge on tree, blame, and commit pages. `CodeService.GetCommit` is extended to return `SignatureStatus` (`verified`, `unverified`, `unsigned`). GPG key management mirrors SSH key management via `GET/POST/DELETE /api/user/gpg_keys`.

---

### Phase 17.3 — Secret Scanning

**Migration** (`056_create_secret_scan_alerts.sql`):

```sql
CREATE TABLE secret_scan_alerts (
    id          BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    repo_id     BIGINT NOT NULL REFERENCES repositories(id) ON DELETE CASCADE,
    commit_sha  TEXT NOT NULL,
    file_path   TEXT NOT NULL,
    line_number INTEGER NOT NULL,
    secret_type TEXT NOT NULL,   -- 'aws_key', 'github_token', 'private_key', etc.
    secret_hint TEXT NOT NULL,   -- first/last 4 chars for display, never full value
    state       TEXT NOT NULL DEFAULT 'open' CHECK(state IN ('open','resolved','false_positive')),
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX idx_secret_alerts_repo ON secret_scan_alerts(repo_id, state);
```

After each push, `SecretScanService.ScanCommit(ctx, repo, commit)` walks changed blobs and applies a set of regexp patterns for common secret types (AWS keys, private keys, JWT secrets, generic API key patterns). Findings are stored as alerts and surfaced in a `GET /{owner}/{repo}/security/secrets` page (write access required). Repo owners receive a notification for each new finding. Patterns are defined as a static slice in the service — no external database needed.

---

## Phase 18 — CI/CD Pipeline Runner, Pipeline YAML Config & UI, CI Status Dashboard

_First-class built-in continuous integration without needing an external CI service._

### Phase 18.1 — CI/CD Pipeline Runner (basic)

**Migration** (`057_create_pipelines.sql`):

```sql
CREATE TABLE pipelines (
    id          BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    repo_id     BIGINT NOT NULL REFERENCES repositories(id) ON DELETE CASCADE,
    commit_sha  TEXT NOT NULL,
    branch      TEXT NOT NULL DEFAULT '',
    status      TEXT NOT NULL DEFAULT 'pending' CHECK(status IN ('pending','running','success','failure','cancelled')),
    triggered_by BIGINT REFERENCES users(id),
    started_at  TIMESTAMPTZ,
    finished_at TIMESTAMPTZ,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE TABLE pipeline_jobs (
    id          BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    pipeline_id BIGINT NOT NULL REFERENCES pipelines(id) ON DELETE CASCADE,
    name        TEXT NOT NULL,
    status      TEXT NOT NULL DEFAULT 'pending' CHECK(status IN ('pending','running','success','failure','skipped')),
    log_output  TEXT NOT NULL DEFAULT '',
    started_at  TIMESTAMPTZ,
    finished_at TIMESTAMPTZ,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
```

On push, `PipelineService.TriggerForCommit(ctx, repo, sha, branch)` is called fire-and-forget. The runner reads `.cloudzilla-ci.yml` from the commit tree, spawns jobs as goroutines (exec via `os/exec`, sandboxed to a temp directory), streams log output to `pipeline_jobs.log_output`, and updates statuses. The commit status API (Phase 3.2) is updated with pass/fail results automatically.

---

### Phase 18.2 — Pipeline YAML Config & UI

**Migration** (`058_create_pipeline_config.sql`):

```sql
CREATE TABLE pipeline_configs (
    id        BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    repo_id   BIGINT NOT NULL REFERENCES repositories(id) ON DELETE CASCADE UNIQUE,
    yaml_path TEXT NOT NULL DEFAULT '.cloudzilla-ci.yml',
    enabled   BOOLEAN NOT NULL DEFAULT TRUE,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
```

The YAML schema supports `jobs:` with `name`, `runs-on` (host only in v1), `steps:` (each with `name`, `run` shell command), and `env:` key-value pairs. A `PipelineConfigService.Parse(yamlContent string)` function validates the schema using `encoding/yaml` (no new dep; already stdlib-compatible via `gopkg.in/yaml.v3` which is used by Viper). The repo settings page gains a "CI/CD" section to enable/disable pipelines and view the YAML schema reference. `GET /{owner}/{repo}/actions` lists pipeline runs.

---

### Phase 18.3 — CI Status Dashboard

_Zero-migration — reads from existing `pipelines` and `pipeline_jobs` tables._

`GET /{owner}/{repo}/actions` renders a paginated list of pipeline runs with status badges, branch names, commit SHAs, and durations. `GET /{owner}/{repo}/actions/{id}` shows the detailed job log (streamed via HTMX `hx-trigger="every 2s"` polling while status is `running`). Branch and status filters supported via query params. The commit list, PR detail, and tree pages show a status indicator badge linking to the most recent pipeline run for that commit SHA.

---

## Phase 19 — LDAP / SAML Improvements, IP Allowlisting & Access Policies, API Rate Limiting & Quotas

_Enterprise hardening: richer SSO, network-level access control, and abuse prevention._

### Phase 19.1 — LDAP / SAML Improvements

_Zero-migration — extends the `sso_configs` JSONB config from Phase 8.3._

Adds LDAP group-to-role mapping: a config field `group_map` specifies which LDAP groups map to `superadmin` or `user` instance roles. SAML improvements: support encrypted assertions, configurable NameID format, and IdP-initiated login (unsolicited responses). A "Test connection" button (`POST /admin/sso/test`) validates LDAP bind or fetches SAML metadata without saving. Adds a `sync_interval_minutes` config key to periodically re-sync LDAP group memberships in the background.

---

### Phase 19.2 — IP Allowlisting & Access Policies

**Migration** (`059_create_access_policies.sql`):

```sql
CREATE TABLE ip_allowlists (
    id         BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    cidr       TEXT NOT NULL,
    label      TEXT NOT NULL DEFAULT '',
    enabled    BOOLEAN NOT NULL DEFAULT TRUE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
```

When any rows exist in `ip_allowlists` with `enabled = TRUE`, a middleware (`middleware/ip_allowlist.go`) blocks all requests from IPs not matching any CIDR. Superadmins manage the list via `GET/POST/DELETE /admin/access-policies`. The middleware always passes the superadmin's own IP through to prevent lockout. CIDR matching uses `net.ParseCIDR` + `cidr.Contains(ip)` from stdlib.

---

### Phase 19.3 — API Rate Limiting & Quotas

**Migration** (`060_create_rate_limit_config.sql`):

```sql
CREATE TABLE rate_limit_config (
    id              BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    subject_type    TEXT NOT NULL DEFAULT 'global' CHECK(subject_type IN ('global','user','token')),
    requests_per_hr INTEGER NOT NULL DEFAULT 5000,
    burst           INTEGER NOT NULL DEFAULT 100,
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
```

A `RateLimitMiddleware` uses an in-memory token bucket per `(subject_type, subject_id)` (`sync.Map` of `*rate.Limiter` from `golang.org/x/time/rate` — the only new dependency). Returns `429 Too Many Requests` with `Retry-After` header when the bucket is exhausted. Limits: authenticated users get `requests_per_hr / 3600` tokens per second; unauthenticated requests share a global bucket. Superadmins configure limits in the admin panel.

---

## Phase 20 — S3/GCS Storage Backend, Instance Clustering / HA, GraphQL API v2

_Infrastructure scalability for large deployments._

### Phase 20.1 — S3/GCS Storage Backend

_Zero-migration — storage backend is an abstraction layer._

Introduces a `StorageBackend` interface in `internal/storage/`:

```go
type Backend interface {
    Put(ctx context.Context, path string, r io.Reader, size int64) error
    Get(ctx context.Context, path string) (io.ReadCloser, error)
    Delete(ctx context.Context, path string) error
    Exists(ctx context.Context, path string) (bool, error)
}
```

Implementations: `LocalBackend` (current disk storage, default), `S3Backend` (AWS SDK v2 — new dependency when enabled), `GCSBackend` (Google Cloud Storage client — new dependency when enabled). All git repos, LFS objects, release assets, registry blobs, and package files are routed through the backend interface. Config: `storage.backend: local|s3|gcs`; backend-specific keys under `storage.s3.*` / `storage.gcs.*`. Migration from local to S3 is a one-time `cloudzilla-cli migrate-storage` command.

---

### Phase 20.2 — Instance Clustering / HA

_Zero-migration for core; adds a `cluster_nodes` table for node registration._

```sql
CREATE TABLE cluster_nodes (
    id           BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    node_id      TEXT NOT NULL UNIQUE,
    address      TEXT NOT NULL,
    last_seen_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
```

Multiple Cloudzilla instances share the same PostgreSQL database and S3/GCS storage backend. Sticky sessions or a session token stored in PostgreSQL (`sessions` table) replace the stateless JWT-only approach for pages that need it. Background jobs (webhook retry, pipeline runner, secret scanning) use advisory locks (`SELECT pg_try_advisory_lock(...)`) to prevent duplicate execution across nodes. Node health endpoint: `GET /api/cluster/health`.

---

### Phase 20.3 — GraphQL API v2

_Zero-migration — GraphQL sits on top of existing services._

Exposes a `POST /api/graphql` endpoint implementing a typed GraphQL schema over the existing service layer. Schema covers: `User`, `Repository`, `Issue`, `PullRequest`, `Comment`, `Release`, `Label`, `Milestone`, `Organization`. Resolvers call service methods — no store access bypassing the service layer. Built using `encoding/json` + a lightweight hand-rolled schema executor (no new GraphQL library required for v1 of the schema). Introspection enabled. Authentication via the same JWT/PAT bearer token as the REST API. A GraphQL Playground page at `GET /api/graphql/playground` (superadmin only).

---

## Roadmap Summary

| Phase | Feature                              | Status     | Migration(s) |
| ----- | ------------------------------------ | ---------- | ------------ |
| 0.1   | Core Platform                        | ✅ Done    | 001–007      |
| 0.2   | OAuth & Organizations                | ✅ Done    | 008–010      |
| 0.3   | Webhooks, Notifs, Admin              | ✅ Done    | 011–015      |
| 1.1   | Labels                               | ✅ Done    | 016          |
| 1.2   | Assignees                            | ✅ Done    | 017          |
| 1.3   | Stars                                | ✅ Done    | 018          |
| 2     | Repository Fork                      | ✅ Done    | 019          |
| 3.1   | Releases                             | ✅ Done    | 020          |
| 3.2   | Commit Status API                    | ✅ Done    | 021          |
| 3.3   | Milestones                           | ✅ Done    | 022          |
| 4.1   | PR Reviews                           | ✅ Done    | 023          |
| 4.2   | PR Line Comments                     | ✅ Done    | 024          |
| 4.3   | Search                               | ✅ Done    | 025          |
| 5.1   | Personal Access Tokens               | ✅ Done    | 027          |
| 5.2   | Deploy Keys                          | ✅ Done    | 028          |
| 5.3   | Draft Pull Requests                  | ✅ Done    | 029          |
| 6.1   | Protected Branches                   | ✅ Done    | 030          |
| 6.2   | CODEOWNERS Support                   | ✅ Done    | —            |
| 6.3   | Code Review Suggestions              | ✅ Done    | 031          |
| 7.1   | Auto-merge                           | ✅ Done    | 032          |
| 7.2   | Issue & PR Templates                 | ✅ Done    | —            |
| 7.3   | Comment Reactions                    | ✅ Done    | 033          |
| 8.1   | Two-Factor Auth (TOTP)               | ✅ Done    | 034          |
| 8.2   | Audit Log                            | ✅ Done    | 035          |
| 8.3   | LDAP / SAML SSO                      | ✅ Done    | 036          |
| 9.1   | Project Boards / Kanban              | ✅ Done    | 036          |
| 9.2   | Wiki                                 | ✅ Done    | —            |
| 9.3   | Issue Pinning & Locking              | ✅ Done    | 038          |
| 10.1  | Repository Insights & Stats          | ✅ Done    | —            |
| 10.2  | @Mentions in Comments                | ✅ Done    | 039          |
| 10.3  | Saved Replies                        | ✅ Done    | 040          |
| 11.1  | Email Notifications                  | ✅ Done    | 041          |
| 11.2  | OAuth Apps / Third-party Clients     | ✅ Done    | 042          |
| 11.3  | Webhook Improvements (retry, filter) | ⬜ Planned | —            |
| 12.1  | Watching                             | ⬜ Planned | 043          |
| 12.2  | Activity Feed                        | ⬜ Planned | 044          |
| 12.3  | Discussions                          | ⬜ Planned | 045          |
| 13.1  | Gists                                | ⬜ Planned | 046          |
| 13.2  | Profile README                       | ⬜ Planned | —            |
| 13.3  | Repository Topics / Tags             | ⬜ Planned | 047          |
| 14.1  | Private Issues                       | ⬜ Planned | 048          |
| 14.2  | Archive & Templates                  | ⬜ Planned | 049          |
| 14.3  | Repository Soft-delete & Recovery    | ⬜ Planned | 050          |
| 15.1  | Advanced Code Search                 | ⬜ Planned | 051          |
| 15.2  | Explore / Trending                   | ⬜ Planned | —            |
| 15.3  | Dependency Graph                     | ⬜ Planned | 052          |
| 16.1  | Container Registry (Docker)          | ⬜ Planned | 053          |
| 16.2  | Generic Package Registry             | ⬜ Planned | 054          |
| 16.3  | Release Asset Enhancements           | ⬜ Planned | —            |
| 17.1  | Git LFS Support                      | ⬜ Planned | 055          |
| 17.2  | Signed Commit Verification (GPG/SSH) | ⬜ Planned | —            |
| 17.3  | Secret Scanning                      | ⬜ Planned | 056          |
| 18.1  | CI/CD Pipeline Runner (basic)        | ⬜ Planned | 057          |
| 18.2  | Pipeline YAML Config & UI            | ⬜ Planned | 058          |
| 18.3  | CI Status Dashboard                  | ⬜ Planned | —            |
| 19.1  | LDAP / SAML Improvements             | ⬜ Planned | —            |
| 19.2  | IP Allowlisting & Access Policies    | ⬜ Planned | 059          |
| 19.3  | API Rate Limiting & Quotas           | ⬜ Planned | 060          |
| 20.1  | S3/GCS Storage Backend               | ⬜ Planned | —            |
| 20.2  | Instance Clustering / HA             | ⬜ Planned | —            |
| 20.3  | GraphQL API v2                       | ⬜ Planned | —            |

**Critical files touched by every phase:**

- `internal/router/router.go` — register new routes + add page names to `pageNames`
- `internal/handler/viewmodels.go` — add data structs for new pages/fragments
- `internal/service/services.go` — wire new service into `Services` struct + `New()`
- `internal/store/stores.go` — wire new store into `Stores` struct + `New()`
- `internal/handler/page_handler.go` — extend existing page handlers with new data fetches

---
