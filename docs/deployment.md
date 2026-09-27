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
| `builder` | `golang:1.23-alpine` | Downloads Tailwind CLI (arch-aware), compiles CSS, builds both Go binaries with `CGO_ENABLED=0 -ldflags="-s -w"` |
| runtime   | `alpine:3.21`        | Copies binaries; installs `ca-certificates tzdata`; exposes 8080/2222                                            |

### Persistent volume (`/data`)

All mutable state lives under `/data` inside the container, mounted as a named Docker volume:

| What             | Path                        |
| ---------------- | --------------------------- |
| Git repositories | `/data/git-repos/`          |
| SSH host key     | `/data/cloudzilla_host_key` |

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

Migration `076_users_email_case_insensitive` refuses to run while two accounts have emails that differ only by case. Its error names their user IDs; change or merge those accounts, then run `cloudzilla-cli migrate` again.
