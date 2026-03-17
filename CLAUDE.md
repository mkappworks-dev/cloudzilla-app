# CLAUDE.md — Cloudzilla Developer Guide

> ⚠️ **Alpha**: Cloudzilla is under active development. APIs, project structure, and conventions may change as the project matures.

## Architecture

- **Backend**: Go 1.23+, chi router, sqlx, go-git, cobra CLI
- **Frontend**: Go `html/template`, HTMX for partial updates, Tailwind CSS
- **DB**: SQLite (default), PostgreSQL (production)
- **Pattern**: Stores → Services → Handlers (strict layer separation)
- **Rendering**: Server-driven; no JavaScript framework, no build tooling needed
- **Git Transport**: HTTP smart protocol + SSH server (both via pure Go, no git binary required)
- **SSH Auth**: Public key authentication via stored SSH keys

## Project Layout

- `cmd/server/` — HTTP server entrypoint
- `cmd/cloudzilla/` — Admin CLI (cobra)
- `internal/config/` — Config loading (viper + YAML)
- `internal/db/` — DB connection + migration runner
- `internal/model/` — Data structs (db + json tags)
- `internal/store/` — Raw SQL queries (sqlx)
- `internal/service/` — Business logic (calls stores)
- `internal/handler/` — HTTP handlers (calls services + renders templates)
- `internal/middleware/` — Auth, logger, CORS
- `internal/router/` — chi route registration + template parsing
- `internal/ssh/` — SSH server for git operations (gliderlabs/ssh)
- `migrations/` — SQL files, embedded via embed.FS
- `cmd/server/frontend/` — Static files + templates
  - `templates/layout.html` — Base HTML shell
  - `templates/pages/*.html` — Page templates (home, login, user, repo, issues, pulls, etc.)
  - `templates/fragments/*.html` — HTMX swap fragments
  - `static/main.css` — Compiled Tailwind output
  - `htmx.min.js` — HTMX library
- `tailwind/` — Tailwind CSS config
  - `input.css` — Tailwind directives
  - `tailwind.config.js` — Theme + content paths

## Dev Commands

```bash
make setup-tailwind     # Download Tailwind CLI (one-time)
make build-css          # Compile Tailwind → static/main.css
make dev                # Run server + Tailwind watch
make migrate            # Run DB migrations
make build              # Build Go binary (embedded templates + CSS)
make lint               # Lint Go code
go test ./...           # Run Go tests
```

## Code Conventions

### Go Handlers

- **Page handlers** (`PageHome`, `PageIssues`, etc.) fetch data and call `h.render(page, data)` to render full pages
- **HTMX handlers** check `r.Header.Get("HX-Request") == "true"` and call `h.renderFragment(name, data)` to return HTML snippets
- **API handlers** return JSON via `writeJSON(w, status, v)`
- Handlers only call services, never stores directly
- Use `context.Context` as first arg in all service/store methods
- JWT is read from `Authorization: Bearer` header OR `cz_token` httpOnly cookie

### Templates (Go html/template)

- **Layout**: `{{define "layout"}}...{{template "content" .}}...{{end}}`
- **Pages**: Each page file defines `{{define "title"}}...{{end}}` and `{{define "content"}}...{{end}}`
- **Fragments**: Fragments define `{{define "fragment-NAME"}}...{{end}}`
- Use `{{.Field}}` for data access; use `.` alone to pass entire struct to nested templates
- HTMX attributes go on HTML elements: `hx-post="/api/..."`, `hx-target="#id"`, `hx-swap="outerHTML"`
- Template auto-escaping prevents XSS (no `html.HTML` needed for user content)

### CSS (Tailwind)

- Use Tailwind utility classes in templates; no custom CSS
- Build with `make build-css` (runs before `make dev` and `make build`)
- Config in `tailwind/tailwind.config.js` — update `content` glob if adding new template dirs
- Output at `cmd/server/frontend/static/main.css` (gitignored)

## Adding a New Feature (checklist)

1. Add SQL migration in `migrations/`
2. Add/update model struct in `internal/model/`
3. Add store method in `internal/store/`
4. Add service method in `internal/service/`
5. Add handler in `internal/handler/` (or update existing)
6. Register route in `internal/router/router.go`
7. If the route must be accessible before setup is complete (e.g. public assets), add it to the allowlist in `middleware/setup.go`
8. Add/update HTML template in `cmd/server/frontend/templates/`
9. Add Tailwind CSS classes to template
10. Add fragment templates if using HTMX swaps
11. Add new page name to `pageNames` slice in `router/router.go` if adding a new page template

## Authentication Flow

1. **Form login**: POST `/login` (form data) → handler calls `User.Authenticate()` → sets httpOnly cookie → redirects to `/`
2. **API login**: POST `/api/auth/login` (JSON) → handler returns JWT in cookie + JSON body
3. **Protected pages**: `optAuthMW` middleware reads cookie, injects claims into context (optional)
4. **HTMX requests**: Browser automatically includes cookie (same-origin); handler checks claims if needed

### Google OAuth Flow

```
User clicks "Sign in with Google"
  → GET /auth/google
      → generate random state, set oauth_state cookie (5 min, httpOnly)
      → redirect to Google auth URL
  → User approves on Google
  → GET /auth/google/callback?code=...&state=...
      → validate state cookie
      → exchange code → access token
      → fetch https://www.googleapis.com/oauth2/v2/userinfo
      → upsert user (see account linking below)
      → generate JWT, set cz_token cookie
      → redirect to /
```

**Account linking priority:**

1. `oauth_provider=google` + `oauth_id` found → log in directly
2. Email already exists (password user) → link OAuth to existing account → log in
3. Neither → create new user (username derived from name/email prefix, deduplicated)

**Configuration** (`config.yaml`):

```yaml
oauth:
  google_client_id: "YOUR_CLIENT_ID.apps.googleusercontent.com"
  google_client_secret: "YOUR_SECRET"
  google_redirect_url: "http://localhost:8080/auth/google/callback"
```

If `google_client_id` is empty, `GET /auth/google` returns 501 Not Implemented. The button still renders on the login page but fails gracefully.

## SSH Key Management

### Adding SSH Keys

Users can add SSH public keys for git operations:

```bash
curl -X POST http://localhost:8080/api/user/keys \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"title": "laptop", "public_key": "ssh-ed25519 AAAA..."}'
```

### SSH Key Storage

- Public keys stored in `ssh_keys` table with MD5 fingerprints
- Fingerprints used for fast public key lookups during SSH handshakes
- One key per user; users can have multiple keys with different titles

### Listing and Deleting Keys

```bash
# List all keys for authenticated user
GET /api/user/keys

# Delete a key by ID
DELETE /api/user/keys/{id}
```

## Git HTTP Smart Protocol

### Overview

Cloudzilla exposes repositories via the Git HTTP smart protocol, allowing standard `git clone/push/pull` operations.

### Endpoints

- `GET /{owner}/{repo}/info/refs?service=git-upload-pack` — List refs (clone/fetch)
- `POST /{owner}/{repo}/git-upload-pack` — Upload pack (clone/fetch data)
- `POST /{owner}/{repo}/git-receive-pack` — Receive pack (push data)

### Authentication

- **Public repos**: No authentication required
- **Private repos**: Requires HTTP Basic Auth or JWT cookie
- Permissions enforced: read access for clone/fetch, write access for push

### Example

```bash
# Clone a public repo
git clone http://localhost:8080/admin/my-project.git

# Clone a private repo (with basic auth)
git clone http://user:password@localhost:8080/admin/private-repo.git

# Push requires write access
git push origin main
```

## SSH Server

### Overview

Cloudzilla runs an SSH server (port 2222 by default) for git operations using public key authentication.

### Configuration

In `config.yaml`:

```yaml
git:
  repos_root: ./git-repos
  ssh_port: 2222 # SSH server port
  ssh_host_key: ./cloudzilla_host_key # Host key file (auto-generated if missing)
```

### SSH Git Operations

Users with SSH keys can clone, fetch, and push via SSH:

```bash
# Add an SSH key first
curl -X POST http://localhost:8080/api/user/keys \
  -H "Authorization: Bearer $TOKEN" \
  -d '{"title": "mykey", "public_key": "'$(cat ~/.ssh/id_ed25519.pub)'"}'

# Clone via SSH
git clone ssh://git@localhost:2222/owner/repo.git

# Standard git operations work
git push origin main
git fetch
git pull
```

### How SSH Auth Works

1. Client initiates SSH connection to port 2222
2. Server presents host public key
3. Client sends user's SSH public key
4. Server computes MD5 fingerprint and looks up matching SSH key in database
5. If found, extracts user ID from key owner
6. User is authenticated and context is populated
7. `git-upload-pack` or `git-receive-pack` command is dispatched with user context
8. Repository permissions are checked (read for upload-pack, write for receive-pack)

## HTMX Pattern (Example: Close Issue)

Template:

```html
<div id="issue-detail" ...>
  {{if eq .Issue.State "open"}}
  <button
    hx-patch="/api/repos/{{.Owner}}/{{.Repo}}/issues/{{.Issue.Number}}"
    hx-vals='{"state":"closed"}'
    hx-target="#issue-detail"
    hx-swap="outerHTML"
  >
    Close Issue
  </button>
  {{end}}
</div>
```

Handler:

```go
func (h *Handler) UpdateIssue(w http.ResponseWriter, r *http.Request) {
    // ... fetch and update issue ...
    if r.Header.Get("HX-Request") == "true" {
        h.renderFragment(w, "fragment-issue-detail", IssueDetailFragData{...})
        return
    }
    writeJSON(w, http.StatusOK, issue)
}
```

Fragment:

```html
{{define "fragment-issue-detail"}}
<div id="issue-detail" ...>
  <!-- Rendered state after toggle -->
  {{if eq .Issue.State "open"}}...{{end}}
</div>
{{end}}
```

HTMX flow: button click → PATCH → handler returns fragment → HTMX replaces `#issue-detail` outerHTML

## Template Parsing

Templates are parsed at startup in `router.mustParseTemplates()`:

1. Parse `layout.html` into a base template
2. Clone base template for each page, parse page file into clone
3. Parse all fragments into a shared template set
4. Store page clones in map, fragment set in handler
5. Handler calls `tmpl.ExecuteTemplate(w, "layout", data)` for pages or `frags.ExecuteTemplate(w, "fragment-NAME", data)` for fragments

This pattern avoids Go template's global `define` namespace issue.

## SQLite Notes

- WAL mode enabled at startup
- Foreign keys enforced (`PRAGMA foreign_keys=ON`)
- Use `?` placeholders (not `$1`)
- For PostgreSQL migration: change driver in config.yaml, use `$N` placeholders

## Git Repository Permissions

### Permission Model

All git operations (HTTP and SSH) respect the same permission rules:

**Read Access** (`git clone`, `git fetch`, `git pull`):

- Public repositories: Always allowed
- Private repositories: Requires authentication + one of:
  - User is the repository owner
  - User has a permission record with role `reader`, `writer`, or `admin`

**Write Access** (`git push`):

- Requires authentication + one of:
  - User is the repository owner
  - User has a permission record with role `writer` or `admin`

### Implementation

- `RepoService.CanRead(ctx, repo, userID)` — checks public/private + permissions (any role grants read)
- `RepoService.CanWrite(ctx, repo, userID)` — owner, org owner, or `writer`/`admin` permission role
- `RepoService.CanManage(ctx, repo, userID)` — owner or org owner **only** (not `admin` collaborator)
- `RepoService.TransferRepo(ctx, repo, requestingUserID, newOwnerUsername)` — moves git dir on disk, updates `owner_id`/`owner_name`; personal repos only
- Both HTTP handlers and SSH handlers call `CanRead`/`CanWrite` before processing git commands
- Bare repository created with `go-git.PlainInit()`, fully compatible with git CLI

## Code Browser

### URL Patterns

| View                    | Route                                     |
| ----------------------- | ----------------------------------------- |
| Root tree               | `/{owner}/{repo}/tree/{ref}`              |
| Subtree                 | `/{owner}/{repo}/tree/{ref}/{path...}`    |
| Blob                    | `/{owner}/{repo}/blob/{ref}/{path...}`    |
| Blame                   | `/{owner}/{repo}/blame/{ref}/{path...}`   |
| Commit log              | `/{owner}/{repo}/commits/{ref}`           |
| Commit log (file scope) | `/{owner}/{repo}/commits/{ref}/{path...}` |
| Single commit diff      | `/{owner}/{repo}/commit/{sha}`            |
| Branches & Tags         | `/{owner}/{repo}/refs`                    |

`{ref}` = branch name, tag name, or commit SHA. Pagination via `?page=N` (1-indexed, 30 per page).

### CodeService (`internal/service/code_service.go`)

`CodeService` has **no store dependency** — it reads git data directly from bare repos on disk via go-git. It is wired in `services.New()` and receives `config.GitConfig` (for `ReposRoot`).

Key methods:

- `ResolveRef(owner, repoName, ref)` → `(*object.Commit, displayRef, error)`
- `GetTree(owner, repoName, ref, path)` → `*TreeResult`
- `GetBlob(owner, repoName, ref, path)` → `*BlobResult`
- `GetBlame(owner, repoName, ref, path)` → `*BlameResult`
- `GetCommits(owner, repoName, ref, page, pageSize)` → `*CommitLog`
- `GetCommit(owner, repoName, sha)` → `*CommitDetail`
- `ListRefs(owner, repoName, defaultBranch)` → `*RefsResult`
- `CreateBranch(owner, repoName, name, fromRef)` → `error`
- `DeleteBranch(owner, repoName, name)` → `error`
- `CreateTag(owner, repoName, name, fromRef)` → `error`
- `DeleteTag(owner, repoName, name)` → `error`

### ResolveRef Priority

1. Branch: `repo.Reference(plumbing.NewBranchReferenceName(ref), true)`
2. Tag: `repo.Reference(plumbing.NewTagReferenceName(ref), true)`
3. Raw SHA: `repo.CommitObject(plumbing.NewHash(ref))`
4. HEAD fallback (when `ref == ""`): `repo.Head()`

Returns `ErrEmptyRepo` sentinel when HEAD resolution fails (repo has no commits). Handlers return 404 on this error.

### Result Types

- `TreeResult` — `Entries []TreeEntry` (dirs first, then files, both sorted), `Ref`, `Path`, `Breadcrumbs`
- `BlobResult` — `Lines []CodeLine`, `IsBinary bool`, `BlameURL`, breadcrumbs
- `BlameResult` — `Lines []BlameLine` with `ShowMeta bool` (true when commit run changes), `BlobURL`, breadcrumbs
- `CommitLog` — `Commits []CommitSummary`, `Ref`, `Page`, `PrevPage`, `NextPage`, `HasMore`
- `CommitDetail` — full commit with `Files []FileDiff` (hunks with add/del/ctx lines), `TotalAdded`, `TotalDeleted`
- `RefsResult` — `Branches []BranchInfo` (`Name`, `Hash`, `IsDefault`), `Tags []TagInfo` (`Name`, `Hash`)

## Deployment

1. Run `make build` — produces single `dist/cloudzilla` binary with embedded templates + CSS
2. No Node.js, npm, or Bun needed
3. `./dist/cloudzilla` runs the server on the configured port
4. Set config via `config.yaml` or environment variables (see `internal/config/`)

## Docker

Cloudzilla ships a multi-stage `Dockerfile` and `docker-compose.yml`.

### Image build stages

| Stage     | Base                 | Purpose                                                                                                          |
| --------- | -------------------- | ---------------------------------------------------------------------------------------------------------------- |
| `builder` | `golang:1.23-alpine` | Downloads Tailwind CLI (arch-aware), compiles CSS, builds both Go binaries with `CGO_ENABLED=0 -ldflags="-s -w"` |
| runtime   | `alpine:3.21`        | Copies binaries; installs `ca-certificates tzdata`; exposes 8080/2222                                            |

### Persistent volume (`/data`)

All mutable state lives under `/data` inside the container, mounted as a named Docker volume:

| What             | Path                        |
| ---------------- | --------------------------- |
| SQLite database  | `/data/cloudzilla.db`       |
| Git repositories | `/data/git-repos/`          |
| SSH host key     | `/data/cloudzilla_host_key` |

### Key environment variables (Viper `CZ_` prefix)

```
CZ_DATABASE_DSN=/data/cloudzilla.db
CZ_GIT_REPOS_ROOT=/data/git-repos
CZ_GIT_SSH_HOST_KEY=/data/cloudzilla_host_key
CZ_AUTH_JWT_SECRET=<strong secret>
CZ_SERVER_PORT=8080
CZ_GIT_SSH_PORT=2222
```

No `config.yaml` file is needed at runtime when env vars are set.

### Docker make targets

```bash
make docker-build   # docker build -t cloudzilla:latest .
make docker-run     # docker compose up -d
make docker-down    # docker compose down
```

### First-run bootstrap

```bash
make docker-run
docker exec -it cloudzilla-cloudzilla-1 /app/cloudzilla-cli migrate
```

Then open `http://localhost:8080` in a browser — the first request redirects to `/setup` where you create the superadmin account via the web wizard.

The SSH host key is auto-generated into the named volume on first boot — no manual `ssh-keygen` step needed.

## Branch & Tag Management

The Refs page (`/{owner}/{repo}/refs`) lists all branches and tags. Authenticated users with write access can create and delete branches/tags via HTMX forms.

**Permission rules:**

- Public repos: refs page always visible (read-only for unauthenticated)
- Write access required for create/delete; default branch delete is blocked (button hidden)

**API endpoints** (all require `authMW`):

| Method | Path                                        | Description                                |
| ------ | ------------------------------------------- | ------------------------------------------ |
| POST   | `/api/repos/{owner}/{repo}/branches`        | Create branch (`name`, `from` form fields) |
| DELETE | `/api/repos/{owner}/{repo}/branches?name=…` | Delete branch                              |
| POST   | `/api/repos/{owner}/{repo}/tags`            | Create tag (`name`, `from` form fields)    |
| DELETE | `/api/repos/{owner}/{repo}/tags?name=…`     | Delete tag                                 |

HTMX responses swap `fragment-branches-list` into `#branches-list` and `fragment-tags-list` into `#tags-list`.

The ref badge on tree and commits pages links to `/{owner}/{repo}/refs` (via `RefsURL` field on `TreeData` / `CommitsData`).

---

## Pull Request Merge Strategies

Cloudzilla supports three merge strategies selectable from the PR detail page.

**Diff view:** The PR detail page shows a full file diff between base and head tips for open PRs. Diff is omitted for closed/merged PRs.

**Available strategies:**

| Strategy        | Button                 | When shown                           | What it does                                            |
| --------------- | ---------------------- | ------------------------------------ | ------------------------------------------------------- |
| Fast-forward    | "Merge (fast-forward)" | Head is a direct descendant of base  | Advances base branch ref — no new commit                |
| Three-way merge | "Create merge commit"  | FF or clean three-way merge possible | Creates a commit with two parents (base + head)         |
| Squash merge    | "Squash and merge"     | FF or clean three-way merge possible | Collapses head commits into a single new commit on base |

**Conflict detection:** `mergeTreesNoConflict` performs a pure tree-level three-way merge — if the same file path was modified on both sides relative to the merge base, all merge buttons are hidden and a conflict warning is shown. The developer must rebase locally and push.

**Merge flow:**

1. `PagePullDetail` calls `CodeService.GetPullDiff(base, head)` → `PRDiffResult{Files, CanFastForward, CanThreeWayMerge}`
2. Template renders diff + conditionally shows strategy buttons based on capability flags
3. On button click → HTMX `PATCH /api/repos/{owner}/{repo}/pulls/{number}` with `state=merged` and `merge_strategy=ff|merge|squash`
4. `UpdatePull` dispatches to `MergePullRequest`, `ThreeWayMergePullRequest`, or `SquashMergePullRequest`
5. On success → `PullService.SetState(merged)` → fragment returned
6. On failure (conflict, missing branch, etc.) → 422 → `hx-on::response-error` fires alert

**`CodeService` methods:**

- `GetPullDiff(owner, repo, base, head)` → `*PRDiffResult`
- `MergePullRequest(owner, repo, base, head)` → `error` (fast-forward only)
- `ThreeWayMergePullRequest(owner, repo, base, head, authorName, authorEmail)` → `error`
- `SquashMergePullRequest(owner, repo, base, head, authorName, authorEmail)` → `error`
- `checkFastForward(repo, baseCommit, headCommit)` → `bool` (private)
- `findMergeBase(repo, a, b)` → `(*object.Commit, error)` (private; LCA via ancestor walk)
- `mergeTreesNoConflict(repo, mergeBase, base, head)` → `(plumbing.Hash, bool, error)` (private)
- `flattenTree(tree)` → `(map[string]mergeFile, error)` (private)
- `buildTree(repo, files)` → `(plumbing.Hash, error)` (private; recursively encodes tree objects)

**`PRDiffResult` type:**

```go
type PRDiffResult struct {
    Files            []FileDiff
    TotalAdded       int
    TotalDeleted     int
    CanFastForward   bool  // head is a descendant of base
    CanThreeWayMerge bool  // branches diverged but no conflicting file edits
}
```

---

## Instance Permissions & Access Control

### Permission levels

| Level        | Roles                       | Description                               |
| ------------ | --------------------------- | ----------------------------------------- |
| Instance     | `superadmin`, `user`        | Controls instance-wide access             |
| Organization | `owner`, `member`           | Controls org membership and repo creation |
| Repository   | `reader`, `writer`, `admin` | Controls per-repo access                  |

### Instance roles

| Role         | Permissions                                                                        |
| ------------ | ---------------------------------------------------------------------------------- |
| `superadmin` | Everything. Manages instance settings, cannot be locked out. Assigned at `/setup`. |
| `user`       | Normal account. Access governed by org/repo permissions and instance settings.     |

### First-run wizard (`/setup`)

When no users exist, **all routes redirect to `/setup`**. The first person to submit the form becomes superadmin. After any user exists, `/setup` permanently redirects to `/`.

`RequireSetup` middleware runs globally (after Recoverer, before routes). Always passes `/setup`, `/static/`, `/invite/`, and `/htmx.min.js` through unconditionally.

`SiteSettingService.IsSetupComplete()` caches the result atomically via `sync/atomic.Bool` — once true it never re-queries the DB.

### Instance settings (`site_settings` table)

Seeded by migration 014. Two boolean keys:

| Key                  | Default | Behaviour when `false`                                                        |
| -------------------- | ------- | ----------------------------------------------------------------------------- |
| `allow_registration` | `true`  | Blocks new account creation (form & OAuth). Invite tokens bypass this.        |
| `allow_login`        | `true`  | Blocks non-superadmin, non-invited logins. "Sign in" link hidden from navbar. |

`SiteSettingService` holds a `sync.RWMutex`-protected `map[string]string` cache. `AllowLogin` / `AllowRegistration` call `loadCache` once on first access and read from the map thereafter. Every `Set` refreshes the cache.

`basePage()` reads `AllowLogin` and sets `BasePage.AllowLogin`; the layout template conditionally renders the "Sign in" link.

### Invitation system

No SMTP required. Superadmin generates a token link → shares it manually.

Flow:

1. Superadmin POSTs `email` to `/api/admin/invitations` → 32-byte hex token, 7-day expiry
2. Admin panel displays `/invite/{token}` link for copying
3. Recipient visits link → form with `email` pre-filled (read-only)
4. On submit: user created via `UserService.Create`, `is_invited = TRUE` set, invitation marked accepted, JWT cookie set → redirect `/`

`is_invited` users always bypass `allow_registration` and `allow_login` checks.

### Admin panel (`/admin/settings`)

Superadmin-only. Accessible via the "Admin" link (yellow) in the navbar.

- **Settings section**: HTMX toggle buttons; POST to `/api/admin/settings`; swaps `fragment-admin-settings` into `#admin-settings-list`
- **Invitations section**: create by email; displays invite URL for copying; delete pending invites; swaps `fragment-admin-invitations` into `#admin-invitations-list`

### Repository collaborators

The `permissions` table has always existed; it now has full CRUD via the repo settings page.

`CanManage(ctx, repo, userID)` — gates collaborator management: **only** `repo.OwnerID == userID` or an org owner (`isOrgOwner`). Collaborators with the `admin` role have write access but cannot manage collaborators.

`ListPermissionsWithUsername` — uses `JOIN users ON p.user_id = u.id`; populates `Permission.Username` (not a DB column, populated via JOIN).

**Permission matrix for personal repos:**

| Who                          | Read private | Push | Manage collaborators | Transfer ownership |
| ---------------------------- | :----------: | :--: | :------------------: | :----------------: |
| Repo owner (`repo.owner_id`) |      ✓       |  ✓   |          ✓           |         ✓          |
| Collaborator: `admin`        |      ✓       |  ✓   |          ✗           |         ✗          |
| Collaborator: `writer`       |      ✓       |  ✓   |          ✗           |         ✗          |
| Collaborator: `reader`       |      ✓       |  ✗   |          ✗           |         ✗          |

For org repos, any org `owner` additionally gets read/write/manage on all repos in that org (via `isOrgOwner`). Transfer is not supported on org repos.

**API endpoints** (all under `/api/repos/{owner}/{repo}/collaborators`):

| Method                            | Auth                       | Description                           |
| --------------------------------- | -------------------------- | ------------------------------------- |
| GET `/collaborators`              | Optional                   | List collaborators with username      |
| POST `/collaborators`             | Required + owner/org-owner | Add collaborator (`username`, `role`) |
| DELETE `/collaborators?user_id=N` | Required + owner/org-owner | Remove collaborator                   |

**Ownership transfer** (personal repos only):

| Method | Path                                 | Auth             | Description                                                              |
| ------ | ------------------------------------ | ---------------- | ------------------------------------------------------------------------ |
| POST   | `/api/repos/{owner}/{repo}/transfer` | Required + owner | Transfer to another user (`new_owner` form field); moves git dir on disk |

HTMX responses swap `fragment-repo-collaborators` into `#repo-collaborators`.

### Error sentinels

`service/user_service.go` exports two errors for OAuth policy enforcement:

- `ErrRegistrationDisabled` — returned when `allow_registration=false` and the user does not exist
- `ErrLoginDisabled` — returned when `allow_login=false` and the user is neither superadmin nor invited

`GoogleOAuthCallback` matches against these and returns 403 with a plain-text message.

---

## Organizations

Cloudzilla supports organization accounts. An org is a shared namespace that can own repositories and have multiple members with roles.

### Roles

| Role     | Description                                                               |
| -------- | ------------------------------------------------------------------------- |
| `owner`  | Full admin: add/remove members, create/manage all repos in the org        |
| `member` | Can view org profile and be listed as a member; no repo management rights |

### Pages

| Route                  | Auth       | Description                      |
| ---------------------- | ---------- | -------------------------------- |
| `/{org}`               | Optional   | Org profile: repos + member list |
| `/orgs/{org}/settings` | Owner only | Manage members (add/remove)      |

The `/{owner}` route first checks if `owner` is a user; if not, falls back to org lookup. Org profile and user profile share the same URL pattern.

### API Endpoints

| Method | Path                                 | Auth     | Description                                                                         |
| ------ | ------------------------------------ | -------- | ----------------------------------------------------------------------------------- |
| POST   | `/api/orgs/`                         | Required | Create org (`name`, `display_name`, `description`)                                  |
| GET    | `/api/orgs/{org}`                    | —        | Get org by name                                                                     |
| GET    | `/api/orgs/{org}/members`            | —        | List org members                                                                    |
| POST   | `/api/orgs/{org}/members`            | Required | Add member (`username`, `role`); owner only                                         |
| DELETE | `/api/orgs/{org}/members/{username}` | Required | Remove member; owner only; last owner blocked                                       |
| POST   | `/api/orgs/{org}/repos`              | Required | Create a repo under the org; owner only                                             |
| POST   | `/api/orgs/{org}/transfer`           | Required | Transfer org ownership (`new_owner` form field); owner only; demotes self to member |

HTMX responses from add/remove member swap `fragment-org-members` into `#org-members`.

### OrgService

- `Create(ctx, creatorUserID, name, displayName, description)` → `(*Organization, error)` — validates name uniqueness against users table; auto-adds creator as owner
- `Get(ctx, name)` → `(*Organization, error)`
- `ListMembers(ctx, orgID)` → `([]OrgMember, error)`
- `IsOwner(ctx, orgID, userID)` → `bool`
- `IsMember(ctx, orgID, userID)` → `bool`
- `AddMember(ctx, orgID, requestingUserID, targetUserID, role)` → `error` — owner-only
- `RemoveMember(ctx, orgID, requestingUserID, targetUserID)` → `error` — owner-only; blocks removing last owner
- `CreateRepo(ctx, orgID, requestingUserID, name, description, private)` → `(*Repository, error)` — owner-only; sets `owner_name` to org name, `org_id` to org ID
- `ListRepos(ctx, orgID)` → `([]Repository, error)`
- `TransferOrg(ctx, orgID, requestingUserID, newOwnerUsername)` → `error` — owner-only; promotes new user to `owner`, demotes requesting user to `member`; adds new user as member if not already one

---

## Webhooks

Webhooks let repo owners receive HTTP POST callbacks when events occur in a repository.

### Supported Events

- `push` — fired when commits are pushed (HTTP or SSH receive-pack)
- `issues` — fired on issue create, close, reopen
- `pull_request` — fired on PR create, close, merge

### Delivery

Webhooks are dispatched fire-and-forget (`go s.Dispatch(...)`). Each delivery is recorded in `webhook_deliveries` with the event name, payload, response code, and any error.

**HMAC signing:** if a secret is configured, requests include `X-Hub-Signature-256: sha256=<HMAC-SHA256>` (GitHub-compatible).

### API Endpoints

All webhook endpoints are under `/api/repos/{owner}/{repo}/hooks`:

| Method | Path                                              | Auth         | Description                                |
| ------ | ------------------------------------------------- | ------------ | ------------------------------------------ |
| GET    | `/api/repos/{owner}/{repo}/hooks/`                | —            | List webhooks for repo                     |
| POST   | `/api/repos/{owner}/{repo}/hooks/`                | Write access | Create webhook (`url`, `secret`, `events`) |
| DELETE | `/api/repos/{owner}/{repo}/hooks/{id}`            | Write access | Delete webhook                             |
| GET    | `/api/repos/{owner}/{repo}/hooks/{id}/deliveries` | Write access | List delivery history                      |

HTMX requests for create/delete swap `fragment-webhooks-list` into `#webhooks-list`.

Default events when `events` is omitted: `push,issues,pull_request`.

### WebhookService

- `Create(ctx, repoID, url, secret, events)` → `(*Webhook, error)`
- `ListByRepo(ctx, repoID)` → `([]Webhook, error)`
- `Delete(ctx, id, repoID)` → `error`
- `ListDeliveries(ctx, webhookID)` → `([]WebhookDelivery, error)`
- `Dispatch(repoID, event, payload)` — fire-and-forget; call as `go s.Webhook.Dispatch(...)`
- `PushPayload(repo, pusher, branch, headSHA)` → `map[string]any`
- `IssuePayload(action, repo, issue)` → `map[string]any`
- `PullPayload(action, repo, pr)` → `map[string]any`

### Repo Settings Page

`/{owner}/{repo}/settings` (write access required) — shows a **Collaborators** section, a **Webhooks** section, and (for personal repo owners) a **Transfer Ownership** danger zone. The collaborators section is only editable by the repo owner or an org owner (`CanManage`). Collaborators with `admin` role can see the settings page (write access) but cannot modify collaborators or transfer.

---

## Notifications

In-app notification system that creates notifications for issue/PR activity involving the author.

### Notification Types

| Type             | Triggered when                            |
| ---------------- | ----------------------------------------- |
| `issue_comment`  | Someone comments on an issue you opened   |
| `pr_comment`     | Someone comments on a PR you opened       |
| `issue_closed`   | Someone closes an issue you opened        |
| `issue_reopened` | Someone reopens an issue you opened       |
| `pr_merged`      | Someone merges a PR you opened            |
| `pr_closed`      | Someone closes a PR you opened            |
| `pr_opened`      | (type reserved; not currently auto-fired) |

Notifications are never created when `actorID == authorID` (self-actions are silent).

### Unread Count in Navbar

`basePage()` calls `NotificationService.CountUnread` on every page render and passes the count as `BasePage.UnreadNotifCount`. Templates show a badge next to the Notifications link.

### Pages & API

| Route / Endpoint                      | Auth     | Description                                   |
| ------------------------------------- | -------- | --------------------------------------------- |
| GET `/notifications`                  | Required | Notifications page (list all)                 |
| PATCH `/api/notifications/{id}`       | Required | Mark single notification as read (HTMX-aware) |
| POST `/api/notifications/read-all`    | Required | Mark all notifications as read (HTMX-aware)   |
| GET `/api/notifications/unread-count` | Required | Returns `{"count": N}` JSON                   |

HTMX responses swap `fragment-notifications-list` into `#notifications-list`.

### NotificationService

- `List(ctx, userID)` → `([]Notification, error)`
- `CountUnread(ctx, userID)` → `(int, error)`
- `MarkRead(ctx, id, userID)` → `error`
- `MarkAllRead(ctx, userID)` → `error`
- `NotifyIssueComment(ctx, repo, issue, actorID, actorName)` — call from `CreateIssueComment` handler
- `NotifyPRComment(ctx, repo, pr, actorID, actorName)` — call from `CreatePRComment` handler
- `NotifyIssueStateChange(ctx, repo, issue, actorID, actorName)` — call from `UpdateIssue` handler
- `NotifyPRStateChange(ctx, repo, pr, actorID, actorName)` — call from `UpdatePull` handler
