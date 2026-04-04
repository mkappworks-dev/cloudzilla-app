# CLAUDE.md — Cloudzilla Developer Guide

> ⚠️ **Alpha**: Cloudzilla is under active development. APIs, project structure, and conventions may change as the project matures.

## Architecture

- **Backend**: Go 1.23+, chi router, sqlx, go-git, cobra CLI
- **Frontend**: [Templ](https://templ.guide/) (type-safe Go HTML components), HTMX for partial updates, Tailwind CSS
- **DB**: PostgreSQL (default and production)
- **Pattern**: Stores → Services → Handlers (strict layer separation)
- **Rendering**: Server-driven; no JavaScript framework; Templ components compile to Go code
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
- `internal/router/` — chi route registration
- `internal/ssh/` — SSH server for git operations (gliderlabs/ssh)
- `internal/view/` — Templ components (compiled to `_templ.go` files)
  - `internal/view/layout/` — Base layout component
  - `internal/view/pages/` — Page components (one per page)
  - `internal/view/fragments/` — HTMX fragment components
- `migrations/` — SQL files, embedded via embed.FS
- `cmd/server/frontend/` — Static files
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

### Templates (Templ)

- **Layout**: `layout.Base(title, unreadCount, user)` component in `internal/view/layout/`
- **Pages**: Each page is a `templ` component in `internal/view/pages/`; call `layout.Base(...)` and pass content as a child component
- **Fragments**: Fragment components live in `internal/view/fragments/`; rendered directly via `component.Render(ctx, w)`
- HTMX attributes go on HTML elements: `hx-post="/api/..."`, `hx-target="#id"`, `hx-swap="outerHTML"`
- Templ auto-escapes all output; use `templ.Raw(...)` only for trusted HTML (e.g. rendered Markdown)
- Regenerate Go code after editing `.templ` files: `~/go/bin/templ generate`
- **Never manually edit `_templ.go` files** — they are generated; only run `templ generate` to update them

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

**Google OAuth:** `GET /auth/google` → Google → callback upserts user → `cz_token` cookie. Links by `oauth_id` then email. Config: `oauth.google_client_id/secret/redirect_url`. Missing client_id → 501.

## Git Transport & Permissions

See [docs/git-transport.md](./docs/git-transport.md) for full endpoints, config, curl examples, SSH auth flow, and permission rules.

- HTTP: `GET /{owner}/{repo}/info/refs`, `POST .../git-upload-pack`, `POST .../git-receive-pack`
- SSH: port 2222; public key auth via `ssh_keys` + `deploy_keys` tables (MD5 fingerprint)
- `RepoService.CanRead` — public repos always pass; private require auth + any role
- `RepoService.CanWrite` — owner, org owner, or `writer`/`admin` role
- `RepoService.CanManage` — owner or org owner only (not `admin` collaborator)
- `RepoService.TransferRepo` — personal repos only; moves git dir on disk

## HTMX & Template Patterns

See [docs/htmx-patterns.md](./docs/htmx-patterns.md) for full example and template parse sequence.

- HTMX handlers check `r.Header.Get("HX-Request") == "true"` → call `h.renderFragment(name, data)`
- Templates parsed at startup in `router.mustParseTemplates()`; pages and fragments are separate sets — inline shared HTML in page templates when needed
- FuncMap helpers: `add a b` (int addition), `percent part total` (0-safe integer %)
- **Keep all `{{if}}` inside `class`/`style` attributes on a single line** — the VS Code HTML formatter inserts leading spaces into split string literals, breaking template comparisons

## PostgreSQL Notes

- Use `$N` numbered placeholders (not `?`)
- Use `BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY` (not `INTEGER PRIMARY KEY AUTOINCREMENT`)
- Use `TIMESTAMPTZ NOT NULL DEFAULT NOW()` (not `DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP`)
- Use `INSERT INTO ... ON CONFLICT DO NOTHING` (not `INSERT OR IGNORE INTO`)
- `LastInsertId()` is not supported — use `RETURNING id` with `QueryRowContext().Scan()`

## Instance Permissions & Access Control

See [docs/access-control.md](./docs/access-control.md) for full tables and flows.

- Three levels: instance (`superadmin`/`user`), org (`owner`/`member`), repo (`reader`/`writer`/`admin`)
- First-run `/setup` → first submitter becomes superadmin; `RequireSetup` middleware redirects all routes until done
- `allow_registration` / `allow_login` site settings; invite tokens bypass both
- `CanManage` — owner or org owner only (not `admin` collaborator); gates collaborator CRUD
- HTMX responses swap `fragment-repo-collaborators` into `#repo-collaborators`

---

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

| Phase   | Feature                                      | Status     | Migration(s) |
| ------- | -------------------------------------------- | ---------- | ------------ |
| 0.1–5.3 | Core Platform → Draft PRs (all done)         | ✅ Done    | 001–029      |
| 6.1–6.3 | Protected Branches → Code Review Suggestions | ✅ Done    | 030–031      |
| 7.1     | Auto-merge                                   | ✅ Done    | 032          |
| 7.2–7.3 | Issue & PR Templates, Reactions              | ✅ Done    | 033          |
| 8.1     | TOTP Two-Factor Authentication               | ✅ Done    | 034          |
| 8.2     | Audit Log                                    | ✅ Done    | 035          |
| 8.3     | LDAP / SAML SSO                              | ✅ Done    | 036–037      |
| 9.1     | Project Boards / Kanban                      | ✅ Done    | 036          |
| 9.2     | Wiki                                         | ✅ Done    | —            |
| 9.3     | Issue Pinning & Locking                      | ✅ Done    | 038          |
| 10.1    | Repository Insights & Stats                  | ✅ Done    | —            |
| 10.2    | @Mentions in Comments                        | ✅ Done    | 039          |
| 10.3    | Saved Replies                                | ✅ Done    | 040          |
| 11.1    | Email Notifications (SMTP)                   | ✅ Done    | 041          |
| 11.2    | OAuth Apps / Third-party Clients             | ✅ Done    | 042          |
| 11.3–20 | (next planned phases)                        | ⬜ Planned | 043–060      |

> Full specs for all planned phases (5–20): [docs/roadmap.md](./docs/roadmap.md)
> Implementation plans for each phase: [docs/superpowers/plans/](./docs/superpowers/plans/)

## Branch Naming Convention

When starting work on a new phase or task, create a branch following this format:

```
<type>/phase-<number>-<name-of-plan>
```

**Types:** `feat` (new feature), `bug` (bug fix), `tech` (technical/infrastructure)

**Examples:**

- `feat/phase-8.2-audit-log`
- `feat/phase-9.1-project-boards`
- `feat/phase-12.3-discussions`
- `bug/phase-8.1-totp-recovery-fix`
- `tech/phase-8.3-sso`

## Running Phases Individually

Each phase (8.2–15.3) has a self-contained implementation plan in `docs/superpowers/plans/`. Phases should be implemented **sequentially** (not in parallel) because:

1. Migration numbers must be sequential (035, 036, ...)
2. Shared files (`router.go`, `services.go`, `stores.go`) would conflict
3. Some phases depend on earlier schema

**To implement a phase in a standalone Claude session:**

```bash
claude -p "Read the plan at docs/superpowers/plans/2026-03-25-phase-<X.Y>-<name>.md and implement it. \
Create branch feat/phase-<X.Y>-<name> from main. Follow all conventions in CLAUDE.md. \
Use the next available migration number. Commit when done."
```

**After each phase completes — required workflow before merging:**

1. **Security review** — run the `pr-review-toolkit:silent-failure-hunter` agent against the branch to check for silent failures, missing error handling, and security issues. Fix any high-confidence findings before opening a PR.
2. **Open a pull request** — do not merge directly to main. Create a PR with `gh pr create` so the diff is visible for review.
3. **Merge after approval** — once the PR is reviewed, merge to main before starting the next phase.
