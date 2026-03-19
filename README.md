# Cloudzilla

A minimal, self-hosted Git forge — single binary, no external runtime dependencies.

**Stack:** Go · Go Templates · HTMX · Tailwind CSS · PostgreSQL

> ⚠️ **Alpha**: Cloudzilla is under active development. APIs and features may change. Not recommended for production use yet.

---

## Architecture Overview

```mermaid
graph TB
    subgraph Client["Client"]
        Browser["Browser (HTMX)"]
        GitCLI["git CLI"]
        SSHCLI["SSH Client"]
    end

    subgraph Cloudzilla["Cloudzilla (Single Binary)"]
        HTTP["HTTP Server :8080\n(chi router)"]
        SSH["SSH Server :2222\n(gliderlabs/ssh)"]

        subgraph Layers["Application Layers"]
            Handler["Handlers\n(page + API + git HTTP)"]
            Service["Services\n(business logic)"]
            Store["Stores\n(SQL queries / sqlc)"]
        end

        subgraph Frontend["Embedded Frontend"]
            Templates["Go html/template"]
            Static["Tailwind CSS + HTMX"]
        end
    end

    subgraph Data["Persistence"]
        PG[("PostgreSQL")]
        Repos["Bare Git Repos\n(on disk)"]
    end

    Browser -->|"HTTP/HTTPS"| HTTP
    GitCLI -->|"HTTP Smart Protocol"| HTTP
    SSHCLI -->|"SSH git transport"| SSH

    HTTP --> Handler
    SSH --> Handler
    Handler --> Service
    Service --> Store
    Store --> PG
    Service --> Repos
    Handler --> Templates
    Templates --> Static

    classDef client fill:#4A90D9,stroke:#2C5F8A,color:#fff
    classDef server fill:#27AE60,stroke:#1A7A40,color:#fff
    classDef data fill:#8E44AD,stroke:#6C3483,color:#fff
    classDef frontend fill:#7F8C8D,stroke:#566573,color:#fff

    class Browser,GitCLI,SSHCLI client
    class HTTP,SSH,Handler,Service,Store server
    class PG,Repos data
    class Templates,Static frontend
```

### Request Flow

```mermaid
sequenceDiagram
    participant B as Browser
    participant MW as Middleware
    participant H as Handler
    participant S as Service
    participant ST as Store
    participant DB as PostgreSQL

    rect rgb(74, 144, 217)
        B->>MW: HTTP Request
    end
    rect rgb(230, 126, 34)
        MW->>MW: JWT auth (cookie/header)
        MW->>MW: RequireSetup check
        MW->>H: request + claims in context
    end
    rect rgb(39, 174, 96)
        H->>S: business logic call
        S->>ST: query
    end
    rect rgb(142, 68, 173)
        ST->>DB: SQL
        DB-->>ST: rows
    end
    rect rgb(39, 174, 96)
        ST-->>S: models
        S-->>H: result
    end
    rect rgb(74, 144, 217)
        H-->>B: HTML page / HTMX fragment / JSON
    end
```

### Authentication Flow

```mermaid
flowchart LR
    subgraph Login["Login Methods"]
        Form["Form POST /login"]
        API["API POST /api/auth/login"]
        Google["GET /auth/google\n(OAuth)"]
        Invite["GET /invite/:token\n(Invitation)"]
    end

    subgraph Processing["Auth Processing"]
        Verify["Verify credentials\nor OAuth token"]
        JWT["Generate JWT"]
        Cookie["Set httpOnly\ncz_token cookie"]
    end

    subgraph Access["Protected Access"]
        OptMW["optAuthMW\n(optional auth)"]
        AuthMW["authMW\n(required auth)"]
        SuperMW["requireSuperadmin\n(superadmin only)"]
    end

    Form --> Verify
    API --> Verify
    Google --> Verify
    Invite --> Verify
    Verify --> JWT --> Cookie

    Cookie --> OptMW
    Cookie --> AuthMW
    Cookie --> SuperMW

    classDef loginMethod fill:#4A90D9,stroke:#2C5F8A,color:#fff
    classDef processing fill:#E67E22,stroke:#CA6F1E,color:#fff
    classDef access fill:#27AE60,stroke:#1A7A40,color:#fff

    class Form,API,Google,Invite loginMethod
    class Verify,JWT,Cookie processing
    class OptMW,AuthMW,SuperMW access
```

### Git Transport Flow

```mermaid
flowchart TB
    subgraph HTTP["Git over HTTP (Smart Protocol)"]
        InfoRefs["GET /info/refs\n?service=git-upload-pack"]
        UploadPack["POST /git-upload-pack\n(clone/fetch)"]
        ReceivePack["POST /git-receive-pack\n(push)"]
    end

    subgraph SSH["Git over SSH (:2222)"]
        SSHConn["SSH connection"]
        KeyLookup["MD5 fingerprint\nlookup in DB"]
        SSHDispatch["git-upload-pack\nor git-receive-pack"]
    end

    subgraph Permissions["Permission Check"]
        CanRead["CanRead()\npublic or reader/writer/admin/owner"]
        CanWrite["CanWrite()\nowner or writer/admin"]
    end

    subgraph GitOps["go-git operations"]
        BareRepo["Bare Repo on disk\n(ReposRoot)"]
    end

    InfoRefs --> CanRead --> BareRepo
    UploadPack --> CanRead --> BareRepo
    ReceivePack --> CanWrite --> BareRepo

    SSHConn --> KeyLookup --> SSHDispatch --> CanRead
    SSHDispatch --> CanWrite

    classDef httpNode fill:#4A90D9,stroke:#2C5F8A,color:#fff
    classDef sshNode fill:#4A90D9,stroke:#2C5F8A,color:#fff
    classDef permNode fill:#E67E22,stroke:#CA6F1E,color:#fff
    classDef gitNode fill:#8E44AD,stroke:#6C3483,color:#fff

    class InfoRefs,UploadPack,ReceivePack httpNode
    class SSHConn,KeyLookup,SSHDispatch sshNode
    class CanRead,CanWrite permNode
    class BareRepo gitNode
```

### Permission Model

```mermaid
graph TD
    subgraph Instance["Instance Level"]
        SA["superadmin"]
        U["user"]
    end

    subgraph Org["Organization Level"]
        OO["org owner"]
        OM["org member"]
    end

    subgraph Repo["Repository Level"]
        RO["repo owner\n● full control"]
        Admin["collab: admin\n● read + push"]
        Writer["collab: writer\n● read + push"]
        Reader["collab: reader\n● read only"]
    end

    SA -->|"creates"| OO
    OO -->|"adds"| OM
    OO -.->|"read + push + manage\non org repos"| RO

    classDef instanceLevel fill:#E67E22,stroke:#CA6F1E,color:#fff
    classDef orgLevel fill:#4A90D9,stroke:#2C5F8A,color:#fff
    classDef repoLevel fill:#27AE60,stroke:#1A7A40,color:#fff

    class SA,U instanceLevel
    class OO,OM orgLevel
    class RO,Admin,Writer,Reader repoLevel
```

---

## Features

- **First-run setup wizard** — visit the server after migrations; first user becomes superadmin automatically
- **Instance access control** — superadmin can toggle `allow_registration` and `allow_login` at runtime; "Sign in" link auto-hides when login is disabled
- **Invitation system** — superadmin generates shareable invite links (no SMTP required); invited users bypass registration and login restrictions permanently
- **Repository collaborators** — owner adds/removes users per-repo via settings page; roles: `reader` (read-only), `writer` (push), `admin` (push, no manage); managed with HTMX, no reload
- **Ownership transfer** — repo owner can transfer a personal repo to another user; org owner can transfer org ownership to another member; both via settings pages
- **Labels** — color-coded tags created per-repo; apply to issues and PRs; displayed as pills on list pages and in detail sidebars; fully managed via HTMX with no page reload
- **Assignees** — assign any user to an issue or PR; sidebar on detail pages with inline add/remove via HTMX
- **Stars** — star/unstar any repo; star count shown on the repo header; stargazers list page (`/{owner}/{repo}/stargazers`); user starred repos page (`/{owner}/stars`)
- User accounts with JWT authentication (httpOnly cookie)
- Google OAuth sign-in (links to existing accounts by email)
- Organization accounts — shared namespaces with member roles (owner/member), org profile page, member management
- Repository management (public/private), repo settings page
- Issues with open/close state
- Pull requests with fast-forward, three-way, and squash merge strategies, diff view, and close workflow
- Inline comments with HTMX live updates (no page reload)
- Webhooks — per-repo HTTP callbacks for push, issues, and pull_request events with HMAC signing and delivery log
- In-app notifications — notified on comments, state changes, and merges; unread badge in navbar
- SSH keys for git operations (ED25519, RSA)
- Git over HTTP (smart protocol) — `git clone/push/pull` with HTTP Basic Auth or JWT cookie
- Git over SSH (port 2222 by default) — public key authentication
- Code browser — file tree, blob viewer with line numbers, per-line blame
- Commit history — paginated commit log per branch/ref
- Commit diff view — unified diff with added/deleted line highlighting
- Branch & tag management — list, create, and delete branches/tags via web UI (HTMX, no reload)
- Clone URLs on repo pages (HTTP & SSH)
- Auth-aware navigation (Sign in/Settings/Notifications/Admin/Sign out)
- Admin CLI for bootstrapping
- Single binary ships API + embedded frontend + CSS
- No Node.js/npm required (Tailwind CLI for dev only)
- PostgreSQL (default); see [Configuration Reference](#configuration-reference)

---

## Quick Start

### Option A — Docker (recommended, no local toolchain required)

```bash
# Build image and start
make docker-build
make docker-run

# Run migrations
docker exec -it cloudzilla-cloudzilla-1 /app/cloudzilla-cli migrate

# Open the app — you'll be redirected to /setup to create your superadmin account
open http://localhost:8080
```

All data (git repos, SSH host key) persists in the `cloudzilla_data` named volume at `/data`. PostgreSQL data persists in the `cloudzilla_pg_data` named volume.
Set `CZ_AUTH_JWT_SECRET` in `docker-compose.yml` to a strong secret before exposing publicly.

```bash
make docker-down        # Stop and remove containers
```

---

### Option B — Local (Go + Tailwind)

### Prerequisites

- Go 1.23+
- PostgreSQL 14+
- (Optional) Tailwind CLI for local CSS development

### 1. Install dependencies

```bash
go mod tidy
make setup-tailwind     # One-time: download Tailwind CLI
make download-mermaid   # One-time: download mermaid.min.js (auto-runs in make dev/build)
```

### 2. Configure

Copy and edit `config.yaml`:

```yaml
server:
  port: 8080
  host: "0.0.0.0"

database:
  driver: postgres
  dsn: postgres://cloudzilla:cloudzilla@localhost/cloudzilla?sslmode=disable

auth:
  jwt_secret: change-me # change this in production
  jwt_expiry: 24h

git:
  repos_root: ./git-repos

# Optional: Google OAuth (leave empty to disable)
oauth:
  google_client_id: ""
  google_client_secret: ""
  google_redirect_url: "http://localhost:8080/auth/google/callback"
```

### 3. Run migrations

```bash
make migrate
```

### 4. Start the dev server

```bash
make dev
# Backend + Tailwind watch: http://localhost:8080
# First visit redirects to /setup — create your superadmin account there
```

---

## Docker Deployment

The fastest path to a self-hosted Cloudzilla instance — no Go or Tailwind required on the host.

### Prerequisites

- Docker 24+ with the Compose plugin (`docker compose`)

### 1. Build and start

```bash
make docker-build   # Builds cloudzilla:latest
make docker-run     # docker compose up -d
```

### 2. First-time bootstrap

```bash
docker exec -it cloudzilla-cloudzilla-1 /app/cloudzilla-cli migrate
```

Then open `http://localhost:8080` — you will be redirected to `/setup` to create the superadmin account via the web wizard.

### 3. Access

```
http://localhost:8080       # Web UI
ssh://git@localhost:2222    # SSH git transport
```

### Persistent data

All state lives in named Docker volumes:

| What             | Volume               | Container path                                             |
| ---------------- | -------------------- | ---------------------------------------------------------- |
| PostgreSQL data  | `cloudzilla_pg_data` | (managed by PostgreSQL container)                          |
| Git repositories | `cloudzilla_data`    | `/data/git-repos/`                                         |
| SSH host key     | `cloudzilla_data`    | `/data/cloudzilla_host_key` (auto-generated on first boot) |

### Configuration

Override any setting via environment variables in `docker-compose.yml` using the `CZ_` prefix (Viper auto-maps `CZ_AUTH_JWT_SECRET` → `auth.jwt_secret`, etc.).

**Before exposing publicly**, update `CZ_AUTH_JWT_SECRET` to a long random string:

```yaml
environment:
  CZ_AUTH_JWT_SECRET: "replace-with-a-long-random-string"
```

### Logs and teardown

```bash
docker compose logs -f      # Follow logs
make docker-down            # Stop and remove containers (volumes are preserved)
docker compose down -v      # Also remove the data volumes (destructive)
```

---

## Production Deployment (Binary / systemd)

### 1. Build the binary

On your dev machine (requires Go 1.23+):

```bash
make build
```

Produces:

- `dist/cloudzilla` — HTTP server binary (API + embedded frontend + CSS)
- `dist/cloudzilla-cli` — Admin CLI binary

No Node.js required at runtime or build time. The binary embeds all templates and compiled CSS.

### 2. Copy files to the server

```bash
scp dist/cloudzilla dist/cloudzilla-cli user@yourserver:/usr/local/bin/
```

### 3. Create a config directory and write `config.yaml`

```bash
sudo mkdir -p /etc/cloudzilla
sudo mkdir -p /var/lib/cloudzilla/git-repos
```

`/etc/cloudzilla/config.yaml`:

```yaml
server:
  port: 8080
  host: "0.0.0.0"

database:
  driver: postgres
  dsn: postgres://cloudzilla:strongpassword@localhost/cloudzilla?sslmode=disable

auth:
  jwt_secret: "replace-with-a-long-random-string"
  jwt_expiry: 24h
  cookie_name: cz_token

git:
  repos_root: /var/lib/cloudzilla/git-repos
  ssh_port: 2222
  ssh_host_key: /etc/cloudzilla/ssh_host_key
```

### 4. Generate the SSH host key

Generate a dedicated host key on the server — do **not** copy the one from your dev machine:

```bash
ssh-keygen -t ed25519 -f /etc/cloudzilla/ssh_host_key -N ""
```

This key is stable — clients store its fingerprint in `~/.ssh/known_hosts`. Replacing it later will cause `WARNING: REMOTE HOST IDENTIFICATION HAS CHANGED` errors for existing users.

### 5. Run migrations

```bash
cloudzilla-cli migrate --config /etc/cloudzilla/config.yaml
```

### 6. Create the superadmin account

Open `http://yourdomain.com` in a browser — you will be redirected to `/setup` to create the superadmin account. No CLI step required.

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

### 8. Expose via reverse proxy (recommended)

Run Nginx or Caddy in front of Cloudzilla on port 443. Example Caddy config:

```
yourdomain.com {
    reverse_proxy localhost:8080
}
```

> **Note:** SSH git traffic (port 2222) bypasses the reverse proxy — open that port directly in your firewall if needed.

---

## Configuration Reference

| Key                          | Default                                      | Description                                    |
| ---------------------------- | -------------------------------------------- | ---------------------------------------------- |
| `server.port`                | `8080`                                       | HTTP listen port                               |
| `server.host`                | `0.0.0.0`                                    | HTTP listen address                            |
| `server.read_timeout`        | `15s`                                        | HTTP read timeout                              |
| `server.write_timeout`       | `15s`                                        | HTTP write timeout                             |
| `database.driver`            | `postgres`                                   | `postgres` (default)                           |
| `database.dsn`               | —                                            | PostgreSQL connection string                   |
| `database.max_open_conns`    | `10`                                         | Max open DB connections                        |
| `database.max_idle_conns`    | `5`                                          | Max idle DB connections                        |
| `auth.jwt_secret`            | `change-me`                                  | JWT signing secret — **change in production**  |
| `auth.jwt_expiry`            | `24h`                                        | JWT token lifetime                             |
| `auth.cookie_name`           | `cz_token`                                   | httpOnly cookie name                           |
| `git.repos_root`             | `./git-repos`                                | Bare git repo storage path                     |
| `git.ssh_port`               | `2222`                                       | SSH server port for git operations             |
| `git.ssh_host_key`           | `./cloudzilla_host_key`                      | SSH host key file (auto-generated if missing)  |
| `oauth.google_client_id`     | `""`                                         | Google OAuth client ID (empty = disabled)      |
| `oauth.google_client_secret` | `""`                                         | Google OAuth client secret                     |
| `oauth.google_redirect_url`  | `http://localhost:8080/auth/google/callback` | OAuth redirect URI (must match Google Console) |

---

## CLI Reference

All commands accept `--config <path>` to override the default config file location.

### `cloudzilla migrate`

Run pending database migrations.

```bash
cloudzilla migrate
cloudzilla migrate --config /etc/cloudzilla/config.yaml
```

User and repository management is handled through the web UI — use `/setup` for the superadmin account and the invite system for subsequent users.

---

## SSH Keys & Git Operations

### Managing SSH Keys

Users can add SSH public keys (ED25519, RSA) via the Settings page or API. Keys are stored with MD5 fingerprints for fast lookups during SSH handshakes.

#### Web UI

1. Log in to Cloudzilla
2. Go to `/settings`
3. Add SSH Key section: paste your public key and give it a name
4. Use the generated clone URL (`ssh://git@host:port/owner/repo.git`) with `git clone`

#### CLI / API

```bash
# Add SSH key
curl -X POST http://localhost:8080/api/user/keys \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "title": "laptop",
    "public_key": "ssh-ed25519 AAAA..."
  }'

# List keys
curl http://localhost:8080/api/user/keys \
  -H "Authorization: Bearer $TOKEN"

# Delete key
curl -X DELETE http://localhost:8080/api/user/keys/{id} \
  -H "Authorization: Bearer $TOKEN"
```

### Git over HTTP (Smart Protocol)

Clone, fetch, and push using HTTP with standard `git` commands.

```bash
# Clone public repo (no auth required)
git clone http://localhost:8080/owner/repo.git

# Clone private repo (with HTTP Basic Auth)
git clone http://user:password@localhost:8080/owner/private-repo.git

# Push requires write access
git push origin main
```

**Permissions:**

- Public repos: anyone can clone/fetch
- Private repos: requires authentication (HTTP Basic Auth or JWT cookie)
- Push: requires write access (owner, org owner, or `writer`/`admin` permission role)

### Git over SSH

Use SSH (port 2222 by default) for cloning, fetching, and pushing with public key authentication.

```bash
# Generate SSH key (if you don't have one)
ssh-keygen -t ed25519 -f ~/.ssh/id_ed25519

# Add your key via web UI (/settings) or API
cat ~/.ssh/id_ed25519.pub | xclip -i  # Copy to clipboard

# Clone via SSH
git clone ssh://git@localhost:2222/owner/repo.git

# Standard git operations work
git pull
git push origin main
git fetch
```

**Configuration** — edit `config.yaml`:

```yaml
git:
  ssh_port: 2222 # SSH server port
  ssh_host_key: ./cloudzilla_host_key # Host key file
```

**Dev**: the host key is auto-generated on first startup if the file is missing. `cloudzilla_host_key` is gitignored — do not commit it.

**Production**: generate the host key explicitly on the server (see [Production Deployment](#production-deployment)). Never copy the dev machine's key to production.

---

## Permission Model

### Repository permissions

The repo **owner** is stored as `owner_id` on the repository row — not in the permissions table. Collaborator roles (`reader`, `writer`, `admin`) are rows in the `permissions` table and only grant the access shown below.

| Who                          | Read private | Push (write) | Manage collaborators | Transfer ownership |
| ---------------------------- | :----------: | :----------: | :------------------: | :----------------: |
| Repo owner (`owner_id`)      |      ✓       |      ✓       |          ✓           |         ✓          |
| Org `owner` (org repos)      |      ✓       |      ✓       |          ✓           |         ✗          |
| Collaborator: `admin`        |      ✓       |      ✓       |          ✗           |         ✗          |
| Collaborator: `writer`       |      ✓       |      ✓       |          ✗           |         ✗          |
| Collaborator: `reader`       |      ✓       |      ✗       |          ✗           |         ✗          |
| Org `member` (no collab row) |      ✗       |      ✗       |          ✗           |         ✗          |
| Unauthenticated              | Public only  |      ✗       |          ✗           |         ✗          |

> `admin` and `writer` currently have identical effective access. The `admin` role is reserved for future capabilities (e.g. managing issues/PR settings) that don't extend to collaborator management.

### Organization permissions

| Who          | Create repo | Add/remove members |   Transfer org ownership   |
| ------------ | :---------: | :----------------: | :------------------------: |
| Org `owner`  |      ✓      |         ✓          | ✓ (demotes self to member) |
| Org `member` |      ✗      |         ✗          |             ✗              |

Org `member` role exists for membership visibility only. All management requires `owner` role.

---

## Code Browser

Browse repository contents directly from the web UI. All views respect repo visibility — private repos require authentication.

### URL Patterns

| View                | URL                                     |
| ------------------- | --------------------------------------- |
| Root file tree      | `/{owner}/{repo}/tree/{ref}`            |
| Subdirectory tree   | `/{owner}/{repo}/tree/{ref}/{path...}`  |
| File content (blob) | `/{owner}/{repo}/blob/{ref}/{path...}`  |
| Per-line blame      | `/{owner}/{repo}/blame/{ref}/{path...}` |
| Commit log          | `/{owner}/{repo}/commits/{ref}`         |
| Single commit diff  | `/{owner}/{repo}/commit/{sha}`          |
| Branches & Tags     | `/{owner}/{repo}/refs`                  |
| Stargazers          | `/{owner}/{repo}/stargazers`            |
| User starred repos  | `/{owner}/stars`                        |

`{ref}` can be a branch name, tag name, or commit SHA. If the ref is not found, the server returns 404.

### Examples

```bash
# Browse root of main branch
http://localhost:8080/admin/my-project/tree/main

# Browse a subdirectory
http://localhost:8080/admin/my-project/tree/main/src/handler

# View a file with line numbers
http://localhost:8080/admin/my-project/blob/main/README.md

# View per-line blame for a file
http://localhost:8080/admin/my-project/blame/main/internal/service/repo_service.go

# Use a commit SHA as the ref
http://localhost:8080/admin/my-project/tree/abc1234
```

### Features

- **File tree**: directories listed before files, each entry links to subtree or blob
- **Blob viewer**: syntax-highlighted line numbers, anchor links per line (`#L42`), "View Blame" link
- **Blame view**: groups consecutive lines by commit — shows hash, author, and date once per run; "View File" returns to blob
- **Commit log**: paginated list of commits for a branch or ref (`?page=N`), each links to the diff view
- **Commit diff**: unified diff per file with green/red line highlighting, hunk headers, and binary detection
- **Binary files**: blob page shows "Binary file not shown" instead of raw bytes
- **Empty repos**: returns a clear error rather than crashing
- **Ref badge**: clickable badge on tree/commits pages links to the Branches & Tags page

### Pull Request Diff & Merge

The PR detail page (`/{owner}/{repo}/pulls/{number}`) shows:

- **Diff view**: all files changed between base and head branch tips — added/deleted lines highlighted, hunk headers, binary detection. Only shown for open PRs.
- **Merge strategies**: up to three buttons appear depending on branch state:
  - **Merge (fast-forward)** — visible only when head is a direct descendant of base. Advances the base branch ref with no new commit.
  - **Create merge commit** — visible when either FF or clean three-way merge is possible. Creates a new commit with two parents (base and head).
  - **Squash and merge** — visible under the same conditions. Collapses all head commits into a single new commit on top of base.
- **Conflict warning**: shown when both branches have edited the same file(s). All merge buttons are hidden; the developer must rebase locally and push.

---

## API Reference

All JSON endpoints are under `/api/`. Authentication uses a JWT in an httpOnly cookie (`cz_token`) or an `Authorization: Bearer <token>` header.

### Auth

| Method | Path                    | Auth | Description                                                     |
| ------ | ----------------------- | ---- | --------------------------------------------------------------- |
| POST   | `/api/auth/login`       | —    | Login; sets `cz_token` cookie and returns token in body         |
| POST   | `/api/auth/logout`      | —    | Clears the auth cookie                                          |
| GET    | `/auth/google`          | —    | Begin Google OAuth flow (redirects to Google)                   |
| GET    | `/auth/google/callback` | —    | Google OAuth callback; sets `cz_token` cookie, redirects to `/` |

### SSH Keys

| Method | Path                 | Auth     | Description                          |
| ------ | -------------------- | -------- | ------------------------------------ |
| GET    | `/api/user/keys`     | Required | List SSH keys for authenticated user |
| POST   | `/api/user/keys`     | Required | Add a new SSH public key             |
| DELETE | `/api/user/keys/:id` | Required | Delete an SSH key by ID              |

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

### Labels

| Method | Path                                                     | Auth         | Description                                   |
| ------ | -------------------------------------------------------- | ------------ | --------------------------------------------- |
| GET    | `/api/repos/:owner/:repo/labels/`                        | —            | List all labels for a repository              |
| POST   | `/api/repos/:owner/:repo/labels/`                        | Write access | Create label (`name`, `color`, `description`) |
| DELETE | `/api/repos/:owner/:repo/labels/:id`                     | Write access | Delete label by ID                            |
| POST   | `/api/repos/:owner/:repo/issues/:number/labels/:labelID` | Write access | Add label to issue (HTMX-aware)               |
| DELETE | `/api/repos/:owner/:repo/issues/:number/labels/:labelID` | Write access | Remove label from issue (HTMX-aware)          |
| POST   | `/api/repos/:owner/:repo/pulls/:number/labels/:labelID`  | Write access | Add label to pull request (HTMX-aware)        |
| DELETE | `/api/repos/:owner/:repo/pulls/:number/labels/:labelID`  | Write access | Remove label from pull request (HTMX-aware)   |

### Assignees

| Method | Path                                                          | Auth         | Description                                                   |
| ------ | ------------------------------------------------------------- | ------------ | ------------------------------------------------------------- |
| POST   | `/api/repos/:owner/:repo/issues/:number/assignees`            | Write access | Add assignee to issue (`username` in body; HTMX-aware)        |
| DELETE | `/api/repos/:owner/:repo/issues/:number/assignees?username=X` | Write access | Remove assignee from issue (HTMX-aware)                       |
| POST   | `/api/repos/:owner/:repo/pulls/:number/assignees`             | Write access | Add assignee to pull request (`username` in body; HTMX-aware) |
| DELETE | `/api/repos/:owner/:repo/pulls/:number/assignees?username=X`  | Write access | Remove assignee from pull request (HTMX-aware)                |

### Stars

| Method | Path                                 | Auth     | Description                           |
| ------ | ------------------------------------ | -------- | ------------------------------------- |
| POST   | `/api/repos/:owner/:repo/star`       | Required | Star a repository (HTMX-aware)        |
| DELETE | `/api/repos/:owner/:repo/star`       | Required | Unstar a repository (HTMX-aware)      |
| GET    | `/api/repos/:owner/:repo/stargazers` | —        | List users who starred the repository |

### Pull Requests

| Method | Path                                    | Auth     | Description                                                                                                                                                       |
| ------ | --------------------------------------- | -------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| GET    | `/api/repos/:owner/:repo/pulls/`        | —        | List pull requests                                                                                                                                                |
| POST   | `/api/repos/:owner/:repo/pulls/`        | Required | Create a pull request                                                                                                                                             |
| GET    | `/api/repos/:owner/:repo/pulls/:number` | —        | Get PR details                                                                                                                                                    |
| PATCH  | `/api/repos/:owner/:repo/pulls/:number` | Required | Update PR state. `state=merged` merges the PR using the strategy in `merge_strategy` (`ff` default, `merge`, or `squash`); `state=closed` closes without merging. |

### Branches & Tags

| Method | Path                                      | Auth     | Description                                                                   |
| ------ | ----------------------------------------- | -------- | ----------------------------------------------------------------------------- |
| POST   | `/api/repos/:owner/:repo/branches`        | Required | Create branch (`name`, `from` form fields; `from` defaults to default branch) |
| DELETE | `/api/repos/:owner/:repo/branches?name=…` | Required | Delete branch (default branch rejected with 400)                              |
| POST   | `/api/repos/:owner/:repo/tags`            | Required | Create tag (`name`, `from` form fields)                                       |
| DELETE | `/api/repos/:owner/:repo/tags?name=…`     | Required | Delete tag                                                                    |

All four endpoints require write access. For HTMX requests they return an HTML fragment; otherwise JSON.

### Git Operations (HTTP Smart Protocol)

| Method | Path                                              | Auth      | Description               |
| ------ | ------------------------------------------------- | --------- | ------------------------- |
| GET    | `/:owner/:repo/info/refs?service=git-upload-pack` | Depends\* | List refs (clone/fetch)   |
| POST   | `/:owner/:repo/git-upload-pack`                   | Depends\* | Upload pack (clone/fetch) |
| POST   | `/:owner/:repo/git-receive-pack`                  | Depends\* | Receive pack (push)       |

\*Depends on repo privacy and user permissions:

- Public repos: no auth required for read
- Private repos: requires HTTP Basic Auth or JWT cookie for read
- Push: requires write permission (owner or `writer`/`admin` role)

### Instance Admin (superadmin only)

| Method | Path                         | Auth       | Description                                               |
| ------ | ---------------------------- | ---------- | --------------------------------------------------------- |
| GET    | `/admin/settings`            | Superadmin | Admin panel: instance settings + invitation management    |
| POST   | `/api/admin/settings`        | Superadmin | Toggle a setting (`key`, `value` form fields; HTMX-aware) |
| POST   | `/api/admin/invitations`     | Superadmin | Create invitation (`email` form field; HTMX-aware)        |
| DELETE | `/api/admin/invitations/:id` | Superadmin | Delete an invitation (HTMX-aware)                         |

### Setup & Invitations

| Method | Path             | Auth | Description                                         |
| ------ | ---------------- | ---- | --------------------------------------------------- |
| GET    | `/setup`         | —    | First-run wizard (redirects to `/` when setup done) |
| POST   | `/setup`         | —    | Submit setup form (creates superadmin, sets cookie) |
| GET    | `/invite/:token` | —    | Invitation acceptance form                          |
| POST   | `/invite/:token` | —    | Accept invitation (creates user, sets auth cookie)  |

### Repository Collaborators

| Method | Path                                              | Auth                       | Description                                            |
| ------ | ------------------------------------------------- | -------------------------- | ------------------------------------------------------ |
| GET    | `/api/repos/:owner/:repo/collaborators`           | Optional                   | List collaborators with usernames                      |
| POST   | `/api/repos/:owner/:repo/collaborators`           | Required + owner/org-owner | Add collaborator (`username`, `role`)                  |
| DELETE | `/api/repos/:owner/:repo/collaborators?user_id=N` | Required + owner/org-owner | Remove collaborator by user ID                         |
| POST   | `/api/repos/:owner/:repo/transfer`                | Required + repo owner      | Transfer repo to another user (`new_owner` form field) |

Only the repo owner (or an org owner for org repos) can add/remove collaborators. Collaborators with the `admin` role cannot manage other collaborators.

### Organizations

| Method | Path                               | Auth     | Description                                                                         |
| ------ | ---------------------------------- | -------- | ----------------------------------------------------------------------------------- |
| POST   | `/api/orgs/`                       | Required | Create organization                                                                 |
| GET    | `/api/orgs/:org`                   | —        | Get organization by name                                                            |
| GET    | `/api/orgs/:org/members`           | —        | List organization members                                                           |
| POST   | `/api/orgs/:org/members`           | Required | Add member (`username`, `role`); owner only                                         |
| DELETE | `/api/orgs/:org/members/:username` | Required | Remove member; owner only; last owner blocked                                       |
| POST   | `/api/orgs/:org/repos`             | Required | Create a repository under the organization; owner only                              |
| POST   | `/api/orgs/:org/transfer`          | Required | Transfer org ownership (`new_owner` form field); owner only; demotes self to member |

### Webhooks

| Method | Path                                           | Auth         | Description                                |
| ------ | ---------------------------------------------- | ------------ | ------------------------------------------ |
| GET    | `/api/repos/:owner/:repo/hooks/`               | —            | List webhooks for repository               |
| POST   | `/api/repos/:owner/:repo/hooks/`               | Write access | Create webhook (`url`, `secret`, `events`) |
| DELETE | `/api/repos/:owner/:repo/hooks/:id`            | Write access | Delete webhook                             |
| GET    | `/api/repos/:owner/:repo/hooks/:id/deliveries` | Write access | List delivery history                      |

Webhooks fire on `push`, `issues`, and `pull_request` events. Requests are signed with `X-Hub-Signature-256` when a secret is configured (GitHub-compatible HMAC-SHA256).

### Notifications

| Method | Path                              | Auth     | Description                        |
| ------ | --------------------------------- | -------- | ---------------------------------- |
| GET    | `/api/notifications/unread-count` | Required | Returns `{"count": N}`             |
| PATCH  | `/api/notifications/:id`          | Required | Mark a single notification as read |
| POST   | `/api/notifications/read-all`     | Required | Mark all notifications as read     |

### HTMX Fragments

| Method | Path                                              | Description                                  |
| ------ | ------------------------------------------------- | -------------------------------------------- |
| GET    | `/fragments/:owner/:repo/issues/:number/comments` | Returns rendered HTML fragment for HTMX swap |

---

## Make Targets

| Target                  | Description                                                   |
| ----------------------- | ------------------------------------------------------------- |
| `make setup-tailwind`   | Download Tailwind CLI (one-time)                              |
| `make download-mermaid` | Download mermaid.min.js (one-time; auto-runs in build/dev)    |
| `make build-css`        | Compile Tailwind CSS to `cmd/server/frontend/static/main.css` |
| `make dev`              | Run backend + Tailwind watch concurrently                     |
| `make build`            | Build Go binaries (with embedded CSS)                         |
| `make build-backend`    | Compile server binary to `dist/cloudzilla`                    |
| `make build-cli`        | Compile CLI binary to `dist/cloudzilla-cli`                   |
| `make migrate`          | Run DB migrations                                             |
| `make lint`             | Run golangci-lint                                             |
| `make test`             | Run Go tests                                                  |
| `make clean`            | Remove build artifacts and database files                     |
| `make docker-build`     | Build Docker image (`cloudzilla:latest`)                      |
| `make docker-run`       | Start with docker compose (detached)                          |
| `make docker-down`      | Stop and remove containers                                    |

---

## Project Structure

```
cmd/
  server/          # HTTP server entrypoint
    frontend/      # Templates + static files (embedded in binary)
      templates/
        layout.html          # Base HTML shell
        pages/               # Page templates (home, login, user, repo, org, org_settings,
        |                    #   repo_settings, issues, pulls, tree, blob, blame, refs,
        |                    #   notifications, setup, invite, admin_settings)
        fragments/           # HTMX swap fragments (issues, pull requests, SSH keys,
                             #   branches/tags, org members, webhooks, notifications,
                             #   admin settings, admin invitations, repo collaborators)
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
  service/         # Business logic (calls stores)
  handler/         # HTTP handlers (page + API + git HTTP)
    page_handler.go          # Page rendering handlers
    ref_handler.go           # Branch & tag create/delete handlers
    org_handler.go           # Organization API handlers
    webhook_handler.go       # Webhook API handlers
    notification_handler.go  # Notification API handlers
    setup_handler.go         # First-run wizard handlers
    admin_handler.go         # Superadmin panel: instance settings + invitations
    invite_handler.go        # Invitation acceptance flow
    viewmodels.go            # Data structs for templates (with BasePage for auth)
    git_http.go              # Git HTTP smart protocol handler
    ssh_key_handler.go       # SSH key management endpoints
  middleware/      # Auth, logger, CORS, RequireSetup (first-run redirect)
  router/          # chi route registration + template parsing
  ssh/             # SSH server for git operations (gliderlabs/ssh)
    server.go      # SSH server implementation
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

| File                           | Creates                                                                           |
| ------------------------------ | --------------------------------------------------------------------------------- |
| `001_create_users.sql`         | `users` table                                                                     |
| `002_create_repositories.sql`  | `repositories` table                                                              |
| `003_create_issues.sql`        | `issues` table                                                                    |
| `004_create_pull_requests.sql` | `pull_requests` table                                                             |
| `005_create_comments.sql`      | `comments` table                                                                  |
| `006_create_permissions.sql`   | `permissions` table                                                               |
| `007_create_ssh_keys.sql`      | `ssh_keys` table                                                                  |
| `008_oauth_users.sql`          | Adds `oauth_provider`, `oauth_id` columns to `users`                              |
| `009_create_organizations.sql` | `organizations` + `org_members` tables                                            |
| `010_repo_owner_name.sql`      | Adds `owner_name` + `org_id` columns to `repositories`                            |
| `011_create_webhooks.sql`      | `webhooks` + `webhook_deliveries` tables                                          |
| `012_create_notifications.sql` | `notifications` table                                                             |
| `013_superadmin.sql`           | Adds `is_superadmin` column to `users`                                            |
| `014_site_settings.sql`        | `site_settings` table (seeded with `allow_registration=true`, `allow_login=true`) |
| `015_invitations.sql`          | `invitations` table; adds `is_invited` column to `users`                          |

---

## Dependencies

### Direct Dependencies

| Package               | Purpose                                                                     | Version |
| --------------------- | --------------------------------------------------------------------------- | ------- |
| `go-chi/chi/v5`       | HTTP router with named params and middleware chaining                       | v5.2.5  |
| `go-chi/cors`         | CORS middleware for chi                                                     | v1.2.2  |
| `golang-jwt/jwt/v5`   | JWT token signing and verification                                          | v5.3.1  |
| `jackc/pgx/v5`        | PostgreSQL driver (stdlib-compatible via pgx/v5/stdlib)                     | v5.8.0  |
| `spf13/cobra`         | CLI command framework with subcommand trees                                 | v1.10.2 |
| `spf13/viper`         | Config file + environment variable loading                                  | v1.21.0 |
| `golang.org/x/crypto` | Secure password hashing (bcrypt, argon2)                                    | v0.49.0 |
| `golang.org/x/oauth2` | OAuth 2.0 client (Google sign-in)                                           | v0.36.0 |
| `go-git/go-git/v5`    | Pure-Go git implementation for clone, fetch, push, and all merge strategies | v5.17.0 |
| `gliderlabs/ssh`      | SSH server library                                                          | v0.3.5+ |

### Notable Indirect Dependencies

| Package                 | Purpose                                      |
| ----------------------- | -------------------------------------------- |
| `go-viper/mapstructure` | Struct mapping used by viper                 |
| `spf13/afero`           | Virtual filesystem abstraction used by viper |
