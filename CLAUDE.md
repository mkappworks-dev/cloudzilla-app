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
7. Add/update HTML template in `cmd/server/frontend/templates/`
8. Add Tailwind CSS classes to template
9. Add fragment templates if using HTMX swaps

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
  ssh_port: 2222                    # SSH server port
  ssh_host_key: ./cloudzilla_host_key  # Host key file (auto-generated if missing)
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
  <button hx-patch="/api/repos/{{.Owner}}/{{.Repo}}/issues/{{.Issue.Number}}"
          hx-vals='{"state":"closed"}' hx-target="#issue-detail" hx-swap="outerHTML">
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
- `RepoService.CanRead(ctx, repo, userID)` — checks public/private + permissions
- `RepoService.CanWrite(ctx, repo, userID)` — checks write permissions
- Both HTTP handlers and SSH handlers call these methods before processing git commands
- Bare repository created with `go-git.PlainInit()`, fully compatible with git CLI

## Code Browser

### URL Patterns

| View | Route |
|---|---|
| Root tree | `/{owner}/{repo}/tree/{ref}` |
| Subtree | `/{owner}/{repo}/tree/{ref}/{path...}` |
| Blob | `/{owner}/{repo}/blob/{ref}/{path...}` |
| Blame | `/{owner}/{repo}/blame/{ref}/{path...}` |
| Commit log | `/{owner}/{repo}/commits/{ref}` |
| Commit log (file scope) | `/{owner}/{repo}/commits/{ref}/{path...}` |
| Single commit diff | `/{owner}/{repo}/commit/{sha}` |

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

## Deployment

1. Run `make build` — produces single `dist/cloudzilla` binary with embedded templates + CSS
2. No Node.js, npm, or Bun needed
3. `./dist/cloudzilla` runs the server on the configured port
4. Set config via `config.yaml` or environment variables (see `internal/config/`)

## Out of Scope (v1)

- Webhooks / notifications
- OAuth providers other than Google (GitHub, GitLab, etc.)
- Organization accounts
- Git branches UI (clone works, but branch/tag management is CLI-only)
- Pull request merging via git commands (web UI only)
