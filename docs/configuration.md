# Configuration & CLI Reference

Cloudzilla is configured via a YAML config file, environment variables, or a combination of both. Environment variables always override config file values.

---

## Configuration Reference

| Key                          | Default                                      | Env Override                    | Description                                     |
| ---------------------------- | -------------------------------------------- | ------------------------------- | ----------------------------------------------- |
| `server.port`                | `8080`                                       | `CZ_SERVER_PORT`                | HTTP listen port                                |
| `server.host`                | `0.0.0.0`                                    | `CZ_SERVER_HOST`                | HTTP listen address                             |
| `server.base_url`            | `http://localhost:8080`                      | `CZ_SERVER_BASE_URL`            | Public base URL (used for CORS, OAuth, emails, noreply commit addresses) |
| `server.read_timeout`        | `15s`                                        | `CZ_SERVER_READ_TIMEOUT`        | HTTP read timeout                               |
| `server.write_timeout`       | `15s`                                        | `CZ_SERVER_WRITE_TIMEOUT`       | HTTP write timeout                              |
| `server.trusted_proxies`     | _(empty)_                                    | `CZ_SERVER_TRUSTED_PROXIES`     | Comma-separated reverse-proxy IPs/CIDRs whose `X-Forwarded-For` is believed |
| `database.dsn`               | `postgres://cloudzilla:cloudzilla@...`       | `CZ_DATABASE_DSN`               | PostgreSQL connection string                    |
| `database.max_open_conns`    | `10`                                         | `CZ_DATABASE_MAX_OPEN_CONNS`    | Max open DB connections                         |
| `database.max_idle_conns`    | `5`                                          | `CZ_DATABASE_MAX_IDLE_CONNS`    | Max idle DB connections                         |
| `auth.jwt_secret`            | `dev-only-do-not-use-in-production-...`      | `CZ_AUTH_JWT_SECRET`            | JWT signing secret -- **change in production**  |
| `auth.jwt_expiry`            | `24h`                                        | `CZ_AUTH_JWT_EXPIRY`            | JWT token lifetime                              |
| `auth.cookie_name`           | `cz_token`                                   | `CZ_AUTH_COOKIE_NAME`           | httpOnly cookie name                            |
| `auth.cookie_secure`         | `false`                                      | `CZ_AUTH_COOKIE_SECURE`         | Set `Secure` flag on cookies (enable for HTTPS) |
| `git.repos_root`             | `./git-repos`                                | `CZ_GIT_REPOS_ROOT`             | Bare git repo storage path                      |
| `git.ssh_port`               | `2222`                                       | `CZ_GIT_SSH_PORT`               | SSH server port for git operations              |
| `git.ssh_host_key`           | `./cloudzilla_host_key`                      | `CZ_GIT_SSH_HOST_KEY`           | SSH host key file (auto-generated if missing)   |
| `oauth.google_client_id`     | `""`                                         | `CZ_OAUTH_GOOGLE_CLIENT_ID`     | Google OAuth client ID (empty = disabled)       |
| `oauth.google_client_secret` | `""`                                         | `CZ_OAUTH_GOOGLE_CLIENT_SECRET` | Google OAuth client secret                      |
| `oauth.google_redirect_url`  | `http://localhost:8080/auth/google/callback` | `CZ_OAUTH_GOOGLE_REDIRECT_URL`  | OAuth redirect URI (must match Google Console)  |
| `smtp.host`                  | `""`                                         | `CZ_SMTP_HOST`                  | SMTP server host (empty = email disabled). Also enables email-verified signup and email verification. |
| `smtp.port`                  | `587`                                        | `CZ_SMTP_PORT`                  | SMTP server port                                |
| `smtp.username`              | `""`                                         | `CZ_SMTP_USERNAME`              | SMTP username                                   |
| `smtp.password`              | `""`                                         | `CZ_SMTP_PASSWORD`              | SMTP password                                   |
| `smtp.from`                  | `noreply@localhost`                          | `CZ_SMTP_FROM`                  | From address for outgoing email                 |
| `smtp.tls`                   | `false`                                      | `CZ_SMTP_TLS`                   | Use TLS for SMTP connection                     |
| `import.allow_local_networks`| `false`                                      | `CZ_IMPORT_ALLOW_LOCAL_NETWORKS`| Let repository imports reach loopback, private and link-local addresses |
| `import.timeout`             | `30m`                                        | `CZ_IMPORT_TIMEOUT`             | Time limit for one repository import            |
| `storage.backend`            | `local`                                      | `CZ_STORAGE_BACKEND`            | Where uploaded files such as avatars go: `local` or `s3`. See [storage](./storage.md) |
| `storage.local.root`         | `./storage`                                  | `CZ_STORAGE_LOCAL_ROOT`         | Directory for the `local` backend               |
| `storage.s3.endpoint`        | `""` (AWS)                                   | `CZ_STORAGE_S3_ENDPOINT`        | Endpoint URL of an S3-compatible server (R2, B2, Garage, versitygw) |
| `storage.s3.region`          | `us-east-1`                                  | `CZ_STORAGE_S3_REGION`          | Bucket region (`auto` for R2)                   |
| `storage.s3.bucket`          | `""`                                         | `CZ_STORAGE_S3_BUCKET`          | Bucket name; required for `s3`                  |
| `storage.s3.access_key_id`   | `""`                                         | `CZ_STORAGE_S3_ACCESS_KEY_ID`   | Static key; empty uses the AWS default credential chain |
| `storage.s3.secret_access_key` | `""`                                       | `CZ_STORAGE_S3_SECRET_ACCESS_KEY` | Static secret; set with `access_key_id` or not at all |
| `storage.s3.path_style`      | `false`                                      | `CZ_STORAGE_S3_PATH_STYLE`      | Path-style URLs, needed by most self-hosted S3 servers |
| `storage.s3.prefix`          | `""`                                         | `CZ_STORAGE_S3_PREFIX`          | Key prefix, so several instances can share a bucket |

### Environment Variable Mapping

All config keys can be overridden via environment variables using the `CZ_` prefix. Dots become underscores: `auth.jwt_secret` becomes `CZ_AUTH_JWT_SECRET`. Viper handles the mapping automatically.

---

## Config File (`config.yaml`)

Cloudzilla looks for `config.yaml` in the current directory by default. Override with `--config <path>`.

### Minimal Development Config

```yaml
database:
  dsn: postgres://cloudzilla:cloudzilla@localhost:5432/cloudzilla?sslmode=disable

auth:
  jwt_secret: "dev-secret-change-in-production"
```

### Production Config

```yaml
server:
  port: 8080
  host: "0.0.0.0"
  base_url: "https://git.example.com"

database:
  dsn: postgres://cloudzilla:strongpassword@localhost/cloudzilla?sslmode=disable
  max_open_conns: 25
  max_idle_conns: 10

auth:
  jwt_secret: "replace-with-a-long-random-string"
  jwt_expiry: 24h
  cookie_name: cz_token
  cookie_secure: true

git:
  repos_root: /var/lib/cloudzilla/git-repos
  ssh_port: 2222
  ssh_host_key: /etc/cloudzilla/ssh_host_key

storage:
  backend: local
  local:
    root: /var/lib/cloudzilla/storage

smtp:
  host: "smtp.example.com"
  port: 587
  username: "user@example.com"
  password: "your-smtp-password"
  from: "noreply@example.com"
  tls: true
```

---

## Docker Configuration

Override any setting via environment variables in `docker-compose.yml`:

```yaml
environment:
  CZ_AUTH_JWT_SECRET: "replace-with-a-long-random-string"
  CZ_AUTH_COOKIE_SECURE: "true"
  CZ_SERVER_BASE_URL: "https://git.example.com"
```

### Email (SMTP)

Email notifications and email verification are disabled when `CZ_SMTP_HOST` is empty (the default). To enable:

```yaml
environment:
  CZ_SMTP_HOST: "smtp.example.com"
  CZ_SMTP_PORT: "587"
  CZ_SMTP_USERNAME: "user@example.com"
  CZ_SMTP_PASSWORD: "your-smtp-password"
  CZ_SMTP_FROM: "noreply@example.com"
  CZ_SMTP_TLS: "true"
```

Verification links point at `server.base_url`, so set it to the public URL. Without SMTP, addresses stay unverified, which keeps Google sign-in from linking to existing accounts by email; a superadmin can mark an address verified from `/admin/settings`. See [Email Verification](./access-control.md#email-verification).

### Persistent Data (Docker volumes)

| What             | Volume               | Container Path                                             |
| ---------------- | -------------------- | ---------------------------------------------------------- |
| PostgreSQL data  | `cloudzilla_pg_data` | (managed by PostgreSQL container)                          |
| Git repositories | `cloudzilla_data`    | `/data/git-repos/`                                         |
| Uploaded files   | `cloudzilla_data`    | `/data/storage/` (avatars; see [storage](./storage.md))    |
| SSH host key     | `cloudzilla_data`    | `/data/cloudzilla_host_key` (auto-generated on first boot) |

---

## Production Deployment (Binary / systemd)

### 1. Build the binary

```bash
make build
```

Produces `dist/cloudzilla` (HTTP server) and `dist/cloudzilla-cli` (admin CLI). No Node.js required -- the binary embeds all templates and compiled CSS.

### 2. Copy to server

```bash
scp dist/cloudzilla dist/cloudzilla-cli user@yourserver:/usr/local/bin/
```

### 3. Create directories and config

```bash
sudo mkdir -p /etc/cloudzilla
sudo mkdir -p /var/lib/cloudzilla/git-repos /var/lib/cloudzilla/storage
```

Write `/etc/cloudzilla/config.yaml` (see Production Config above).

### 4. Generate the SSH host key

```bash
ssh-keygen -t ed25519 -f /etc/cloudzilla/ssh_host_key -N ""
```

This key is stable -- clients store its fingerprint in `~/.ssh/known_hosts`. Replacing it later causes `WARNING: REMOTE HOST IDENTIFICATION HAS CHANGED` errors.

### 5. Run migrations

```bash
cloudzilla-cli migrate --config /etc/cloudzilla/config.yaml
```

### 6. Create the superadmin account

Open `http://yourdomain.com` -- you will be redirected to `/setup` to create the superadmin account via the web wizard.

### 7. Run as a systemd service

`/etc/systemd/system/cloudzilla.service`:

```ini
[Unit]
Description=Cloudzilla Git Forge
After=network.target

[Service]
ExecStart=/usr/local/bin/cloudzilla --config /etc/cloudzilla/config.yaml
Restart=on-failure
User=cloudzilla
WorkingDirectory=/var/lib/cloudzilla

[Install]
WantedBy=multi-user.target
```

```bash
sudo useradd --system --no-create-home cloudzilla
sudo chown -R cloudzilla:cloudzilla /var/lib/cloudzilla /etc/cloudzilla
sudo systemctl daemon-reload
sudo systemctl enable --now cloudzilla
```

### 8. Reverse proxy (recommended)

Run Nginx or Caddy in front of Cloudzilla on port 443. Example Caddy config:

```
yourdomain.com {
    reverse_proxy localhost:8080
}
```

SSH git traffic (port 2222) bypasses the reverse proxy -- open that port directly in your firewall if needed.

---

## CLI Reference

All commands accept `--config <path>` to override the default config file location.

### `cloudzilla-cli migrate`

Run pending database migrations.

```bash
cloudzilla-cli migrate
cloudzilla-cli migrate --config /etc/cloudzilla/config.yaml
```

Migrations are embedded in the binary and run in order. Safe to run repeatedly -- already-applied migrations are skipped.

### `cloudzilla-cli seed`

Fill a fresh instance with test data for manual testing: a superadmin (`siteadmin`, `admin@example.test`), 100 users, 10 organizations and 150 repositories with a year of backdated git history, plus issues, pull requests, reviews, discussions, releases, stars and gists. The default size takes under a minute.

In development, seed the dev instance right after migrating it:

```bash
make migrate
make seed
make dev
```

`make seed` reads `config.yaml` like `make dev` does, so it fills the dev database and writes the bare repositories to `./git-repos`, where the server reads them. It refuses to run unless the database has no accounts and `git.repos_root` is empty. To reseed, stop `make dev`, then recreate the database and empty `./git-repos`:

```bash
docker compose exec postgres dropdb -U cloudzilla --force cloudzilla
docker compose exec postgres createdb -U cloudzilla cloudzilla
rm -rf git-repos
make migrate && make seed
```

To keep your dev data, seed a scratch database and repos root instead, and start the server from the same shell so it reads both. A server pointed at another `git.repos_root` lists the seeded repositories with no files, branches or tags.

```bash
docker compose exec postgres createdb -U cloudzilla cloudzilla_seed
export CZ_DATABASE_DSN=postgres://cloudzilla:cloudzilla@localhost:5432/cloudzilla_seed?sslmode=disable
export CZ_GIT_REPOS_ROOT=./git-repos-seed
make migrate && make seed
make dev
```

`make seed` uses the default size; for another, run `cloudzilla-cli seed` (or `go run ./cmd/cloudzilla/. seed`) with the flags below. Run the server with the seed's `server.base_url`: commit authors use noreply addresses built from it.

| Flag         | Default           | Meaning                                         |
| ------------ | ----------------- | ----------------------------------------------- |
| `--users`    | `100`             | Users besides the superadmin                    |
| `--orgs`     | `10`              | Organizations                                   |
| `--repos`    | `150`             | Repositories                                    |
| `--seed`     | `1`               | Random seed; the same seed builds the same data |
| `--password` | `cloudzilla-seed` | Password for every seeded account               |

SMTP is switched off for the run, so the notifications it creates send no email.

### Instance management

User and repository management is handled through the web UI:

- `/setup` -- first-run superadmin creation
- `/admin/settings` -- site settings and invitation management
- Invite system for subsequent users (when registration is disabled)
