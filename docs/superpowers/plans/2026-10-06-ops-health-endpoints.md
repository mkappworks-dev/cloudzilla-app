# Health endpoints — Implementation Plan

> Execute with superpowers:subagent-driven-development: one task at a time, test first, then a review pass over the whole diff.

**Goal:** `/healthz` (liveness) and `/readyz` (readiness) answer probes ahead of all middleware, and the Docker image reports its own health.

**Architecture:** `db.Pending` → `store.HealthStore` (ping, pending migrations) → `service.HealthService` (three checks under one deadline, cached migrations pass) → `handler.Healthz`/`Readyz` → `probeMux`, which embeds `*chi.Mux` and intercepts the two exact paths before the router's middleware.

**Spec:** [`.scratch/ops-health-metrics-backup/issues/01-health.md`](../../../.scratch/ops-health-metrics-backup/issues/01-health.md)

## Global Constraints

- Stores → services → handlers. The handler calls `Services.Health` only.
- The `/readyz` body never carries error text; causes go to `slog.Warn`.
- `router.New` must still satisfy `chi.Routes`: the reserved-names test walks it.
- No new dependencies. Comments state only a *why*, in one line.
- Stage files by explicit path. Never stage `.claude/`.

## Task 1 — `db.Pending`

- [x] Integration tests in `internal/db/pending_test.go`: migrated schema → none; deleted `101_user_code_themes` row → that version; dropped `schema_migrations` → every version, sorted.
- [x] Extract `migrationFiles(fs.FS)` from `runMigrations`; `Pending` uses it and `to_regclass('schema_migrations')` to detect a missing table.

## Task 2 — `HealthStore` and `HealthService`

- [x] Unit tests with a stub `HealthProbe`: all ok; database down → `database: fail`, `migrations: skipped`, no migrations query; pending → fail, then ok once applied; a passing migrations check isn't queried again; query error → fail; missing and read-only repos root → `storage: fail`; no `.readyz-*` left behind.
- [x] `store.HealthStore{Ping, PendingMigrations}`, wired in `store.New`.
- [x] `service.HealthService.Readiness(ctx)` with a 3 s `context.WithTimeout`, an `atomic.Bool` for the migrations cache and `os.CreateTemp(root, ".readyz-*")` for storage. Wired as `Services.Health`.

## Task 3 — handlers and dispatch

- [x] Router tests with a nil database and a stub probe: `/healthz` 200 `ok` text/plain, no-store, no cookie, no `request` log line; `/readyz` 200/503 JSON without error text; HEAD 200; other methods 405 with `Allow: GET, HEAD`; 200 requests from one IP all 200.
- [x] Integration tests on a fresh schema with no users: `/` redirects to `/setup` while both probes answer 200; `/healthz/anything` gets the router's setup redirect.
- [x] `handler.Healthz`, `handler.Readyz`; `probeMux` returned from `router.New`.

## Task 4 — reserved names, Docker, docs

- [x] `healthz`, `readyz` in `reservedOwnerNames`; `TestValidateOwnerName` covers mixed case.
- [x] Dockerfile `HEALTHCHECK` on `/healthz` with busybox `wget` and `${CZ_SERVER_PORT:-8080}`.
- [x] `docs/deployment.md`: "Health checks" section and an "Upgrading" note with the SQL to find existing owners; `docs/access-control.md` reserved list.

## Task 5 — verify

- [x] `gofmt`, `go vet`, `golangci-lint` v2.14.0, `go test ./...` with `TEST_DATABASE_DSN`.
- [x] Run the built server against a scratch database: 503 before `cloudzilla-cli migrate`, 200 after; `storage: fail` with the repos root moved away; `/healthz` 200 and `/readyz` 503 at the 3 s deadline with Postgres frozen.
- [ ] `docker build` + `docker inspect --format '{{.State.Health.Status}}'`: needs Docker.
