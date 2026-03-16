# Cloudzilla

A minimal, self-hosted Git forge — single binary, no external runtime dependencies.

**Stack:** Go · Go Templates · HTMX · Tailwind CSS · SQLite (PostgreSQL-ready)

---

## Features

- User accounts with JWT authentication (httpOnly cookie)
- Repository management
- Issues with open/close state
- Pull requests with merge/close workflow
- Inline comments with HTMX live updates (no page reload)
- Admin CLI for bootstrapping
- Single binary ships API + embedded frontend + CSS
- No Node.js/npm required (Tailwind CLI for dev only)
- SQLite by default; swap to PostgreSQL via two config lines

---

## Quick Start

### Prerequisites

- Go 1.23+
- (Optional) Tailwind CLI for local CSS development

### 1. Install dependencies

```bash
go mod tidy
make setup-tailwind     # One-time: download Tailwind CLI
```

### 2. Configure

Copy and edit `config.yaml`:

```yaml
server:
  port: 8080
  host: "0.0.0.0"

database:
  driver: sqlite3 # or "postgres"
  dsn: ./cloudzilla.db # or postgres DSN

auth:
  jwt_secret: change-me # change this in production
  jwt_expiry: 24h

git:
  repos_root: ./git-repos
```

### 3. Run migrations

```bash
make migrate
```

### 4. Create an admin user

```bash
go run ./cmd/cloudzilla/. create-user \
  --username admin \
  --email admin@localhost \
  --password changeme
```

### 5. Start the dev server

```bash
make dev
# Backend + Tailwind watch: http://localhost:8080
```

---

## Production Build

```bash
make build
./dist/cloudzilla --config /etc/cloudzilla/config.yaml
```

`make build` produces:

- `dist/cloudzilla` — HTTP server binary (API + embedded frontend + CSS)
- `dist/cloudzilla-cli` — Admin CLI binary

The server binary embeds all frontend templates and compiled CSS. No Node.js required at runtime or build time.

---

## Configuration Reference

| Key                       | Default           | Description                                   |
| ------------------------- | ----------------- | --------------------------------------------- |
| `server.port`             | `8080`            | HTTP listen port                              |
| `server.host`             | `0.0.0.0`         | HTTP listen address                           |
| `server.read_timeout`     | `15s`             | HTTP read timeout                             |
| `server.write_timeout`    | `15s`             | HTTP write timeout                            |
| `database.driver`         | `sqlite3`         | `sqlite3` or `postgres`                       |
| `database.dsn`            | `./cloudzilla.db` | DB connection string                          |
| `database.max_open_conns` | `10`              | Max open DB connections                       |
| `database.max_idle_conns` | `5`               | Max idle DB connections                       |
| `auth.jwt_secret`         | `change-me`       | JWT signing secret — **change in production** |
| `auth.jwt_expiry`         | `24h`             | JWT token lifetime                            |
| `auth.cookie_name`        | `cz_token`        | httpOnly cookie name                          |
| `git.repos_root`          | `./git-repos`     | Bare git repo storage path                    |

### Switching to PostgreSQL

Change two lines in `config.yaml`:

```yaml
database:
  driver: postgres
  dsn: postgres://user:pass@localhost/cloudzilla?sslmode=disable
```

> **Note:** SQL queries use `?` placeholders (SQLite syntax). Full PostgreSQL placeholder compatibility (`$1`, `$2`, ...) is a future migration task. The driver connection itself works.

---

## CLI Reference

All commands accept `--config <path>` to override the default config file location.

### `cloudzilla migrate`

Run pending database migrations.

```bash
cloudzilla migrate
cloudzilla migrate --config /etc/cloudzilla/config.yaml
```

### `cloudzilla create-user`

Create a new user account.

```bash
cloudzilla create-user \
  --username <name> \
  --email <email> \
  --password <password>
```

### `cloudzilla create-repo`

Create a new repository.

```bash
cloudzilla create-repo \
  --owner <username> \
  --name <repo-name> \
  --description "Optional description"
```

---

## API Reference

All JSON endpoints are under `/api/`. Authentication uses a JWT in an httpOnly cookie (`cz_token`) or an `Authorization: Bearer <token>` header.

### Auth

| Method | Path               | Auth | Description                                             |
| ------ | ------------------ | ---- | ------------------------------------------------------- |
| POST   | `/api/auth/login`  | —    | Login; sets `cz_token` cookie and returns token in body |
| POST   | `/api/auth/logout` | —    | Clears the auth cookie                                  |

### Users

| Method | Path                         | Auth | Description              |
| ------ | ---------------------------- | ---- | ------------------------ |
| GET    | `/api/users/:username`       | —    | Get user profile         |
| GET    | `/api/users/:username/repos` | —    | List user's repositories |

### Repositories

| Method | Path                      | Auth     | Description            |
| ------ | ------------------------- | -------- | ---------------------- |
| GET    | `/api/repos/`             | —        | List all repositories  |
| POST   | `/api/repos/`             | Required | Create a repository    |
| GET    | `/api/repos/:owner/:repo` | —        | Get repository details |

### Issues

| Method | Path                                                  | Auth     | Description               |
| ------ | ----------------------------------------------------- | -------- | ------------------------- |
| GET    | `/api/repos/:owner/:repo/issues/`                     | —        | List issues               |
| POST   | `/api/repos/:owner/:repo/issues/`                     | Required | Create an issue           |
| GET    | `/api/repos/:owner/:repo/issues/:number`              | —        | Get issue details         |
| PATCH  | `/api/repos/:owner/:repo/issues/:number`              | Required | Update issue (open/close) |
| GET    | `/api/repos/:owner/:repo/issues/:number/comments`     | —        | List comments             |
| POST   | `/api/repos/:owner/:repo/issues/:number/comments`     | Required | Add a comment             |
| DELETE | `/api/repos/:owner/:repo/issues/:number/comments/:id` | Required | Delete a comment          |

### Pull Requests

| Method | Path                                    | Auth     | Description             |
| ------ | --------------------------------------- | -------- | ----------------------- |
| GET    | `/api/repos/:owner/:repo/pulls/`        | —        | List pull requests      |
| POST   | `/api/repos/:owner/:repo/pulls/`        | Required | Create a pull request   |
| GET    | `/api/repos/:owner/:repo/pulls/:number` | —        | Get PR details          |
| PATCH  | `/api/repos/:owner/:repo/pulls/:number` | Required | Update PR (merge/close) |

### HTMX Fragments

| Method | Path                                              | Description                                  |
| ------ | ------------------------------------------------- | -------------------------------------------- |
| GET    | `/fragments/:owner/:repo/issues/:number/comments` | Returns rendered HTML fragment for HTMX swap |

---

## Make Targets

| Target                | Description                                                   |
| --------------------- | ------------------------------------------------------------- |
| `make setup-tailwind` | Download Tailwind CLI (one-time)                              |
| `make build-css`      | Compile Tailwind CSS to `cmd/server/frontend/static/main.css` |
| `make dev`            | Run backend + Tailwind watch concurrently                     |
| `make build`          | Build Go binaries (with embedded CSS)                         |
| `make build-backend`  | Compile server binary to `dist/cloudzilla`                    |
| `make build-cli`      | Compile CLI binary to `dist/cloudzilla-cli`                   |
| `make migrate`        | Run DB migrations                                             |
| `make lint`           | Run golangci-lint                                             |
| `make test`           | Run Go tests                                                  |
| `make clean`          | Remove build artifacts and database files                     |

---

## Project Structure

```
cmd/
  server/          # HTTP server entrypoint
    frontend/      # Templates + static files (embedded in binary)
      templates/
        layout.html          # Base HTML shell
        pages/               # Page templates (home, login, user, repo, issues, pulls, etc.)
        fragments/           # HTMX swap fragments
      static/
        main.css             # Compiled Tailwind output
      htmx.min.js            # HTMX library
  cloudzilla/      # Admin CLI (cobra)
internal/
  config/          # Config loading (viper + YAML)
  db/              # DB connection + migration runner
  model/           # Data structs (db + json tags)
  store/           # Store layer (uses sqlc-generated db queries)
    query/         # SQL query files for sqlc code generation
    db/            # Generated sqlc code (models + query methods)
  service/         # Business logic
  handler/         # HTTP handlers (page + API)
    page_handler.go          # Page rendering handlers
    viewmodels.go            # Data structs for templates
  middleware/      # Auth, logger, CORS
  router/          # chi route registration + template parsing
migrations/        # SQL migration files (embedded via embed.FS)
tailwind/          # Tailwind CSS configuration
  input.css        # Tailwind directives
  tailwind.config.js         # Theme customization
config.yaml        # Default configuration
sqlc.yaml          # sqlc code generation config
Makefile
```

### Architecture layers

```
Handler → Service → Store → Database
```

Handlers call services only. Services call stores only. Stores own all SQL.

---

## Database Migrations

Migrations live in `migrations/` and are embedded into the binary at build time. They run in order on `cloudzilla migrate`.

| File                           | Creates               |
| ------------------------------ | --------------------- |
| `001_create_users.sql`         | `users` table         |
| `002_create_repositories.sql`  | `repositories` table  |
| `003_create_issues.sql`        | `issues` table        |
| `004_create_pull_requests.sql` | `pull_requests` table |
| `005_create_comments.sql`      | `comments` table      |
| `006_create_permissions.sql`   | `permissions` table   |

---

## What's Not in v1

The following are intentionally out of scope for the initial release:

- Git HTTP smart protocol (`git clone/push/pull`)
- SSH server
- Code browser (file tree, blob, blame)
- Commit history and diff rendering
- Webhooks and notifications
- OAuth / SSO
- Organization accounts

---

## Dependencies

### Direct Dependencies

| Package | Purpose | Version |
|---------|---------|---------|
| `go-chi/chi/v5` | HTTP router with named params and middleware chaining | v5.2.5 |
| `go-chi/cors` | CORS middleware for chi | v1.2.2 |
| `golang-jwt/jwt/v5` | JWT token signing and verification | v5.3.1 |
| `jackc/pgx/v5` | PostgreSQL driver (stdlib-compatible via pgx/v5/stdlib) | v5.8.0 |
| `modernc.org/sqlite` | Pure-Go SQLite driver (no CGo) | v1.46.1 |
| `spf13/cobra` | CLI command framework with subcommand trees | v1.10.2 |
| `spf13/viper` | Config file + environment variable loading | v1.21.0 |
| `golang.org/x/crypto` | Secure password hashing (bcrypt, argon2) | v0.49.0 |

### Notable Indirect Dependencies

| Package | Purpose |
|---------|---------|
| `go-viper/mapstructure` | Struct mapping used by viper |
| `spf13/afero` | Virtual filesystem abstraction used by viper |
