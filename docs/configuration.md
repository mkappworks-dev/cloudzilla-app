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
| `smtp.host`                  | `""`                                         | `CZ_SMTP_HOST`                  | SMTP server host (empty = email disabled). Also enables email-verified signup, email verification and password reset emails. |
| `smtp.port`                  | `587`                                        | `CZ_SMTP_PORT`                  | SMTP server port                                |
| `smtp.username`              | `""`                                         | `CZ_SMTP_USERNAME`              | SMTP username                                   |
| `smtp.password`              | `""`                                         | `CZ_SMTP_PASSWORD`              | SMTP password                                   |
| `smtp.from`                  | `noreply@localhost`                          | `CZ_SMTP_FROM`                  | From address for outgoing email                 |
| `smtp.tls`                   | `false`                                      | `CZ_SMTP_TLS`                   | Use TLS for SMTP connection                     |
| `import.allow_local_networks`| `false`                                      | `CZ_IMPORT_ALLOW_LOCAL_NETWORKS`| Let repository imports reach loopback, private and link-local addresses |
| `import.timeout`             | `30m`                                        | `CZ_IMPORT_TIMEOUT`             | Time limit for one repository import            |
| `webhook.allow_local_networks`| `false`                                     | `CZ_WEBHOOK_ALLOW_LOCAL_NETWORKS`| Let webhooks be created for and delivered to loopback, private and link-local addresses |
| `rate_limit.enabled`         | `true`                                       | `CZ_RATE_LIMIT_ENABLED`         | Rate-limit every request that isn't a static asset; see [Rate limits](#rate-limits) |
| `rate_limit.window`          | `1h`                                         | `CZ_RATE_LIMIT_WINDOW`          | Length of the window each budget covers         |
| `rate_limit.core.authenticated` | `5000`                                    | `CZ_RATE_LIMIT_CORE_AUTHENTICATED` | Pages and `/api/*` per signed-in bucket      |
| `rate_limit.core.anonymous`  | `1000`                                       | `CZ_RATE_LIMIT_CORE_ANONYMOUS`  | Pages and `/api/*` per client IP                |
| `rate_limit.git.authenticated` | `1000`                                     | `CZ_RATE_LIMIT_GIT_AUTHENTICATED` | Git-over-HTTP requests per signed-in bucket   |
| `rate_limit.git.anonymous`   | `200`                                        | `CZ_RATE_LIMIT_GIT_ANONYMOUS`   | Git-over-HTTP requests per client IP            |
| `rate_limit.archive.authenticated` | `100`                                  | `CZ_RATE_LIMIT_ARCHIVE_AUTHENTICATED` | Archive downloads per signed-in bucket    |
| `rate_limit.archive.anonymous` | `20`                                       | `CZ_RATE_LIMIT_ARCHIVE_ANONYMOUS` | Archive downloads per client IP               |
| `rate_limit.search.authenticated` | `600`                                   | `CZ_RATE_LIMIT_SEARCH_AUTHENTICATED` | Searches per signed-in bucket              |
| `rate_limit.search.anonymous` | `60`                                        | `CZ_RATE_LIMIT_SEARCH_ANONYMOUS` | Searches per client IP                         |
| `quota.user.repos`           | `0`                                          | `CZ_QUOTA_USER_REPOS`           | Live repositories each user may own. `0` is unlimited; see [Quotas](#quotas) |
| `quota.user.storage_bytes`   | `0`                                          | `CZ_QUOTA_USER_STORAGE_BYTES`   | Disk, in bytes, each user's repositories and wikis may use together |
| `quota.org.repos`            | `0`                                          | `CZ_QUOTA_ORG_REPOS`            | Live repositories each organization may own    |
| `quota.org.storage_bytes`    | `0`                                          | `CZ_QUOTA_ORG_STORAGE_BYTES`    | Disk, in bytes, each organization's repositories and wikis may use together |
| `storage.backend`            | `local`                                      | `CZ_STORAGE_BACKEND`            | Where uploaded files such as avatars go: `local` or `s3`. See [storage](./storage.md) |
| `storage.local.root`         | `./storage`                                  | `CZ_STORAGE_LOCAL_ROOT`         | Directory for the `local` backend               |
| `storage.s3.endpoint`        | `""` (AWS)                                   | `CZ_STORAGE_S3_ENDPOINT`        | Endpoint URL of an S3-compatible server (R2, B2, Garage, versitygw) |
| `storage.s3.region`          | `us-east-1`                                  | `CZ_STORAGE_S3_REGION`          | Bucket region (`auto` for R2)                   |
| `storage.s3.bucket`          | `""`                                         | `CZ_STORAGE_S3_BUCKET`          | Bucket name; required for `s3`                  |
| `storage.s3.access_key_id`   | `""`                                         | `CZ_STORAGE_S3_ACCESS_KEY_ID`   | Static key; empty uses the AWS default credential chain |
| `storage.s3.secret_access_key` | `""`                                       | `CZ_STORAGE_S3_SECRET_ACCESS_KEY` | Static secret; set with `access_key_id` or not at all |
| `storage.s3.path_style`      | `false`                                      | `CZ_STORAGE_S3_PATH_STYLE`      | Path-style URLs, needed by most self-hosted S3 servers |
| `storage.s3.prefix`          | `""`                                         | `CZ_STORAGE_S3_PREFIX`          | Key prefix, so several instances can share a bucket |
| `mirror.enabled`             | `true`                                       | `CZ_MIRROR_ENABLED`             | Sync pull mirrors and offer mirror options. When off, existing mirrors stay read-only |
| `mirror.allow_local_networks`| `false`                                      | `CZ_MIRROR_ALLOW_LOCAL_NETWORKS`| Let pull mirrors reach loopback, private and link-local addresses |
| `mirror.min_interval`        | `10m`                                        | `CZ_MIRROR_MIN_INTERVAL`        | Shortest sync interval a mirror may use         |
| `mirror.default_interval`    | `8h`                                         | `CZ_MIRROR_DEFAULT_INTERVAL`    | Sync interval for new mirrors (at most `720h`)  |
| `mirror.max_concurrent`      | `3`                                          | `CZ_MIRROR_MAX_CONCURRENT`      | Syncs running at once on each server instance   |
| `mirror.timeout`             | `30m`                                        | `CZ_MIRROR_TIMEOUT`             | Time limit for one sync                         |
| `security.secret_key`        | `""`                                         | `CZ_SECURITY_SECRET_KEY`        | Key that encrypts stored credentials, such as mirror tokens. At least 32 bytes; see [Secret key](#secret-key) |
| `metrics.listen_addr`        | `""`                                         | `CZ_METRICS_LISTEN_ADDR`        | Address of a second listener that serves only `GET /metrics`, e.g. `127.0.0.1:9090`. Empty turns it off. See [Metrics](./deployment.md#metrics) |

### Secret key

`security.secret_key` encrypts secrets the server has to read back later. Each use gets its own key, derived with HKDF-SHA256, and values are sealed with AES-256-GCM. When it is unset, features that store such secrets refuse to store them and say which setting to set.

Generate one with `openssl rand -base64 32`. The string is used as-is, at least 32 bytes. Keep it apart from `auth.jwt_secret`, so rotating the JWT secret never touches stored credentials. **Losing or changing the key makes every stored credential unreadable;** they then have to be entered again.

### Environment Variable Mapping

All config keys can be overridden via environment variables using the `CZ_` prefix. Dots become underscores: `auth.jwt_secret` becomes `CZ_AUTH_JWT_SECRET`. Viper handles the mapping automatically.

### Rate limits

Each request counts against one resource's budget for its subject, per `rate_limit.window`:

- **Resources.** `git` is `…/info/refs`, `…/git-upload-pack` and `…/git-receive-pack`: a clone, fetch or push is two requests. `archive` is `GET /{owner}/{repo}/archive/…`. `search` is `/search` and `/search/code`. `core` is everything else. Static assets (`/static/*`, `/htmx.min.js`, `/alpine.min.js`, `/favicon.ico`) and avatars (`/avatars/*`) aren't counted.
- **Subjects.** A signed-in user has two buckets, each with the full `authenticated` budget: `web` for browser sessions, and `token` shared by all of their personal access tokens and OAuth-app tokens. Anything else, including a credential that doesn't verify and a token bound to a signing key (whose signature only the route can check), counts against the client's IPv4 address or IPv6 /64 with the `anonymous` budget.
- `0` makes a budget unlimited. A negative budget, or a window that isn't positive, stops the server at startup.

The per-IP limits on sign-up and password login (see [Deployment](./deployment.md#behind-a-reverse-proxy)) apply on top and can't be configured. Behind a proxy, set `server.trusted_proxies`, or every anonymous client shares the proxy's budget. Git over SSH isn't rate-limited. API clients see their budget in response headers; see [Rate limits](./api-reference.md#rate-limits).

```yaml
rate_limit:
  window: 1h
  git:
    anonymous: 500
  archive:
    anonymous: 0   # unlimited
```

### Quotas

Four limits, all off (`0`) by default, so upgrading changes nothing until you set one. Every user gets the same quota and so does every organization; there are no per-owner overrides. A superadmin's personal account is exempt, but organizations are always subject to theirs. A negative value stops the server at startup.

- **Repositories.** An owner's live repositories, those not in the trash, so deleting one frees its slot at once. At the limit, an owner can't create, fork, import, generate from a template, restore, or be handed a repository by a transfer (checked when the transfer is offered and again when it is accepted). The refusal is `403` with `{"error":"repository quota reached (50 of 50)"}`.
- **Storage.** The on-disk size of each live repository's git directory plus its wiki, summed. Gists don't count. A push is capped at the space left, and is refused like an oversized pack (`413` over HTTP, a message on stderr over SSH) naming the usage and the quota. A push that only deletes refs still goes through, so an owner at the limit can free space. Web file commits and wiki edits are refused at or over the quota; deleting a file or a wiki page is not.
- **How size is kept.** `repositories.size_bytes` is measured at startup for any repository without one, and again in the background after a push, a web commit, a wiki change, an import, a mirror sync, a fork, a template creation, a restore, and `cloudzilla gc`. Usage can lag a write by one measurement. Unpacked loose objects count at full size until `cloudzilla gc` prunes them.
- **Not exact.** A check and the create that follows aren't atomic, so creates racing each other can overshoot the repository limit by a few. A pack is capped at the space left when the push starts, so two simultaneous pushes can both fit and together pass the quota.

User Settings and an organization's settings show one line, such as `Repositories 12 of 50 · Storage 1.2 GiB of 10 GiB`, naming only the limits that are set.

```yaml
quota:
  user:
    repos: 50
    storage_bytes: 10737418240   # 10 GiB
  org:
    repos: 200
```

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

security:
  secret_key: "replace-with-at-least-32-random-bytes"

metrics:
  listen_addr: "127.0.0.1:9090"

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

Verification and password reset links point at `server.base_url`, so set it to the public URL. Without SMTP, nobody can reset a forgotten password from `/login`; a superadmin issues a link from `/admin/users/{username}` or prints one with [`cloudzilla-cli password-reset-link`](#cloudzilla-cli-password-reset-link). Without SMTP, addresses stay unverified, which keeps Google sign-in from linking to existing accounts by email; a superadmin can mark an address verified from `/admin/settings`. See [Email Verification](./access-control.md#email-verification).

### Persistent Data (Docker volumes)

| What             | Volume               | Container Path                                             |
| ---------------- | -------------------- | ---------------------------------------------------------- |
| PostgreSQL data  | `postgres_data`      | (managed by PostgreSQL container)                          |
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

### `cloudzilla-cli password-reset-link`

Print a single-use link that lets a user choose a new password, for when the instance can't send email or the user can't receive it. It works for 24 hours and replaces any link the user already has.

```bash
cloudzilla-cli password-reset-link alice --config /etc/cloudzilla/config.yaml
```

The link is built from `server.base_url`. Accounts created through Google, LDAP or SAML sign-up have no password, so the command refuses them. It also refuses a suspended account: unsuspend it first. Unlike an emailed link, this one doesn't mark the user's email address verified. Each link is recorded as `user.password.reset_link` in the audit log. A 2FA account still needs its TOTP or backup code to use the link; if those are lost too, run [`reset-2fa`](#cloudzilla-cli-reset-2fa) first. See [Resetting a forgotten password](./access-control.md#resetting-a-forgotten-password).

### `cloudzilla-cli reset-2fa`

Turn off two-factor authentication for a user who has lost their authenticator and backup codes, such as a sole superadmin with no other admin to reset it from `/admin/users`.

```bash
cloudzilla-cli reset-2fa alice --config /etc/cloudzilla/config.yaml
```

It clears the TOTP secret, flag and backup codes through the same code as the admin `reset-2fa` action, records `admin.user.2fa_reset` in the audit log with no actor ID and the actor name `cloudzilla-cli`, and mails the user the same security notice when SMTP is configured. A failed notice prints a warning but doesn't undo the reset. It doesn't sign the user out or touch their password, and doesn't revoke a password reset link already issued. For a user without 2FA, it says so and changes nothing. A suspended account is reset but stays suspended. See [Two-factor authentication](./access-control.md#two-factor-authentication).

### `cloudzilla-cli backup`

Write the database, repositories, local storage root and SSH host key to one tar archive (mode 0600).

```bash
cloudzilla-cli backup --output /backups/cloudzilla.tar
cloudzilla-cli backup --output - | zstd > cloudzilla.tar.zst
```

| Flag | Default | Meaning |
| --- | --- | --- |
| `--output` | (required) | Archive path, or `-` for stdout. The archive is built beside the path and renamed, so a failure leaves no partial file |
| `--pg-dump` | `pg_dump` | `pg_dump` binary. Its major version must be at least the server's |

It fails before writing anything when `pg_dump` is missing or older than the server. The archive holds secrets: store it encrypted. See [Backup and restore](./deployment.md#backup-and-restore).

### `cloudzilla-cli restore`

Rebuild an empty instance from a backup archive.

```bash
cloudzilla-cli restore --input /backups/cloudzilla.tar
zstd -dc cloudzilla.tar.zst | cloudzilla-cli restore --input -
```

| Flag | Default | Meaning |
| --- | --- | --- |
| `--input` | (required) | Archive path, or `-` for stdin |
| `--pg-restore` | `pg_restore` | `pg_restore` binary |
| `--replace-host-key` | off | Overwrite a different SSH host key already in place |

It refuses a database that already has tables, a non-empty `git.repos_root`, an unknown format version, an unsafe tar entry and a backup with a migration the binary lacks. Then it restores the database, extracts the repositories, writes the host key (mode 0600), applies newer migrations and prints repository rows without a directory and directories without a row. Sign-ins survive only with the same `auth.jwt_secret`. See [Backup and restore](./deployment.md#backup-and-restore).

### Instance management

User and repository management is handled through the web UI:

- `/setup` -- first-run superadmin creation
- `/admin/settings` -- site settings and invitation management
- Invite system for subsequent users (when registration is disabled)
