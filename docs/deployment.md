# Deployment

## Building

```bash
make build   # produces dist/cloudzilla (single binary, embedded templates + CSS)
```

The binaries report `git describe --tags --always --dirty` (e.g. `v0.4.0-41-g8ab8c6e4`) as their version in the page footer and `cz-admin --version`. Override it with `make build VERSION=v0.4.0`.

`make build` also produces `dist/cz`. `cz-admin` is the operator tool and runs on the server host; `cz` is the separate remote client that developers run on their own machines, so the server image and archive leave it out. See [cli](./cli.md).

No Node.js, npm, or Bun needed at runtime. Set config via `config.yaml` or environment variables.

## Docker

Cloudzilla ships a multi-stage `Dockerfile` and `docker-compose.yml`.

### Image build stages

| Stage     | Base                 | Purpose                                                                                                                     |
| --------- | -------------------- | --------------------------------------------------------------------------------------------------------------------------- |
| `builder` | `golang:1.27-alpine` | Downloads Tailwind CLI (arch-aware musl build), compiles CSS, builds both Go binaries with `CGO_ENABLED=0 -ldflags="-s -w"` |
| runtime   | `alpine:3.24`        | Copies binaries; installs `ca-certificates tzdata postgresql18-client`; exposes 8080/2222; `HEALTHCHECK` on `/healthz`                          |

### Persistent volume (`/data`)

All mutable state lives under `/data` inside the container, mounted as a named Docker volume:

| What             | Path                        |
| ---------------- | --------------------------- |
| Git repositories | `/data/git-repos/`          |
| Uploaded files   | `/data/storage/`            |
| SSH host key     | `/data/cloudzilla_host_key` |

`/data/storage` holds avatars when `storage.backend` is `local`, the default. Back it up with the database. To keep uploads in S3 or an S3-compatible bucket instead, set `CZ_STORAGE_BACKEND=s3` and the `CZ_STORAGE_S3_*` variables; see [storage](./storage.md) for AWS and R2 examples and the backup notes.

### PostgreSQL volume (`postgres_data`)

The compose `postgres` service runs `postgres:18-alpine` with `postgres_data` mounted at `/var/lib/postgresql`; the 18+ image keeps its cluster in the versioned subdirectory `18/docker`. Don't mount the volume at `/var/lib/postgresql/data` — the image refuses to start with a mount there.

#### Upgrading from PostgreSQL 17

Postgres 18 can't open a volume created by the old `postgres:17-alpine` service; the container exits with `Error: in 18+, these Docker images are configured to store database data in a format ...`. Dump the database before switching compose files, then restore it into a fresh volume:

```bash
# 1. Still on the Postgres 17 compose file (already switched? check out the old docker-compose.yml for this step)
docker compose exec -T postgres pg_dump -U cloudzilla -Fc cloudzilla > cloudzilla.dump
docker compose down

# 2. Remove only the Postgres volume. Not `down -v`: that also deletes cloudzilla_data (git repos, SSH host key)
docker volume ls --filter name=postgres_data
docker volume rm <project>_postgres_data

# 3. On the Postgres 18 compose file
docker compose up -d --wait postgres
docker compose exec -T postgres pg_restore -U cloudzilla -d cloudzilla < cloudzilla.dump
docker compose up -d
```

Keep `cloudzilla.dump` until you've checked the restored instance.

### Key environment variables (Viper `CZ_` prefix)

```
CZ_DATABASE_DRIVER=postgres
CZ_DATABASE_DSN=postgres://cloudzilla:cloudzilla@postgres:5432/cloudzilla?sslmode=disable
CZ_GIT_REPOS_ROOT=/data/git-repos
CZ_GIT_SSH_HOST_KEY=/data/cloudzilla_host_key
CZ_STORAGE_LOCAL_ROOT=/data/storage
CZ_AUTH_JWT_SECRET=<strong secret>
CZ_SECURITY_SECRET_KEY=<at least 32 bytes>
CZ_SERVER_PORT=8080
CZ_GIT_SSH_PORT=2222
```

No `config.yaml` file is needed at runtime when env vars are set.

### Docker make targets

```bash
make docker-build   # docker build --build-arg VERSION=$(VERSION) -t cloudzilla-app:latest .
make docker-run     # docker compose up -d
make docker-down    # docker compose down
```

`make docker-build` stamps the image with the same version as `make build`. A plain `docker build` reports `dev` unless you pass `--build-arg VERSION=<tag>`. Release images carry their tag.

### First-run bootstrap

```bash
make docker-run
docker exec -it cloudzilla-cloudzilla-1 /app/cz-admin migrate
```

Then open `http://localhost:8080` — the first request redirects to `/setup` where you create the superadmin account via the web wizard.

The SSH host key is auto-generated into the named volume on first boot — no manual `ssh-keygen` step needed.

### Health checks

Two unauthenticated endpoints on the main HTTP port answer probes. They're served ahead of all middleware, so they write no request log line, set no cookie, skip the `/setup` redirect and count against no rate limit. Both accept `GET` and `HEAD`, and send `Cache-Control: no-store`.

| Endpoint   | Answers                                                                                          | Use it for                                                         |
| ---------- | ------------------------------------------------------------------------------------------------ | ------------------------------------------------------------------ |
| `/healthz` | Liveness: `200 ok` whenever the process serves HTTP. Touches neither the database nor the disk. | Docker `HEALTHCHECK`, Kubernetes `livenessProbe`                   |
| `/readyz`  | Readiness: `200` when every check passes, otherwise `503`.                                       | Load-balancer health checks, Kubernetes `readinessProbe`           |

`/readyz` returns JSON such as `{"status":"fail","checks":{"database":"ok","migrations":"fail","storage":"ok"}}`. Each check is `ok`, `fail` or `skipped`, and all of them share a 3-second deadline:

- `database`: the server can ping Postgres.
- `migrations`: every migration embedded in the binary is recorded in `schema_migrations`. The server never migrates, so this fails after an upgrade until `cz-admin migrate` runs. It's `skipped` while the database check fails.
- `storage`: a temporary `.readyz-*` file can be created, written and removed in `git.repos_root`. This catches a missing volume, a read-only mount and wrong ownership, but not a full disk.

Give a Kubernetes `readinessProbe` a `timeoutSeconds` of at least 4: the default of 1 turns a slow database into a probe timeout instead of a 503.

The body never says why a check failed; the server logs the cause at `WARN` with `msg="readiness check failed"`. Readiness doesn't depend on setup having completed, on the SSH listener, or on SMTP or OAuth.

The image's `HEALTHCHECK` probes `/healthz`, not `/readyz`. Orchestrators restart unhealthy containers, and a restart fixes neither a database outage nor a pending migration; and on first boot readiness fails by design until `cz-admin migrate` runs. It requests `http://127.0.0.1:${CZ_SERVER_PORT:-8080}/healthz` with any proxy disabled, so it reads the port only from the `CZ_SERVER_PORT` environment variable, not from `config.yaml`, and it needs `server.host` to accept loopback connections (the default `0.0.0.0` does). Check it with `docker inspect --format '{{.State.Health.Status}}' <container>`.

### Metrics

Cloudzilla exposes Prometheus metrics on a separate listener, off by default. Set `metrics.listen_addr` (`CZ_METRICS_LISTEN_ADDR`) to start it; it serves only `GET /metrics`. The main port never serves metrics: there, `/metrics` is an ordinary `/{owner}` path.

**The metrics port has no authentication. Don't publish it, and don't proxy it.** Bind it to loopback (`127.0.0.1:9090`), or to a private network that only Prometheus can reach. Metric labels carry route patterns, never user or repository names.

Under Docker Compose, set the address and leave the port out of `ports:`; Prometheus on the same Compose network reaches it as `cloudzilla:9090`:

```yaml
  cloudzilla:
    ports:
      - "8080:8080"
      - "2222:2222"
      # no "9090:9090" here
    environment:
      CZ_METRICS_LISTEN_ADDR: ":9090"
```

A Prometheus `scrape_config`:

```yaml
scrape_configs:
  - job_name: cloudzilla
    static_configs:
      - targets: ["cloudzilla:9090"]
```

| Metric | Type | Labels | Meaning |
| ------ | ---- | ------ | ------- |
| `cloudzilla_build_info` | gauge | `version` | Always 1 |
| `cloudzilla_http_requests_total` | counter | `method`, `route`, `code` | Requests by chi route pattern (`unmatched` when routing found none); a method outside the standard verbs is `other`. `/healthz` and `/readyz` aren't counted |
| `cloudzilla_http_request_duration_seconds` | histogram | `method`, `route` | Request duration, buckets from 5 ms to 60 s |
| `cloudzilla_http_requests_in_flight` | gauge | | Requests being served |
| `cloudzilla_git_operations_total` | counter | `transport` (`http`, `ssh`), `service` (`upload-pack`, `receive-pack`), `result` (`ok`, `error`) | Transport exchanges. One HTTP fetch can take several `upload-pack` POSTs, so this isn't a count of fetches |
| `cloudzilla_git_bytes_total` | counter | `transport`, `service` | Bytes received (`receive-pack`) or sent (`upload-pack`) |
| `cloudzilla_import_jobs` | gauge | `state` (`queued`, `running`) | Imports in memory, read at scrape time |
| `cloudzilla_imports_total` | counter | `result` (`succeeded`, `failed`) | Finished imports |
| `cloudzilla_webhook_deliveries_total` | counter | `attempt` (`first`, `retry`), `result` (`success`, `failure`) | Delivery attempts; a non-2xx response is a failure |
| `cloudzilla_webhook_retries_due` | gauge | | Deliveries due for retry at the last 60-second retry tick |
| `go_sql_*` | various | `db_name="cloudzilla"` | Connection pool: open, in-use and idle connections, wait count and wait time |
| `go_*`, `process_*` | various | | Go runtime and process stats (`process_*` on Linux only) |

### Backup and restore

`cz-admin backup` writes one uncompressed tar holding the database (`pg_dump --format=custom`), every repository under `git.repos_root`, the local storage root (avatars, when `storage.backend` is `local`) and the SSH host key. `cz-admin restore` rebuilds an empty instance from it. Both run `pg_dump` / `pg_restore` from `PATH`; the Docker image includes `postgresql18-client`. On a binary install, add your OS's client package. Its major version must be at least the server's: `backup` fails, naming the version needed, when the client is missing or older. `--pg-dump` and `--pg-restore` point at a specific binary.

**The archive holds password hashes, webhook and TOTP secrets, every private repository and the SSH host key's private half.** Store it encrypted and mode 0600 (the file is created that way).

What a backup doesn't cover:

- **Configuration.** Keep `config.yaml` and your `CZ_*` variables yourself. Restore with the same `auth.jwt_secret`, or everyone is signed out. Restore with the same `security.secret_key`, or stored credentials such as mirror tokens become unreadable.
- **An S3 bucket.** With `storage.backend: s3` the bucket isn't copied; the manifest records its name, and `restore` prints a warning. Use bucket versioning or replication.

#### Hot backup

The server can keep running. `pg_dump` takes one consistent snapshot first. Then each repository is copied refs first (`HEAD`, `config`, `packed-refs`, `refs/`) and objects second. Git writes objects before it moves a ref, so every ref in the copy resolves, and so does every commit the database names, even when a push lands mid-copy. **Don't run `cz-admin gc` while a backup runs:** it could prune an object the copy still needs.

A repository created, renamed, transferred or deleted during the copy can disagree with its database row. `restore` lists those as warnings. For a copy that is consistent by construction, stop the server first:

```bash
docker compose stop cloudzilla
docker compose run --rm -T --no-deps --entrypoint /app/cz-admin cloudzilla backup --output - \
  | zstd -q -o "/backups/cloudzilla-$(date +%F).tar.zst"
docker compose start cloudzilla
```

#### Nightly backup

`--output -` writes the archive to stdout, so it can leave the container compressed and encrypted without touching a volume. This crontab line keeps one archive per day:

```cron
0 3 * * * umask 077; cd /srv/cloudzilla && docker compose exec -T cloudzilla /app/cz-admin backup --output - | zstd -q | age -r age1yourpublickey... -o /backups/cloudzilla-$(date +\%F).tar.zst.age
```

The summary prints to stderr. Test a restore from your newest archive now and then; an untested backup is a hope.

#### Restore runbook

Restore only runs into a new, empty database and an empty (or missing) `git.repos_root`. It checks the whole archive first and refuses, writing nothing, when:

- the database already has a `schema_migrations` table or any other table;
- `git.repos_root` is not empty;
- the archive's format version is unknown, an entry is a symlink, device or hard link, or an entry path is absolute or contains `..`;
- the backup holds a migration this binary doesn't have (restore with the release that took it, or a newer one);
- a different SSH host key is already in place (the server generates one on first boot). Remove it, or pass `--replace-host-key`.

1. Start a fresh Postgres 18 with an empty volume, and leave the Cloudzilla volume empty too. Don't start the `cloudzilla` service yet: its first boot would create a host key.
2. Set the same `auth.jwt_secret` and `security.secret_key` as the old instance.
3. Restore:

   ```bash
   docker compose up -d --wait postgres
   age -d -i key.txt /backups/cloudzilla-2026-10-08.tar.zst.age | zstd -dc \
     | docker compose run --rm -T --no-deps --entrypoint /app/cz-admin cloudzilla restore --input -
   ```

4. Read the report. Warnings name repository rows without a directory, and directories without a row. Migrations newer than the backup are applied, so a backup from an older release restores into a newer one.
5. `docker compose up -d`.

If extraction or migration fails after the database was restored, the error says so. Drop the database, empty the volumes and start again from step 1.

### Behind a reverse proxy

Set `server.trusted_proxies` (`CZ_SERVER_TRUSTED_PROXIES`) to the proxy's IP or CIDR, e.g. `CZ_SERVER_TRUSTED_PROXIES=172.16.0.0/12` for a Docker network. `X-Forwarded-For` is ignored from any other peer, because clients can forge it. A trusted proxy must write bare IP addresses into `X-Forwarded-For`: Cloudzilla reads it right to left and stops at a hop written as `ip:port` or `[v6]`, so those clients share the proxy's budget. Without this setting, audit-log IPs and the per-IP rate limits see only the proxy's address, so every client shares one budget. The limits are 10 attempts per 15 minutes on account creation (`/register`, `/register/complete/{token}` and `/invite/{token}`) and 30 per 15 minutes on password login (`/login`, `/api/auth/login` and `/auth/ldap`); each route has its own budget, and an IPv6 client is counted per /64.

The global [rate limits](./configuration.md#rate-limits) count anonymous requests per client IP the same way, so a missing `server.trusted_proxies` puts every anonymous visitor behind the proxy into one budget. When a request carries `X-Forwarded-For` from a peer that isn't trusted, Cloudzilla logs one warning naming the peer.

### Several instances

Rate-limit counts live in each process. Behind a round-robin load balancer, N instances allow up to N times each budget.

### Upgrading

`healthz` and `readyz` are reserved owner names, because `/healthz` and `/readyz` are the health-check endpoints. An existing user or organization with either name keeps its repositories at `/{owner}/{repo}`, but its profile page at `/{owner}` is shadowed. Find them with:

```sql
SELECT id, username FROM users WHERE lower(username) IN ('healthz', 'readyz');
SELECT id, name FROM organizations WHERE lower(name) IN ('healthz', 'readyz');
```

Migration `082_users_email_case_insensitive` refuses to run while two accounts have emails that differ only by case. Its error names their user IDs; change or merge those accounts, then run `cz-admin migrate` again.

The owner-name rule (see [access-control](./access-control.md)) is checked only when a user or organization is created, so names from before it may fail it. An owner whose name fails `service.ValidateName` can't create, fork or receive repositories, and one whose name isn't a single safe path segment (containing `/`, `\`, `*`, `?`, `[` or `]`, or equal to `.` or `..`) has its repositories refused on the web, the API, SSH and Git smart-HTTP. List every name that fails the rule with:

```sql
SELECT id, username FROM users WHERE username !~ '^[A-Za-z0-9][A-Za-z0-9_-]{0,38}$';
SELECT id, name FROM organizations WHERE name !~ '^[A-Za-z0-9][A-Za-z0-9_-]{0,38}$';
```

Rename them in the database, update `repositories.owner_name` to match, and move the owner's directory under `git.repos_root`.

The shared, case-insensitive owner namespace is also checked only on create, so owners from before it may already collide. A user and an organization with the same name share one directory under `git.repos_root`, and so do owners whose names differ only by case on a case-insensitive filesystem (the default on macOS and Windows), so their repositories alias each other. List them with:

```sql
-- a user and an organization with the same name, ignoring case
SELECT u.id AS user_id, u.username, o.id AS org_id, o.name AS org_name
FROM users u JOIN organizations o ON lower(u.username) = lower(o.name);
-- usernames that differ only by case
SELECT lower(username) AS name, array_agg(id ORDER BY id) AS user_ids
FROM users GROUP BY lower(username) HAVING count(*) > 1;
-- organization names that differ only by case
SELECT lower(name) AS name, array_agg(id ORDER BY id) AS org_ids
FROM organizations GROUP BY lower(name) HAVING count(*) > 1;
```

Rename all but one owner in each row in the database, updating `repositories.owner_name` to match. The old directory also holds the other owner's repositories, so move only the renamed owner's repository directories (`<name>.git`, `<name>.wiki.git` and their `.deleted.` copies) into its new one.
