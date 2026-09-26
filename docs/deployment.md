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
