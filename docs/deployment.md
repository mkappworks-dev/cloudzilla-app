# Deployment

## Building

```bash
make build   # produces dist/cloudzilla (single binary, embedded templates + CSS)
```

No Node.js, npm, or Bun needed at runtime. Set config via `config.yaml` or environment variables.

## Docker

Cloudzilla ships a multi-stage `Dockerfile` and `docker-compose.yml`.

### Image build stages

| Stage     | Base                 | Purpose                                                                                                          |
| --------- | -------------------- | ---------------------------------------------------------------------------------------------------------------- |
| `builder` | `golang:1.27-alpine` | Downloads Tailwind CLI (arch-aware), compiles CSS, builds both Go binaries with `CGO_ENABLED=0 -ldflags="-s -w"` |
| runtime   | `alpine:3.24`        | Copies binaries; installs `ca-certificates tzdata`; exposes 8080/2222                                            |

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
make docker-build   # docker build -t cloudzilla-app:latest .
make docker-run     # docker compose up -d
make docker-down    # docker compose down
```

### First-run bootstrap

```bash
make docker-run
docker exec -it cloudzilla-cloudzilla-1 /app/cloudzilla-cli migrate
```

Then open `http://localhost:8080` — the first request redirects to `/setup` where you create the superadmin account via the web wizard.

The SSH host key is auto-generated into the named volume on first boot — no manual `ssh-keygen` step needed.

### Behind a reverse proxy

Set `server.trusted_proxies` (`CZ_SERVER_TRUSTED_PROXIES`) to the proxy's IP or CIDR, e.g. `CZ_SERVER_TRUSTED_PROXIES=172.16.0.0/12` for a Docker network. `X-Forwarded-For` is ignored from any other peer, because clients can forge it. A trusted proxy must write bare IP addresses into `X-Forwarded-For`: Cloudzilla reads it right to left and stops at a hop written as `ip:port` or `[v6]`, so those clients share the proxy's budget. Without this setting, audit-log IPs and the per-IP rate limits see only the proxy's address, so every client shares one budget. The limits are 10 attempts per 15 minutes on account creation (`/register`, `/register/complete/{token}` and `/invite/{token}`) and 30 per 15 minutes on password login (`/login`, `/api/auth/login` and `/auth/ldap`); each route has its own budget, and an IPv6 client is counted per /64.

### Upgrading

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
