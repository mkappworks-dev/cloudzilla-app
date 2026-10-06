# Liveness and readiness endpoints, and a Docker HEALTHCHECK

Created: 2026-10-06
Category: enhancement
Status: ready-for-agent
Spec: [../spec.md](../spec.md) (Health)

## What to build

**Liveness: `GET|HEAD /healthz`.** Returns `200 ok` as `text/plain` with `Cache-Control: no-store`. No database or disk access.

**Readiness: `GET|HEAD /readyz`.** Returns 200 or 503 with `{"status":"ok|fail","checks":{"database":…,"migrations":…,"storage":…}}`. Each check reports `ok`, `fail` or `skipped`. All checks share one 3-second deadline, and failure details go to the log at warn, never into the body.

- **`database`:** `PingContext`.
- **`migrations`:** a new `db.Pending(ctx, db) ([]string, error)` in `internal/db/migrate.go` shares the embedded file list with `runMigrations`. A missing `schema_migrations` table counts as all pending. Once the check passes, the result is cached for the process. It reports `skipped` when the database check failed.
- **`storage`:** create, write and remove a `.readyz-*` temp file in `git.repos_root`.

The checks live behind a service, so the handler only calls services.

**Dispatch.** A wrapper around the chi router answers the exact paths `/healthz` and `/readyz` before any global middleware runs: no `Logger` line, no CSRF cookie, no `RequireSetup` redirect, no auth, no rate limit. Other methods get 405. Every other path, including `/healthz/x`, goes on to the router.

**Reserved names.** Add `healthz` and `readyz` to `reservedOwnerNames`. Add an "Upgrading" note to `docs/deployment.md` with SQL that lists existing users and organizations using those names. Their profile page at `/{owner}` is shadowed; their repositories aren't.

**Docker.** Add a `HEALTHCHECK` on `/healthz` using busybox `wget` and `${CZ_SERVER_PORT:-8080}`.

**Docs.** Add a "Health checks" section to `docs/deployment.md`. It covers what each endpoint checks, which one load balancers and Kubernetes probes should use, and why the image's `HEALTHCHECK` uses liveness.

## Acceptance criteria

- [ ] `/healthz` returns 200 while the database is unreachable.
- [ ] `/readyz` returns 503 with `database: fail` and `migrations: skipped` when the database is unreachable.
- [ ] `/readyz` returns 503 with `migrations: fail` while an embedded migration is missing from `schema_migrations`, and 200 after `cloudzilla-cli migrate`.
- [ ] `/readyz` returns 503 with `storage: fail` when `git.repos_root` is missing or not writable, and leaves no `.readyz-*` file behind on success.
- [ ] Before setup completes, both endpoints answer directly instead of redirecting to `/setup`.
- [ ] Neither endpoint sets a `csrf_token` cookie or writes a `request` log line, and neither is counted by `middleware.RateLimit`.
- [ ] `POST /healthz` returns 405, and `GET /healthz/anything` reaches the router.
- [ ] Creating a user or organization named `healthz` or `readyz` (any case) fails with the reserved-name error.
- [ ] `docker build` then `docker run` reaches `healthy` on the image's own `HEALTHCHECK`.

## Tests

- **Unit** (`internal/router` or `internal/handler`). With a stub health service:
  - status codes and bodies for each check outcome;
  - no `Set-Cookie` header;
  - the wrapped app handler never runs for the two paths;
  - 405 for other methods.
- **Integration** (`TEST_DATABASE_DSN`). `db.Pending` reports:
  - nothing on a migrated fresh schema;
  - the deleted version after a `DELETE FROM schema_migrations WHERE version = …`;
  - everything when the schema has no `schema_migrations` table.
- **Storage check.** Use `t.TempDir()`, plus a read-only directory. Skip the read-only case when running as root.
- **Docker.** Check the `HEALTHCHECK` by hand with `docker inspect --format '{{.State.Health.Status}}'`. Note it in the PR if Docker isn't available.

## Files

- `internal/router/router.go`
- `internal/db/migrate.go`
- `internal/service/` (new health service, wired in `services.go`)
- `internal/handler/` (health handlers)
- `internal/service/owner_name.go`
- `Dockerfile`
- `docs/deployment.md`
