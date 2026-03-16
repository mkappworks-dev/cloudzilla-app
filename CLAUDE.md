# CLAUDE.md — Cloudzilla Developer Guide

## Architecture

- **Backend**: Go 1.23+, chi router, sqlx, go-git, cobra CLI
- **Frontend**: Go `html/template`, HTMX for partial updates, Tailwind CSS
- **DB**: SQLite (default), PostgreSQL (production)
- **Pattern**: Stores → Services → Handlers (strict layer separation)
- **Rendering**: Server-driven; no JavaScript framework, no build tooling needed

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

## Deployment

1. Run `make build` — produces single `dist/cloudzilla` binary with embedded templates + CSS
2. No Node.js, npm, or Bun needed
3. `./dist/cloudzilla` runs the server on the configured port
4. Set config via `config.yaml` or environment variables (see `internal/config/`)

## Out of Scope (v1)

- Git HTTP smart protocol (push/pull)
- SSH server
- Code diff rendering
- Webhooks / notifications
- OAuth / SSO
- Organization accounts
