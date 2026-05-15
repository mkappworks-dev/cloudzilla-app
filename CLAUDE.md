# CLAUDE.md — Cloudzilla Developer Guide

> ⚠️ **Alpha**: APIs, project structure, and conventions may change.

## Architecture

- **Backend**: Go 1.23+, chi router, sqlx, go-git, cobra CLI
- **Frontend**: [Templ](https://templ.guide/) (type-safe Go HTML components), HTMX for partial updates, Tailwind CSS
- **DB**: PostgreSQL
- **Pattern**: Stores → Services → Handlers (strict layer separation)
- **Rendering**: Server-driven; no JS framework; Templ compiles to Go
- **Git Transport**: HTTP smart protocol + SSH server (both pure Go, no git binary required)
- **SSH Auth**: Public key auth via stored SSH keys

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
- `internal/router/` — chi route registration
- `internal/ssh/` — SSH server for git operations (gliderlabs/ssh)
- `internal/view/` — Templ components (compiled to `_templ.go`)
  - `layout/` base layout, `pages/` page components, `fragments/` HTMX fragments
- `migrations/` — SQL files, embedded via embed.FS
- `cmd/server/frontend/static/` — `main.css` (compiled Tailwind), `htmx.min.js`
- `tailwind/` — `input.css`, `tailwind.config.js`

## Dev Commands

```bash
make setup-tailwind     # Download Tailwind CLI (one-time)
make download-mermaid   # Download mermaid.min.js (auto-runs in build/dev)
make download-htmx      # Download htmx.min.js (auto-runs in build/dev)
make build-css          # Compile Tailwind → static/main.css
make dev                # Run server + Tailwind watch
make migrate            # Run DB migrations
make build              # Build Go binary (embedded templates + CSS)
make lint               # Lint Go code
go test ./...           # Run Go tests
```

## Code Conventions

### Comments

- Default to **no comment**. Add one only when the *why* is non-obvious: a hidden constraint, a subtle invariant, a workaround for a specific bug, or behavior that would surprise a reader.
- Do **not** narrate *what* the code does — names and types already do that.
- Do **not** reference the current task, PR, or recent commit ("added for X flow", "see issue #123"). Those belong in the commit message.
- Single-line `// ...` is the default; multi-line block comments and multi-paragraph docstrings are usually a sign the comment is over-explaining.
- When editing existing code, prefer **deleting** stale or explanatory comments over preserving them.

### Go Handlers

- **Page handlers** fetch data and call `h.render(page, data)` to render full pages
- **HTMX handlers** check `r.Header.Get("HX-Request") == "true"` and call `h.renderFragment(name, data)`
- **API handlers** return JSON via `writeJSON(w, status, v)`
- Handlers call services only, never stores directly
- `context.Context` is the first arg of every service/store method
- JWT read from `Authorization: Bearer` header OR `cz_token` httpOnly cookie

### Templates (Templ)

- **Layout**: `layout.Base(title, unreadCount, user)` in `internal/view/layout/`
- **Pages** live in `internal/view/pages/`; call `layout.Base(...)` with content as child
- **Fragments** in `internal/view/fragments/`; rendered via `component.Render(ctx, w)`
- HTMX attrs go on HTML elements: `hx-post`, `hx-target`, `hx-swap`
- Templ auto-escapes output; use `templ.Raw(...)` only for trusted HTML (e.g. rendered Markdown)
- After editing `.templ` files run `~/go/bin/templ generate`
- **Never manually edit `_templ.go` files** — they are generated
- **Keep `{{if}}` inside `class`/`style` attributes on one line** — VS Code's HTML formatter splits string literals and breaks template comparisons

### CSS (Tailwind)

- Tailwind utilities only; no custom CSS
- Build: `make build-css` (runs before `make dev` and `make build`)
- Config in `tailwind/tailwind.config.js` — update `content` glob when adding template dirs
- Output: `cmd/server/frontend/static/main.css` (gitignored)

## Adding a New Feature

1. Add SQL migration in `migrations/` (next sequential number)
2. Add/update model struct in `internal/model/`
3. Add store method in `internal/store/` — wire into `Stores` struct in `stores.go`
4. Add service method in `internal/service/` — wire into `Services` struct in `services.go`
5. Add handler in `internal/handler/` (extend `page_handler.go` for new page data)
6. Add view-model struct to `internal/handler/viewmodels.go`
7. Register route in `internal/router/router.go`; add page name to `pageNames` slice if new page
8. If route must be accessible pre-setup (e.g. public assets), add to allowlist in `middleware/setup.go`
9. Add/update Templ component in `internal/view/`; run `templ generate`
10. Add Tailwind classes; add fragments if using HTMX swaps

## Authentication Flow

1. **Form login**: POST `/login` → `User.Authenticate()` → httpOnly cookie → redirect `/`
2. **API login**: POST `/api/auth/login` (JSON) → JWT in cookie + JSON body
3. **Protected pages**: `optAuthMW` reads cookie, injects claims into context
4. **HTMX**: browser auto-includes cookie (same-origin)
5. **Google OAuth**: `GET /auth/google` → callback upserts user (links by `oauth_id` then email) → `cz_token` cookie. Config: `oauth.google_client_id/secret/redirect_url`. Missing client_id → 501.

## PostgreSQL Notes

- `$N` numbered placeholders (not `?`)
- `BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY` (not `INTEGER ... AUTOINCREMENT`)
- `TIMESTAMPTZ NOT NULL DEFAULT NOW()` (not `DATETIME ... DEFAULT CURRENT_TIMESTAMP`)
- `INSERT ... ON CONFLICT DO NOTHING` (not `INSERT OR IGNORE`)
- No `LastInsertId()` — use `RETURNING id` with `QueryRowContext().Scan()`
- Nullable FK: `sql.NullInt64` (see `repo_store.go`)
- Batch `IN (...)`: `strings.Join` with numbered `$N` params

## Subsystem Docs

Detailed APIs, endpoint tables, and flows live under `docs/`. Read these before working in the matching area:

- [docs/git-transport.md](./docs/git-transport.md) — HTTP/SSH endpoints, SSH auth, permission rules (`CanRead`/`CanWrite`/`CanManage`/`IsOwner`)
- [docs/access-control.md](./docs/access-control.md) — Instance/org/repo role tables, setup flow, invite tokens
- [docs/htmx-patterns.md](./docs/htmx-patterns.md) — HTMX handler example, template parse order, FuncMap helpers
- [docs/code-browser.md](./docs/code-browser.md) — `CodeService` API, ref resolution priority. `ErrEmptyRepo` sentinel → 404 when repo has no commits
- [docs/pr-merge.md](./docs/pr-merge.md) — `ff` / `merge` / `squash` strategies. `mergeTreesNoConflict` detects file-level conflicts and hides merge buttons
- [docs/organizations.md](./docs/organizations.md) — `OrgService` API. `/{owner}` route checks user first, falls back to org
- [docs/webhooks.md](./docs/webhooks.md) — Events, HMAC-SHA256 signing, `Dispatch` is fire-and-forget
- [docs/notifications.md](./docs/notifications.md) — Notification types. Never fire when `actorID == authorID`
- [docs/deployment.md](./docs/deployment.md) — Docker, env vars (`CZ_DATABASE_DSN`, `CZ_AUTH_JWT_SECRET`, `CZ_GIT_REPOS_ROOT`, `CZ_GIT_SSH_HOST_KEY`), bootstrap
- [docs/ROADMAP.md](./docs/ROADMAP.md) — Phase status, milestone structure, planned work
- [docs/superpowers/plans/](./docs/superpowers/plans/) — Per-phase implementation plans

## Branch Naming

```
<type>/phase-<number>-<slug>
```

`type` is `feat`, `bug`, or `tech`. Example: `feat/phase-12.3-discussions`.
