# Deployment

## Building

```bash
make build   # produces dist/cloudzilla (single binary, embedded templates + CSS)
```

The binaries report `git describe --tags --always --dirty` (e.g. `v0.4.0-41-g8ab8c6e4`) as their version in the page footer and `cloudzilla-cli --version`. Override it with `make build VERSION=v0.4.0`.

No Node.js, npm, or Bun needed at runtime. Set config via `config.yaml` or environment variables.

## Docker

Cloudzilla ships a multi-stage `Dockerfile` and `docker-compose.yml`.

### Image build stages

| Stage     | Base                 | Purpose                                                                                                                     |
| --------- | -------------------- | --------------------------------------------------------------------------------------------------------------------------- |
| `builder` | `golang:1.27-alpine` | Downloads Tailwind CLI (arch-aware musl build), compiles CSS, builds both Go binaries with `CGO_ENABLED=0 -ldflags="-s -w"` |
| runtime   | `alpine:3.24`        | Copies binaries; installs `ca-certificates tzdata`; exposes 8080/2222; `HEALTHCHECK` on `/healthz`                          |

### Persistent volume (`/data`)

All mutable state lives under `/data` inside the container, mounted as a named Docker volume:

| What             | Path                        |
| ---------------- | --------------------------- |
| Git repositories | `/data/git-repos/`          |
| SSH host key     | `/data/cloudzilla_host_key` |

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
CZ_AUTH_JWT_SECRET=<strong secret>
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
docker exec -it cloudzilla-cloudzilla-1 /app/cloudzilla-cli migrate
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
- `migrations`: every migration embedded in the binary is recorded in `schema_migrations`. The server never migrates, so this fails after an upgrade until `cloudzilla-cli migrate` runs. It's `skipped` while the database check fails.
- `storage`: a temporary `.readyz-*` file can be created, written and removed in `git.repos_root`. This catches a missing volume, a read-only mount and wrong ownership, but not a full disk.

The body never says why a check failed; the server logs the cause at `WARN` with `msg="readiness check failed"`. Readiness doesn't depend on setup having completed, on the SSH listener, or on SMTP or OAuth.

The image's `HEALTHCHECK` probes `/healthz`, not `/readyz`. Orchestrators restart unhealthy containers, and a restart fixes neither a database outage nor a pending migration; and on first boot readiness fails by design until `cloudzilla-cli migrate` runs. Check it with `docker inspect --format '{{.State.Health.Status}}' <container>`.

### Behind a reverse proxy

Set `server.trusted_proxies` (`CZ_SERVER_TRUSTED_PROXIES`) to the proxy's IP or CIDR, e.g. `CZ_SERVER_TRUSTED_PROXIES=172.16.0.0/12` for a Docker network. `X-Forwarded-For` is ignored from any other peer, because clients can forge it. A trusted proxy must write bare IP addresses into `X-Forwarded-For`: Cloudzilla reads it right to left and stops at a hop written as `ip:port` or `[v6]`, so those clients share the proxy's budget. Without this setting, audit-log IPs and the per-IP rate limits see only the proxy's address, so every client shares one budget. The limits are 10 attempts per 15 minutes on account creation (`/register`, `/register/complete/{token}` and `/invite/{token}`) and 30 per 15 minutes on password login (`/login`, `/api/auth/login` and `/auth/ldap`); each route has its own budget, and an IPv6 client is counted per /64.

### Upgrading

`healthz` and `readyz` are reserved owner names, because `/healthz` and `/readyz` are the health-check endpoints. An existing user or organization with either name keeps its repositories at `/{owner}/{repo}`, but its profile page at `/{owner}` is shadowed. Find them with:

```sql
SELECT id, username FROM users WHERE lower(username) IN ('healthz', 'readyz');
SELECT id, name FROM organizations WHERE lower(name) IN ('healthz', 'readyz');
```

Migration `082_users_email_case_insensitive` refuses to run while two accounts have emails that differ only by case. Its error names their user IDs; change or merge those accounts, then run `cloudzilla-cli migrate` again.

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
