# CLAUDE.md — Cloudzilla

> ⚠️ **Alpha**: APIs, project structure, and conventions may change.

Self-hosted git forge. Go (chi, sqlx, go-git, cobra) + PostgreSQL; UI is server-rendered [Templ](https://templ.guide/) with HTMX, Alpine.js, and Tailwind. Git transport (HTTP smart protocol + SSH) is pure Go — no `git` binary.

## Layering

Stores → Services → Handlers, strictly:

- `internal/store/` holds raw SQL; `internal/service/` holds business logic; `internal/handler/` calls services only.
- `context.Context` is the first arg of every store and service method.
- Handlers render with `h.render(w, r, component)` — a page from `internal/view/pages/` or a fragment from `internal/view/fragments/`. HTMX requests are detected with `r.Header.Get("HX-Request") == "true"`. JSON APIs use `writeJSON(w, status, v)`.
- View-model structs live in `internal/view/viewmodels_*.go`; pages embed `BasePage` from `basePage(r, h.Services)`.

## Commands

Targets live in the `Makefile` (`dev`, `build`, `migrate`, `lint`, `test`, `test-integration`). Integration tests need `TEST_DATABASE_DSN`; `make test-integration` starts the test DB and sets it.

## Templ

- Edit `.templ` files, then run `make generate-templ`. `_templ.go` files are generated output — regenerate them, keep hands off.
- Keep `{{if}}` inside `class`/`style` attributes on one line: VS Code's HTML formatter splits the string literal and breaks the comparison.
- Use `templ.Raw(...)` only for trusted HTML such as rendered Markdown.

## CSS

Tailwind utilities only. When adding a template directory, extend the `content` glob in `tailwind/tailwind.config.js`.

## Adding a feature

1. Migration in `internal/db/migrations/` (next sequential number).
2. Model in `internal/model/`.
3. Store method; wire into `Stores` in `internal/store/stores.go`.
4. Service method; wire into `Services` in `internal/service/services.go`.
5. Handler in `internal/handler/` (full pages go in `page_*_handler.go`).
6. View-model in `internal/view/viewmodels_*.go`; Templ component in `internal/view/`.
7. Route in `internal/router/router.go`. A route reachable before setup completes must also be added to the path check in `internal/middleware/setup.go`.

## PostgreSQL

Get new IDs via `RETURNING id` + `QueryRowContext().Scan()`. Nullable FKs use `sql.NullInt64`. Batch `IN (...)` is built with `strings.Join` over numbered `$N` params.

## Comments

Write a comment only for the _why_ the code can't show: a hidden constraint, an invariant, a workaround, a surprise. One line by default. Task, PR, and history context goes in the commit message. When editing, delete stale comments.

## Subsystem docs

Read the matching doc before working in an area:

- [git-transport](./docs/git-transport.md) — HTTP/SSH endpoints, SSH key auth, `CanRead`/`CanWrite`/`CanManage`/`IsOwner`
- [access-control](./docs/access-control.md) — instance/org/repo roles, setup flow, invite tokens, login + Google OAuth
- [api-reference](./docs/api-reference.md) — JSON API endpoints
- [configuration](./docs/configuration.md) — config keys and `CZ_*` env vars
- [deployment](./docs/deployment.md) — Docker, bootstrap
- [code-browser](./docs/code-browser.md) — `CodeService`, ref resolution, `ErrEmptyRepo` → 404
- [pr-merge](./docs/pr-merge.md) — `ff`/`merge`/`squash`, conflict detection
- [organizations](./docs/organizations.md) — `OrgService`; `/{owner}` resolves user first, then org
- [webhooks](./docs/webhooks.md) — events, HMAC signing, fire-and-forget `Dispatch`
- [notifications](./docs/notifications.md) — types; skipped when `actorID == authorID`
- [ui-overhaul-class-map](./docs/ui-overhaul-class-map.md) — mockup → shadcn class map; the source of truth when porting UI
- [ROADMAP](./docs/ROADMAP.md) and [plans](./docs/superpowers/plans/) — phase status and per-phase plans

## Branches

`<type>/<slug>`, where `type` is `feat`, `fix` (or `bug`), or `tech`. Prefix the slug with `phase-<n>-` when the work belongs to a roadmap phase — e.g. `feat/phase-12.3-discussions`, `fix/contributor-stats-sha-dedup`.

Worktrees go in `.worktrees/<type>+<slug>` (gitignored), mirroring the branch: `git worktree add .worktrees/fix+foo -b fix/foo`.

## Pull requests

- Title: Conventional Commits, enforced by `.github/workflows/pr-title-lint.yml` — e.g. `feat(ui): UI overhaul phase 8 — dashboard`.
- Body: fill in [`.github/pull_request_template.md`](./.github/pull_request_template.md), ticking only the checklist items that apply.
