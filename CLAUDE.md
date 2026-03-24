# CLAUDE.md — Cloudzilla Developer Guide

> ⚠️ **Alpha**: Cloudzilla is under active development. APIs, project structure, and conventions may change as the project matures.

## Architecture

- **Backend**: Go 1.23+, chi router, sqlx, go-git, cobra CLI
- **Frontend**: Go `html/template`, HTMX for partial updates, Tailwind CSS
- **DB**: PostgreSQL (default and production)
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
make download-mermaid   # Download mermaid.min.js (one-time; auto-runs in build/dev)
make build-css          # Compile Tailwind → static/main.css
make dev                # Run server + Tailwind watch (auto-downloads mermaid if missing)
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

`GET /auth/google` → state cookie → Google → `GET /auth/google/callback` → upsert user → `cz_token` cookie → redirect `/`

**Account linking priority:** existing `oauth_id` match → email match (links to password account) → create new user

**Config** (`config.yaml`): `oauth.google_client_id`, `oauth.google_client_secret`, `oauth.google_redirect_url`. If `google_client_id` is empty, returns 501 (button still renders, fails gracefully).

**Error sentinels** (`service/user_service.go`): `ErrRegistrationDisabled` (allow_registration=false, user not found), `ErrLoginDisabled` (allow_login=false, not superadmin/invited) — `GoogleOAuthCallback` returns 403 on these.

## Git Transport (HTTP + SSH)

See [docs/git-transport.md](./docs/git-transport.md) for full details (endpoints, config, curl examples, SSH auth flow).

- HTTP: `GET /{owner}/{repo}/info/refs`, `POST /{owner}/{repo}/git-upload-pack`, `POST /{owner}/{repo}/git-receive-pack`
- SSH: port 2222; public key auth via `ssh_keys` table (MD5 fingerprint lookup)
- SSH keys managed via `GET/POST/DELETE /api/user/keys`
- `CanRead` / `CanWrite` enforced on both transports before processing

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

1. Create a `template.FuncMap` with custom helpers (`add`, `percent`)
2. Parse `layout.html` into a base template with the FuncMap
3. Clone base template for each page, parse page file into clone
4. Parse all fragments into a shared template set (also with the FuncMap)
5. Store page clones in map, fragment set in handler
6. Handler calls `tmpl.ExecuteTemplate(w, "layout", data)` for pages or `frags.ExecuteTemplate(w, "fragment-NAME", data)` for fragments

This pattern avoids Go template's global `define` namespace issue.

**Custom FuncMap helpers** (defined in `router.go`):

- `add a b` — integer addition (`{{add .OpenCount .ClosedCount}}`)
- `percent part total` — integer percentage, 0 when total=0 (`{{percent .ClosedCount $total}}`)

Page templates cannot call fragment templates directly (they are in separate template sets). Fragment templates are only used as HTMX swap responses from handlers. Inline the shared HTML in page templates if needed.

## PostgreSQL Notes

- Use `$N` numbered placeholders (not `?`)
- Use `BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY` (not `INTEGER PRIMARY KEY AUTOINCREMENT`)
- Use `TIMESTAMPTZ NOT NULL DEFAULT NOW()` (not `DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP`)
- Use `INSERT INTO ... ON CONFLICT DO NOTHING` (not `INSERT OR IGNORE INTO`)
- `LastInsertId()` is not supported — use `RETURNING id` with `QueryRowContext().Scan()`

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

## Code Browser & Refs

See [docs/code-browser.md](./docs/code-browser.md) for full CodeService API, URL patterns, ResolveRef priority, result types, and Branch/Tag management endpoints.

- Routes: `tree/{ref}`, `blob/{ref}`, `blame/{ref}`, `commits/{ref}`, `commit/{sha}`, `refs`
- `CodeService` in `internal/service/code_service.go` — no store dependency, reads bare repos via go-git
- `ErrEmptyRepo` sentinel → 404 when repo has no commits
- Write access required for branch/tag create/delete; default branch delete is blocked

## Deployment

See [docs/deployment.md](./docs/deployment.md) for Docker setup, env var reference, and first-run bootstrap.

- Build: `make build` → single binary `dist/cloudzilla` (embedded templates + CSS)
- Key env vars: `CZ_DATABASE_DSN`, `CZ_AUTH_JWT_SECRET`, `CZ_GIT_REPOS_ROOT`, `CZ_GIT_SSH_HOST_KEY`
- Docker: `make docker-build && make docker-run`; first-run: `docker exec ... cloudzilla-cli migrate`

## Pull Request Merge Strategies

See [docs/pr-merge.md](./docs/pr-merge.md) for full CodeService merge API and PRDiffResult type.

- Three strategies: fast-forward (`ff`), three-way merge (`merge`), squash (`squash`)
- `PATCH /api/repos/{owner}/{repo}/pulls/{number}` with `state=merged&merge_strategy=ff|merge|squash`
- `mergeTreesNoConflict` detects file-level conflicts → hides all merge buttons, shows warning
- Diff view shown for open PRs only; omitted for closed/merged

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

---

## Organizations

See [docs/organizations.md](./docs/organizations.md) for OrgService API + full endpoint table.

- Roles: `owner` (full admin: members + repos), `member` (view-only)
- `/{org}` profile page (repos + members); `/orgs/{org}/settings` for owners
- `/{owner}` route checks user first, falls back to org lookup
- `OrgService` in `internal/service/org_service.go`

---

## Webhooks

See [docs/webhooks.md](./docs/webhooks.md) for WebhookService API + full endpoint table.

- Events: `push`, `issues`, `pull_request`; HMAC-SHA256 signed if secret set
- Dispatch: `go s.Webhook.Dispatch(repoID, event, payload)` (fire-and-forget)
- Extend: add event string + `XPayload()` to `webhook_service.go`; no schema change
- Repo settings page (`/{owner}/{repo}/settings`) shows Collaborators + Webhooks + Transfer sections

---

## Notifications

See [docs/notifications.md](./docs/notifications.md) for NotificationService API + pages/API table.

- Types: `issue_comment`, `pr_comment`, `issue_closed`, `issue_reopened`, `pr_merged`, `pr_closed`
- Never fire when `actorID == authorID` (self-actions are silent)
- Extend: add const to `model/notification.go`, add `NotifyX` to `notification_service.go`
- `basePage()` calls `CountUnread` on every render → `BasePage.UnreadNotifCount` badge in navbar

---

## Cloudzilla Feature Roadmap

Full phase specs (all phases, implemented and planned): [docs/roadmap.md](./docs/roadmap.md)

**Cross-cutting rules (all phases):**

- No new Go dependencies needed
- Notification extension: add const to `model/notification.go`, add `NotifyX` to `notification_service.go`
- Webhook extension: add event string + `XPayload()` to `webhook_service.go`; no schema change
- PostgreSQL: `$N` placeholders, `RETURNING id`, `sql.NullInt64` for nullable FK (see `repo_store.go`)
- Batch SQL `IN (...)`: `strings.Join` with numbered `$N` params
- HTMX assignee/label API: POST body, DELETE query param

**Critical files touched by every phase:**

- `internal/router/router.go` — register routes + add page names to `pageNames`
- `internal/handler/viewmodels.go` — add data structs for new pages/fragments
- `internal/service/services.go` — wire new service into `Services` struct + `New()`
- `internal/store/stores.go` — wire new store into `Stores` struct + `New()`
- `internal/handler/page_handler.go` — extend existing page handlers with new data fetches

| Phase | Feature                             | Status     | Migration(s) |
| ----- | ----------------------------------- | ---------- | ------------ |
| 0.1   | Core Platform                       | ✅ Done    | 001–007      |
| 0.2   | OAuth & Organizations               | ✅ Done    | 008–010      |
| 0.3   | Webhooks, Notifs, Admin             | ✅ Done    | 011–015      |
| 1.1   | Labels                              | ✅ Done    | 016          |
| 1.2   | Assignees                           | ✅ Done    | 017          |
| 1.3   | Stars                               | ✅ Done    | 018          |
| 2     | Repository Fork                     | ✅ Done    | 019          |
| 3.1   | Releases                            | ✅ Done    | 020          |
| 3.2   | Commit Status API                   | ✅ Done    | 021          |
| 3.3   | Milestones                          | ✅ Done    | 022          |
| 4.1   | PR Reviews                          | ✅ Done    | 023          |
| 4.2   | PR Line Comments                    | ✅ Done    | 024          |
| 4.3   | Search                              | ✅ Done    | 025          |
| —     | Bug fixes (Phase 0–4)               | ✅ Done    | 026          |
| 5–20  | Personal Access Tokens → GraphQL v2 | ⬜ Planned | 027–060      |

> Full specs for all planned phases (5–20): [docs/roadmap.md](./docs/roadmap.md)
