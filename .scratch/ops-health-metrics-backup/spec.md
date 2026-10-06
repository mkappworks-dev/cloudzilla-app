# Operator health checks, metrics, and backup/restore

Created: 2026-10-06
Category: enhancement
Status: ready-for-agent

## Problem

An operator can't ask a running Cloudzilla whether it's healthy, can't see how it's performing, and has no supported way to get the instance back after losing a disk.

### Health

No route exists for a probe to call. Every request passes through the global middleware in `internal/router/router.go`:

- `middleware.Logger` writes an info line for each request.
- `CSRF` sets a `csrf_token` cookie on any request that doesn't have one.
- `RequireSetup` (`internal/middleware/setup.go`) redirects everything except `/setup`, `/static/`, `/invite/`, `/htmx.min.js` and `/alpine.min.js` to `/setup`. It does this until the first account exists, and it calls `CountAccounts` on every request in the meantime.

So before setup, a probe of `/` gets a 303 and costs a DB query. After setup, it renders the home page (more DB queries) and adds a log line.

None of the failures that matter shows up in a probe:

- **Database down.** No route reports it.
- **Unmigrated schema.** The server never migrates; `cloudzilla-cli migrate` does. A new binary can start against an old schema.
- **Repo volume unusable.** Nothing notices when the volume is missing or has gone read-only.

The Dockerfile has no `HEALTHCHECK`; only the compose `postgres` service has one.

### Metrics

What gets recorded today:

- **Requests:** timing goes only to the log line from `middleware.Logger`.
- **Pushes:** `pack_bytes` and `duration_ms` are logged by `internal/handler/git_http.go` and `internal/ssh/server.go`, through the existing `gittransport.ByteCounter`.
- **Fetches, imports, webhook deliveries and the DB pool:** nothing is recorded.

There is no `/metrics` route and `go.mod` has no Prometheus client. The roadmap's cross-cutting rules say "No new Go dependencies needed", but the user waived that rule for this work (see Decisions).

### Backup

The admin CLI (`cmd/cloudzilla`, cobra) has `migrate`, `gc`, `stats` and `seed`, but no backup or restore.

`docs/deployment.md` does dump and restore the database (`pg_dump -Fc` / `pg_restore`), but only as a step of the Postgres 17 → 18 upgrade. Nothing covers:

- the git repositories,
- the SSH host key,
- capturing the database and the repositories so that they agree with each other.

The runtime image (`alpine:3.24` plus `ca-certificates tzdata`) has no `pg_dump`.

## State an instance holds

| What             | Where                                            | Notes |
| ---------------- | ------------------------------------------------ | ----- |
| Database         | Postgres at `database.dsn`                       | All metadata, gists, users, access tokens, webhook secrets, TOTP secrets |
| Git repositories | `git.repos_root` (`/data/git-repos` in Docker)   | `<owner>/<name>.git` and `<owner>/<name>.wiki.git`. Soft-deleted repos sit beside them as `.deleted.<unix>` copies until the daily purge (30 days). `.import-tmp/` is scratch space, cleared at boot by `ImportService.RemoveStaleTemp` |
| SSH host key     | `git.ssh_host_key` (`/data/cloudzilla_host_key`) | If it changes, every client sees `REMOTE HOST IDENTIFICATION HAS CHANGED` |
| Avatars          | none on disk                                     | Today only a URL column (`users.avatar_url`, `organizations.avatar_url`). The parallel S3 avatar session adds stored files |
| Config           | `config.yaml` / `CZ_*`                           | Owned by the operator and not backed up. Restoring with a different `auth.jwt_secret` signs everyone out |

## Proposed design

### Health (issue 01)

**Liveness: `GET|HEAD /healthz`.** Returns `200 ok` as `text/plain` with `Cache-Control: no-store`. It touches neither the database nor the disk, so it fails only when the process can't serve HTTP.

**Readiness: `GET|HEAD /readyz`.** Returns 200 when every check passes and 503 otherwise. The body is JSON: `{"status":"ok","checks":{"database":"ok","migrations":"ok","storage":"ok"}}`. Each check reports `ok`, `fail` or `skipped`. Failure details go to the log at warn, never into the body: the endpoint is unauthenticated, and a driver error can name hosts and paths. All checks share one 3-second deadline.

- **`database`:** `PingContext`.
- **`migrations`:** a new `db.Pending(ctx, db)` lists the embedded migrations missing from `schema_migrations`, using the same file list as `runMigrations`. A missing `schema_migrations` table means everything is pending. Once the check passes it stays passed for the life of the process, because a binary's migration set is fixed and applied versions aren't removed. It's `skipped` when the database check fails.
- **`storage`:** create a `.readyz-*` temp file in `git.repos_root`, write to it, then remove it. This catches a missing volume, a read-only mount and wrong ownership. It doesn't measure free space.

Readiness deliberately skips three things:

- **Setup completion:** a fresh instance must be ready so that `/setup` can be served.
- **The SSH listener.**
- **Optional integrations** (SMTP, OAuth).

**Bypass.** A small handler wraps the chi router and answers the exact paths `/healthz` and `/readyz` before any global middleware runs. They therefore skip `RequestID`, `ClientIP`, `Logger`, `CORS`, `MaxFormBodySize`, `CSRF` (so no cookie is set), `RequireSetup`, `HighlightBudget` and all auth. They also skip every rate limiter: the route-level `RateLimit` and any global limiter the API rate-limiting session adds. `/healthz/x` and every other path still go to the router. Methods other than GET and HEAD get 405.

**Reserved names.** An owner name is the first URL segment (see the comment on `reservedOwnerNames` in `internal/service/owner_name.go`). `healthz` and `readyz` are currently valid usernames, so they join the reserved list. `metrics` doesn't, because `/metrics` is served only on its own listener.

An owner who already has one of those names keeps `/{owner}/{repo}` and everything below it. Only their profile page at `/{owner}` is shadowed. `docs/deployment.md` "Upgrading" gets the SQL to find them.

**Docker.** The `HEALTHCHECK` probes liveness with busybox `wget`, which ships in `alpine` (no extra package): `HEALTHCHECK --interval=30s --timeout=5s --start-period=20s --retries=3 CMD wget -q -O /dev/null "http://127.0.0.1:${CZ_SERVER_PORT:-8080}/healthz" || exit 1`.

It probes liveness rather than readiness for two reasons. Orchestrators restart unhealthy containers, and a restart fixes neither a database outage nor a pending migration. And on first boot, readiness fails by design until `cloudzilla-cli migrate` runs. Load balancers and Kubernetes `readinessProbe`s use `/readyz`.

### Metrics (issue 02)

**Exposition.** `github.com/prometheus/client_golang` (v1.24.1 at the time of writing), served by `promhttp.HandlerFor`.

- **Registry:** an `internal/metrics` package owns its own `prometheus.Registry`, not the global default registry. That way a library can't add series behind our back, and tests can build a fresh registry.
- **Collectors:** `collectors.NewGoCollector()`, `collectors.NewProcessCollector(…)` (process metrics are reported on Linux only) and `collectors.NewDBStatsCollector(db, "cloudzilla")`.
- **Names:** every Cloudzilla metric is declared in one file, so the set can be read in one place.

**Serving.** Off by default. Setting `metrics.listen_addr` (`CZ_METRICS_LISTEN_ADDR`, e.g. `127.0.0.1:9090`, or `:9090` on a private Docker network) starts a second `http.Server` that serves only `GET /metrics`. The port is never published or proxied, so the main listener never serves metrics. It shuts down with the main server.

**Starting set.** Every name below is prefixed `cloudzilla_` unless shown otherwise:

| Metric | Type | Labels | Source |
| ------ | ---- | ------ | ------ |
| `build_info` | gauge (1) | `version` | `main.version` |
| `http_requests_total` | counter | `method`, `route`, `code` | Middleware. `route` is chi's route pattern; `unmatched` when routing found none; `method` outside the standard verbs becomes `other` |
| `http_request_duration_seconds` | histogram | `method`, `route` | Same middleware. Buckets 5 ms to 60 s |
| `http_requests_in_flight` | gauge | (none) | Same middleware |
| `git_operations_total` | counter | `transport` (`http`/`ssh`), `service` (`upload-pack`/`receive-pack`), `result` (`ok`/`error`) | `GitUploadPack`/`GitReceivePack` and `execGitService` |
| `git_bytes_total` | counter | `transport`, `service` | Receive-pack: the existing `ByteCounter`. Upload-pack: a new counting writer around `resp.Encode` |
| `import_jobs` | gauge | `state` (`queued`/`running`) | Scrape-time read of `ImportService`'s in-memory jobs |
| `imports_total` | counter | `result` (`succeeded`/`failed`) | `ImportService.finish` |
| `webhook_deliveries_total` | counter | `attempt` (`first`/`retry`), `result` (`success`/`failure`) | `WebhookService.deliver` and `retryDeliver` |
| `webhook_retries_due` | gauge | (none) | Set on each 60 s retry tick from `ListPendingRetry` |
| `go_sql_*{db_name="cloudzilla"}` | various | (none) | `collectors.NewDBStatsCollector`: open, in-use and idle connections, wait count and wait time |
| `go_*`, `process_*` | various | (none) | The standard Go and process collectors, so stock dashboards work |

Out of scope: per-repo or per-user labels (unbounded cardinality), SSH session gauges, OpenTelemetry, and exemplars.

On HTTP, one fetch can take more than one `git-upload-pack` POST, because stateless negotiation sends several. `git_operations_total` therefore counts exchanges, not user-level fetches. The metric's HELP text says so.

### Backup and restore (issue 03)

**`cloudzilla-cli backup --output <path|->`** writes one uncompressed tar with mode 0600. It's uncompressed because packs are already zlib-compressed; operators can pipe it through `zstd` or `gzip`. The archive holds the database, every repository, password hashes and the host key's private half, and the docs say so. Entries, in order:

1. `cloudzilla-backup.json`, the manifest: format version, Cloudzilla version, creation time, newest applied migration, `pg_dump` version, and per-section file counts and bytes.
2. `database.pgdump`, from `pg_dump --format=custom --no-owner --no-privileges` against `database.dsn`.
3. `git-repos/…`, the tree under `git.repos_root`. It skips `.import-tmp/` and `.readyz-*`, and keeps `.deleted.*` copies so the 30-day undo window survives a restore.
4. `ssh_host_key`, when the file exists.

**Capture order (hot backup).** `pg_dump` runs first and reads one consistent snapshot. Then each repository is copied refs first (`HEAD`, `config`, `packed-refs`, `refs/`), objects second. Git writes objects before it moves a ref, and `cloudzilla-cli gc` prunes only unreachable loose objects older than its grace period (14 days by default). So every ref in the copy, and every SHA the database snapshot names, resolves in the copy, as long as `gc` doesn't run during the backup.

A repository created, renamed, transferred or deleted while the copy runs can disagree with its row; restore reports these. For a fully consistent backup, stop the server first. The docs show both.

**`cloudzilla-cli restore --input <path|->`** refuses to start unless:

- the target database has no `schema_migrations` table (a fresh, empty database);
- `git.repos_root` is empty or missing, the same guard `seed` uses;
- it understands the manifest's format version;
- the backup's newest migration is one this binary knows.

Then it:

1. Runs `pg_restore --no-owner --no-privileges --exit-on-error --single-transaction`.
2. Extracts the repositories, accepting only regular files and directories whose cleaned paths stay inside the root.
3. Writes the host key. If a different key is already there (the server generates one on first boot), it stops with a message; `--replace-host-key` overwrites it.
4. Applies any migrations newer than the backup, so a backup from an older release restores into a newer binary.
5. Reports repository rows with no directory and directories with no row. These are warnings, not failures.

**`pg_dump` / `pg_restore`.** Both come from `PATH`; `--pg-dump` and `--pg-restore` flags override that. The image gains `postgresql18-client`, which is in Alpine 3.24 main (18.6-r0, 3.3 MiB installed). Its major version must be at least the server's; compose runs `postgres:18-alpine`. Binary installs use the OS package. `backup` checks `pg_dump --version` and fails clearly when the client is missing or older than the server.

**Object storage.** Version 1 covers the local filesystem only. When the S3 avatar work lands:

- a local-disk avatar backend's directory becomes another archive section;
- an S3 bucket isn't copied. The manifest records the backend and bucket, and the docs point at bucket versioning or replication.

`restore` warns when the manifest names a backend it can't restore.

**Docs** (`docs/deployment.md` and `docs/configuration.md`):

- a "Backup and restore" section: a nightly cron example, the stop-the-server variant and a restore runbook;
- CLI reference entries for both commands;
- two drifts fixed while there: `configuration.md` names the Postgres volume `cloudzilla_pg_data` where compose has `postgres_data`, and the Dockerfile comment still mentions a SQLite DB.

## Acceptance criteria

The issues carry the full lists. In summary:

- [ ] `/healthz` answers 200 with the database stopped. `/readyz` answers 503 with the database stopped, with a migration pending, or with `git.repos_root` read-only, and 200 once each is fixed.
- [ ] Neither endpoint sets a cookie, logs a request line, redirects to `/setup`, needs auth or counts against any rate limit.
- [ ] The Docker image reports `healthy` on its own `HEALTHCHECK`.
- [ ] With `metrics.listen_addr` set, `/metrics` on that address serves the starting set. The main port doesn't serve metrics: there, `/metrics` is an ordinary `/{owner}` path.
- [ ] `cloudzilla-cli backup` then `cloudzilla-cli restore` into an empty database and repos root gives the same rows in every table and the same refs in every repository. An integration test proves it.

## Relevant files

- `internal/router/router.go`: global middleware order; where the health wrapper goes
- `internal/middleware/setup.go`, `rate_limit.go`, `csrf.go`, `logger.go`: what the health paths skip; `Logger` already reads chi's route pattern after routing
- `internal/service/owner_name.go`: `reservedOwnerNames`
- `internal/db/migrate.go`: embedded migrations, `schema_migrations`
- `internal/db/db.go`: `database/sql` over `pgx/v5/stdlib`, so pool stats come from `sql.DB.Stats()`
- `cmd/server/main.go`: listeners, workers, shutdown; where the metrics listener starts
- `internal/handler/git_http.go`, `internal/ssh/server.go`, `internal/gittransport/observe.go`: git transport and `ByteCounter`
- `internal/service/import_service.go`: in-memory import jobs and slots
- `internal/service/webhook_service.go`, `internal/store/webhook_store.go`: deliveries, `ListPendingRetry`
- `internal/config/config.go`: new `metrics` section
- `go.mod`: adds `github.com/prometheus/client_golang`
- `cmd/cloudzilla/main.go`: cobra root; new `backup` and `restore` commands
- `internal/service/repo_dirs.go`, `cmd/cloudzilla/gc.go`: repo directory layout and the gc grace period
- `internal/seed`: test data for the restore round trip
- `Dockerfile`, `docker-compose.yml`, `docs/deployment.md`, `docs/configuration.md`

## Decisions

Settled with the user on 2026-10-06:

1. **Metrics exposition:** `prometheus/client_golang`. The roadmap's no-new-dependencies rule is waived for this work.
2. **Protecting `/metrics`:** a separate listen address (`metrics.listen_addr`, off by default). There's no token, and the main port never serves metrics.
3. **Backup form:** `cloudzilla-cli backup` and `restore`, wrapping `pg_dump`/`pg_restore`. The image gains `postgresql18-client`.
4. **Hot or cold:** hot capture in the order above, with a reconciliation report on restore. The stop-the-server variant is documented for a fully consistent copy.
5. **`HEALTHCHECK` target:** liveness (`/healthz`).
6. **Paths:** `/healthz` and `/readyz`, with `healthz` and `readyz` reserved as owner names.
7. **Testing the restore:** the round trip runs under `make test-integration` only; each PR states what ran. Running integration tests in CI is a separate follow-up.

## Delivery

One PR per issue, in the order 01, 02, 03:

- **01 Health:** on `feat/ops-health-metrics-backup`, which also carries this spec.
- **02 Metrics:** on `feat/ops-metrics`.
- **03 Backup and restore:** on `feat/ops-backup-restore`.

The issues are independent, except that 03 reuses `db.Pending` from 01.

## Comments

**Claude, 2026-10-06:** Triaged with the user. The open questions are resolved as listed under Decisions, and the status is now `ready-for-agent`.
