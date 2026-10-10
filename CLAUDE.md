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

Targets live in the `Makefile` (`dev`, `build`, `migrate`, `seed`, `lint`, `test`, `test-integration`). Integration tests need `TEST_DATABASE_DSN`; `make test-integration` starts the test DB and sets it.

## Templ

- Edit `.templ` files, then run `make generate-templ`. `_templ.go` files are generated output — regenerate them, keep hands off.
- Keep `{{if}}` inside `class`/`style` attributes on one line: VS Code's HTML formatter splits the string literal and breaks the comparison.
- Use `templ.Raw(...)` only for trusted HTML such as rendered Markdown.

## CSS

Tailwind v4 utilities only. `tailwind/input.css` is the whole config: `@theme` maps the tokens to utilities, and `@source` scans only `internal/view/**/*.templ`, so a class that appears only in Go code isn't generated. Templates outside `internal/view/` need their own `@source`.

`space-y-*` and `divide-y` style every child but the last: keep a rendered element last (hidden inputs, `<script>` and `<dialog>` go first or outside), and use `flex flex-col gap-*` when rows toggle with `x-show`.

## Static assets

`cmd/server/frontend/` is embedded and served by `internal/assets`. Templates reference a file as `assets.URL("/static/x.js")`, never as a literal path: the `?v=<content hash>` query is what lets browsers cache it for a year.

## Adding a feature

1. Migration in `internal/db/migrations/` (next sequential number).
2. Model in `internal/model/`.
3. Store method; wire into `Stores` in `internal/store/stores.go`.
4. Service method; wire into `Services` in `internal/service/services.go`.
5. Handler in `internal/handler/` (every `Page*` method goes in `<concern>_page_handler.go`; JSON and form-`POST` handlers stay in `<concern>_handler.go`).
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
- [sso](./docs/sso.md) — LDAP bind and SAML assertion checks, `findOrProvisionUser` linking rules, `/admin/sso` settings
- [wiki](./docs/wiki.md) — `<repo>.wiki.git`, slug rule and `.order`, CanWrite/CanManage, quota and `409`
- [cli](./docs/cli.md) — the `cz` remote client: install, token storage, scopes per command, what it can't do
- [api-reference](./docs/api-reference.md) — JSON API endpoints
- [configuration](./docs/configuration.md) — config keys and `CZ_*` env vars
- [deployment](./docs/deployment.md) — Docker, bootstrap
- [code-browser](./docs/code-browser.md) — `CodeService`, ref resolution, `ErrEmptyRepo` → 404
- [pr-merge](./docs/pr-merge.md) — `ff`/`merge`/`squash`, conflict detection
- [repo-import](./docs/repo-import.md) — background clone jobs, SSRF guard on go-git's HTTP client, `import.*` config
- [repo-mirrors](./docs/repo-mirrors.md) — pull mirrors: `MirrorService` sync and lease-based scheduler, read-only guard, sealed tokens, `mirror.*` config
- [storage](./docs/storage.md) — local/S3 `Backend`, object layout and backup, avatar processing and `/avatars/*` serving, `components.Avatar`'s context lookup
- [organizations](./docs/organizations.md) — `OrgService`; `/{owner}` resolves user first, then org
- [webhooks](./docs/webhooks.md) — events, HMAC signing, fire-and-forget `Dispatch`
- [notifications](./docs/notifications.md) — types; skipped when `actorID == authorID`
- [ui-overhaul-class-map](./docs/ui-overhaul-class-map.md) — mockup → shadcn class map and Tailwind v3 → v4 names; the source of truth when porting UI
- [testing](./docs/testing.md) — what CI runs, why `backup` and `seed` are local-only, `make test-integration`
- [ROADMAP](./docs/ROADMAP.md) — phase status

## Branches

`<type>/<slug>`, where `type` is `feat`, `fix` (or `bug`), or `tech`. Prefix the slug with `phase-<n>-` when the work belongs to a roadmap phase — e.g. `feat/phase-12.3-discussions`, `fix/contributor-stats-sha-dedup`.

Worktrees go in `<main checkout>/.worktrees/<type>+<slug>` (gitignored), mirroring the branch: `git worktree add .worktrees/fix+foo -b fix/foo`. From inside another worktree (e.g. one under `.claude/worktrees/`), pass the main checkout's absolute path, or the new one nests inside that worktree.

## Pull requests

- Title: Conventional Commits, enforced by `.github/workflows/pr-title-lint.yml` — e.g. `feat(ui): UI overhaul phase 8 — dashboard`.
- Body: fill in [`.github/pull_request_template.md`](./.github/pull_request_template.md), ticking only the checklist items that apply and striking through the rest (`- [ ] ~~item~~`) so a reviewer can tell "not applicable" from "forgot".

## Agent skills

### Issue tracker

Before creating, reading, triaging or finishing a spec or ticket (markdown under `.scratch/`), read `docs/agents/issue-tracker.md`.

### Workflow

`/to-spec` → `/to-tickets` → `/triage` → `/implement`, from the `mattpocock-skills` plugin. Step table and install command: [CONTRIBUTING.md](./CONTRIBUTING.md#planning-workflow-claude-code).

### Triage labels

New work enters through the tracker as a spec and tickets under `.scratch/`. `/to-spec` and `/to-tickets` output is already `ready-for-agent`; run `/triage` on anything else (a bug, a rough idea, a ticket with no `Status:`) before any code.

When setting a triage role, write it on the ticket's `Status:` line as `needs-triage`, `needs-info`, `ready-for-agent`, `ready-for-human` or `wontfix`. See `docs/agents/triage-labels.md`.

### Domain docs

Before using the domain glossary (`CONTEXT.md`) or ADRs (`docs/adr/`), read `docs/agents/domain.md`.
