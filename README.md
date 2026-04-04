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
            Templates["Templ Components"]
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
- **Repository forks** — fork any readable repo into your own namespace with one click; forked repo shows "Forked from owner/name" badge; original repo's fork count increments; forked repo is a fully functional bare git repo (clone, push, pull all work)
- **Releases** — publish versioned releases tied to git tags; supports markdown bodies, draft and prerelease flags; latest release badge on repo page; full CRUD via web UI and API
- **Commit Status API** — CI tools can post build statuses (`pending`, `success`, `failure`, `error`) per SHA and context; combined status aggregated from all contexts; status checks surface on commit pages and PR detail pages
- **Milestones** — create sprint-planning milestones per repo with title, description, and optional due date; assign issues and PRs to milestones via sidebar picker; progress bar tracks open/closed issue counts; open/close milestones; full CRUD via web UI and API
- **PR Reviews** — reviewers submit Approve / Request Changes / Comment reviews on open PRs; any `changes_requested` review blocks all merge buttons until the reviewer re-submits with a different state; reviewer cannot review their own PR; notifications sent to PR author
- **PR Line Comments** — click "+" on any diff line to open an inline comment form (HTMX, no page reload); comments anchor to `path:line` and render below the target line; comment authors can edit their comment inline; repo writers and comment authors can delete comments
- **Code Review Suggestions** — inline ` ```suggestion ` blocks in PR line comments render as a green suggested-change preview; repo writers can apply a suggestion with one click, which writes a new commit directly to the PR head branch
- **Branch Protection** — per-repo glob patterns (e.g. `main`, `release/*`) that enforce required review counts, required commit-status checks, and optional force-push blocking; enforced on both HTTP and SSH push, and on PR merge
- **CODEOWNERS** — repo owners can add a `CODEOWNERS` (or `.github/CODEOWNERS`) file; when a PR is opened, matched owners are automatically added as assignees based on the files changed
- **Auto-merge** — enable auto-merge on any open PR with a chosen strategy (fast-forward, merge commit, or squash); merges automatically once all required reviews are approved and all required commit-status checks pass; can be disabled at any time
- **Issue & PR Templates** — `.github/ISSUE_TEMPLATE.md` and `.github/PULL_REQUEST_TEMPLATE.md` auto-populate the new issue/PR form body when present in the repository
- **Comment Reactions** — emoji reactions on issue and PR comments; per-user toggle; reaction counts displayed inline
- **TOTP Two-Factor Authentication** — optional TOTP 2FA (RFC 6238) on user accounts; QR code setup via settings page; enforced at login with recovery codes
- **Search** — full-text search across repositories, issues, pull requests, and users via PostgreSQL `tsvector` + GIN indexes; search bar in the navbar on every page; tabbed results page (`/search?q=...&type=repos|issues|pulls|users|all`)
- User accounts with JWT authentication (httpOnly cookie)
- Google OAuth sign-in (links to existing accounts by email)
- Organization accounts — shared namespaces with member roles (owner/member), org profile page, member management
- Repository management (public/private), repo settings page
- Issues with open/close state
- Pull requests with fast-forward, three-way, and squash merge strategies, diff view, and close workflow
- Inline comments on issues and PRs with HTMX live updates (no page reload); comment authors can edit their own comments; deletion restricted to the author or a repo writer
- Webhooks — per-repo HTTP callbacks for `push`, `issues`, and `pull_request` events with HMAC signing and delivery log; push events fired on both HTTP and SSH pushes
- In-app notifications — notified on comments, state changes, and merges; unread badge in navbar
- SSH keys for git operations (ED25519, RSA)
- Git over HTTP (smart protocol) — full pack protocol; `git clone/push/pull` with HTTP Basic Auth or JWT cookie; push triggers webhooks
- Git over SSH (port 2222 by default) — full pack protocol; public key authentication; push triggers webhooks
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

| View                | URL                                                                         |
| ------------------- | --------------------------------------------------------------------------- |
| Root file tree      | `/{owner}/{repo}/tree/{ref}`                                                |
| Subdirectory tree   | `/{owner}/{repo}/tree/{ref}/{path...}`                                      |
| File content (blob) | `/{owner}/{repo}/blob/{ref}/{path...}`                                      |
| Per-line blame      | `/{owner}/{repo}/blame/{ref}/{path...}`                                     |
| Commit log          | `/{owner}/{repo}/commits/{ref}`                                             |
| Single commit diff  | `/{owner}/{repo}/commit/{sha}`                                              |
| Branches & Tags     | `/{owner}/{repo}/refs`                                                      |
| Stargazers          | `/{owner}/{repo}/stargazers`                                                |
| User starred repos  | `/{owner}/stars`                                                            |
| Forked repo         | `/{forkOwner}/{forkName}` (shows "Forked from" badge)                       |
| Releases list       | `/{owner}/{repo}/releases`                                                  |
| Release detail      | `/{owner}/{repo}/releases/tag/{tagName}`                                    |
| Milestones list     | `/{owner}/{repo}/milestones`                                                |
| Search results      | `/search?q=...&type=all\|repos\|issues\|pulls\|users`                       |
| Personal tokens     | `/settings/tokens`                                                          |
| Repo settings       | `/{owner}/{repo}/settings` (collaborators, webhooks, deploy keys, transfer) |

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

### Personal Access Tokens

Generate long-lived API tokens scoped to specific capabilities without exposing your password:

- Create tokens at `/settings/tokens` — choose a name, one or more scopes, and an optional expiry date
- Scopes: `repo:read`, `repo:write`, `issues:write`, `pulls:write`
- The raw token (`czp_<hex>`) is displayed **once** at creation time — only its SHA-256 hash is stored
- Use as `Authorization: Bearer czp_<token>` on any API endpoint in place of a JWT cookie
- Tokens can be revoked at any time from the settings page

### Deploy Keys

Per-repository SSH keys for CI/CD pipelines, isolated from user SSH keys:

- Added and managed from `/{owner}/{repo}/settings` — title, public key, and read-only flag
- A read-only deploy key may clone/fetch but cannot push; a read-write key can push
- During SSH auth, deploy keys are checked alongside user SSH keys; access is restricted to the specific repository the key was created for
- Revoke a key at any time from the repository settings page

### Draft Pull Requests

PRs can be opened (or converted) as drafts to signal work in progress:

- Create with `"is_draft": true` in the API, or convert any open PR via `PATCH {"is_draft": true}`
- Draft PRs appear in the **Draft** filter tab on `/{owner}/{repo}/pulls` — excluded from the default **Open** view
- The PR detail page shows a yellow **Draft** banner and a **Ready for review** button; all merge buttons and the review form are hidden
- Click **Ready for review** (or `PATCH {"is_draft": false}`) to convert — merge buttons reappear
- Attempting to merge a draft PR returns `422 cannot merge a draft pull request`

### Pull Request Diff & Merge

The PR detail page (`/{owner}/{repo}/pulls/{number}`) shows:

- **Diff view**: all files changed between base and head branch tips — added/deleted lines highlighted, hunk headers, binary detection. Only shown for open PRs.
- **Merge strategies**: up to three buttons appear depending on branch state:
  - **Merge (fast-forward)** — visible only when head is a direct descendant of base. Advances the base branch ref with no new commit.
  - **Create merge commit** — visible when either FF or clean three-way merge is possible. Creates a new commit with two parents (base and head).
  - **Squash and merge** — visible under the same conditions. Collapses all head commits into a single new commit on top of base.
- **Conflict warning**: shown when both branches have edited the same file(s). All merge buttons are hidden; the developer must rebase locally and push.
- **Merge gate**: if any reviewer has submitted a `changes_requested` review, all merge buttons are replaced with a red banner until the block is cleared.

### PR Reviews

Each PR detail page has a **Reviews** section below the diff:

- Reviewers submit a **Comment**, **Approve**, or **Request changes** review with an optional body
- A reviewer's latest review replaces their previous one (upsert on `(pull_id, author_id)`)
- Any active `changes_requested` review blocks all merge buttons; re-submitting as `approved` or `commented` lifts the block
- A reviewer cannot review their own PR

### PR Line Comments

Inline comments are anchored to specific diff lines:

- A `+` button appears on each diff line for users with write access on open PRs
- Clicking `+` reveals an inline form via HTMX — no page reload
- Comments render below their target line and survive page reloads
- Comment authors can edit their comment inline (author-only; repo writers cannot edit others' comments)
- Repo writers and comment authors can delete comments with the `×` button

### Code Review Suggestions

Inline suggestions let reviewers propose exact text replacements directly in a line comment:

- Write a line comment with a fenced ` ```suggestion ` block containing the replacement text
- The suggestion renders as a green diff preview (removed lines in red, added lines in green) on the PR diff page
- Any user with write access on the repo can click **Apply suggestion** — this writes a new commit to the PR head branch with the suggested change applied
- Suggestion commits are authored as the applying user and appear in the commit history

### Branch Protection

Protect branches from unreviewed or broken pushes by defining glob-matched protection rules:

- Create rules at `/{owner}/{repo}/settings` — each rule has a pattern (e.g. `main`, `release/*`), a required review count, required status check contexts, and an optional force-push block
- **Required reviews**: merging a PR into a protected branch is blocked until at least N Approve reviews (with no outstanding Request Changes) are present
- **Required status checks**: merging is blocked until all named CI contexts report `success` on the PR head SHA
- **Block force push**: the HTTP and SSH receive-pack handlers reject `--force` pushes to matching branches
- Rules are checked on both HTTP and SSH push, and on every PR merge attempt
- Sentinels: `ErrForcePushBlocked`, `ErrPushRequiresPR`, `ErrInsufficientReviews`, `ErrStatusCheckFailed`

### CODEOWNERS

Automatically assign reviewers based on file ownership:

- Place a `CODEOWNERS` or `.github/CODEOWNERS` file in the repository root; format follows the standard `pattern @owner` syntax (one rule per line, `#` comments ignored)
- When a PR is opened, the service resolves which patterns match the changed files and adds the corresponding owners as PR assignees
- Works with both usernames (`@alice`) and multiple owners per line (`@alice @bob`)

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

### Personal Access Tokens

| Method | Path                   | Auth     | Description                                                                                 |
| ------ | ---------------------- | -------- | ------------------------------------------------------------------------------------------- |
| GET    | `/settings/tokens`     | Required | Page: list tokens + create form; shows raw token once via `?new_token=...` after creation   |
| POST   | `/api/user/tokens`     | Required | Create PAT (`name`, `scopes[]`, optional `expires_at`); returns `{"token": "czp_..."}` once |
| DELETE | `/api/user/tokens/:id` | Required | Revoke a PAT by ID                                                                          |

Raw token format: `czp_<32-byte hex>`. Use as `Authorization: Bearer czp_<token>`. Only the SHA-256 hash is stored; the raw value cannot be recovered after creation.

### Deploy Keys

| Method | Path                               | Auth                 | Description                                         |
| ------ | ---------------------------------- | -------------------- | --------------------------------------------------- |
| GET    | `/api/repos/:owner/:repo/keys`     | Required + CanManage | List deploy keys for a repository                   |
| POST   | `/api/repos/:owner/:repo/keys`     | Required + CanManage | Add deploy key (`title`, `public_key`, `read_only`) |
| DELETE | `/api/repos/:owner/:repo/keys/:id` | Required + CanManage | Delete a deploy key by ID                           |

Deploy keys authenticate via SSH using the key's MD5 fingerprint. A `read_only` key cannot push; a read-write key can. Each key is scoped to a single repository — attempting to use it against another repo is rejected.

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

| Method | Path                                                  | Auth     | Description                              |
| ------ | ----------------------------------------------------- | -------- | ---------------------------------------- |
| GET    | `/api/repos/:owner/:repo/issues/`                     | —        | List issues                              |
| POST   | `/api/repos/:owner/:repo/issues/`                     | Required | Create an issue                          |
| GET    | `/api/repos/:owner/:repo/issues/:number`              | —        | Get issue details                        |
| PATCH  | `/api/repos/:owner/:repo/issues/:number`              | Required | Update issue (open/close)                |
| GET    | `/api/repos/:owner/:repo/issues/:number/comments`     | —        | List comments                            |
| POST   | `/api/repos/:owner/:repo/issues/:number/comments`     | Required | Add a comment                            |
| PATCH  | `/api/repos/:owner/:repo/issues/:number/comments/:id` | Required | Edit a comment body (author only)        |
| DELETE | `/api/repos/:owner/:repo/issues/:number/comments/:id` | Required | Delete a comment (author or repo writer) |

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

### Forks

| Method | Path                           | Auth     | Description                                                                        |
| ------ | ------------------------------ | -------- | ---------------------------------------------------------------------------------- |
| POST   | `/api/repos/:owner/:repo/fork` | Required | Fork the repository into the authenticated user's namespace; redirects to fork URL |

### Pull Requests

| Method | Path                                    | Auth     | Description                                                                                                                                                       |
| ------ | --------------------------------------- | -------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| GET    | `/api/repos/:owner/:repo/pulls/`        | —        | List pull requests                                                                                                                                                |
| POST   | `/api/repos/:owner/:repo/pulls/`        | Required | Create a pull request                                                                                                                                             |
| GET    | `/api/repos/:owner/:repo/pulls/:number` | —        | Get PR details                                                                                                                                                    |
| PATCH  | `/api/repos/:owner/:repo/pulls/:number` | Required | Update PR state. `state=merged` merges the PR using the strategy in `merge_strategy` (`ff` default, `merge`, or `squash`); `state=closed` closes without merging. |

### PR Reviews

| Method | Path                                            | Auth     | Description                                                                                                           |
| ------ | ----------------------------------------------- | -------- | --------------------------------------------------------------------------------------------------------------------- |
| GET    | `/api/repos/:owner/:repo/pulls/:number/reviews` | Optional | List all reviews for a pull request                                                                                   |
| POST   | `/api/repos/:owner/:repo/pulls/:number/reviews` | Required | Submit or update a review (`state`, `body`); upserts per reviewer; `state=changes_requested` blocks all merge buttons |

Valid `state` values: `approved`, `changes_requested`, `commented`, `pending`.

### PR Line Comments

| Method | Path                                                       | Auth     | Description                                                    |
| ------ | ---------------------------------------------------------- | -------- | -------------------------------------------------------------- |
| GET    | `/api/repos/:owner/:repo/pulls/:number/line_comments`      | Optional | List all line comments for a pull request                      |
| POST   | `/api/repos/:owner/:repo/pulls/:number/line_comments`      | Required | Create a line comment (`path`, `line`, `body`, `diff_side`)    |
| GET    | `/api/repos/:owner/:repo/pulls/:number/line_comments/form` | Required | Returns inline comment form HTML fragment (`?path=...&line=N`) |
| PATCH  | `/api/repos/:owner/:repo/pulls/:number/line_comments/:id`  | Required | Edit a line comment body (author only)                         |
| DELETE | `/api/repos/:owner/:repo/pulls/:number/line_comments/:id`  | Required | Delete a line comment (author or repo writer only)             |

### Branch Protections

| Method | Path                                       | Auth   | Description                                                                                      |
| ------ | ------------------------------------------ | ------ | ------------------------------------------------------------------------------------------------ |
| GET    | `/{owner}/{repo}/branches/protections`     | Manage | List all branch protection rules for the repository                                              |
| POST   | `/{owner}/{repo}/branches/protections`     | Manage | Create a rule (`pattern`, `require_review_count`, `require_status_checks[]`, `block_force_push`) |
| PATCH  | `/{owner}/{repo}/branches/protections/:id` | Manage | Update an existing rule (same fields as POST; only provided fields are changed)                  |
| DELETE | `/{owner}/{repo}/branches/protections/:id` | Manage | Delete a protection rule                                                                         |

### Search

| Method | Path      | Auth     | Description                                                                                                                                        |
| ------ | --------- | -------- | -------------------------------------------------------------------------------------------------------------------------------------------------- |
| GET    | `/search` | Optional | Full-text search. Query params: `q` (search term), `type` (`all`, `repos`, `issues`, `pulls`, `users`). Private repos visible only to their owner. |

### Releases

| Method | Path                                      | Auth         | Description                                                                |
| ------ | ----------------------------------------- | ------------ | -------------------------------------------------------------------------- |
| GET    | `/api/repos/:owner/:repo/releases`        | —            | List releases                                                              |
| POST   | `/api/repos/:owner/:repo/releases`        | Write access | Create a release (`tag_name`, `name`, `body`, `is_prerelease`, `is_draft`) |
| GET    | `/api/repos/:owner/:repo/releases/latest` | —            | Get the latest non-draft release                                           |
| GET    | `/api/repos/:owner/:repo/releases/:id`    | —            | Get release by ID                                                          |
| PATCH  | `/api/repos/:owner/:repo/releases/:id`    | Write access | Update a release                                                           |
| DELETE | `/api/repos/:owner/:repo/releases/:id`    | Write access | Delete a release; HTMX requests return `HX-Redirect` to the releases list  |

### Commit Statuses

| Method | Path                                          | Auth     | Description                                                                 |
| ------ | --------------------------------------------- | -------- | --------------------------------------------------------------------------- |
| POST   | `/api/repos/:owner/:repo/statuses/:sha`       | Required | Create or update a status (`state`, `context`, `target_url`, `description`) |
| GET    | `/api/repos/:owner/:repo/statuses/:sha`       | —        | List all statuses for a commit SHA                                          |
| GET    | `/api/repos/:owner/:repo/commits/:sha/status` | —        | Get combined status (`{state, statuses:[]}`) — aggregated from all contexts |

Valid `state` values: `pending`, `success`, `failure`, `error`. Combined state uses worst-case: `error` > `failure` > `pending` > `success`.

### Milestones

| Method | Path                                               | Auth         | Description                                                    |
| ------ | -------------------------------------------------- | ------------ | -------------------------------------------------------------- |
| GET    | `/api/repos/:owner/:repo/milestones`               | —            | List all milestones (with open/closed issue counts)            |
| POST   | `/api/repos/:owner/:repo/milestones`               | Write access | Create a milestone (`title`, `description`, `due_date`)        |
| GET    | `/api/repos/:owner/:repo/milestones/:number`       | —            | Get milestone by number                                        |
| PATCH  | `/api/repos/:owner/:repo/milestones/:number`       | Write access | Update or change state (`state=closed`/`open` to close/reopen) |
| DELETE | `/api/repos/:owner/:repo/milestones/:number`       | Write access | Delete a milestone (issues/PRs have `milestone_id` cleared)    |
| POST   | `/api/repos/:owner/:repo/issues/:number/milestone` | Write access | Set or remove milestone on an issue (`milestone_id` in body)   |
| POST   | `/api/repos/:owner/:repo/pulls/:number/milestone`  | Write access | Set or remove milestone on a pull request                      |

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

- Public repos: no auth required for clone/fetch; push always requires auth
- Private repos: requires HTTP Basic Auth or JWT cookie for clone/fetch
- Push (`git-receive-pack`): requires write permission (owner or `writer`/`admin` role); unauthenticated requests to the receive-pack info/refs endpoint receive `401 Unauthorized` with a `WWW-Authenticate: Basic` challenge
- Both HTTP and SSH pushes fire `push` webhooks per updated branch

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

## Roadmap

| Phase   | Feature                                      | Status     | Migration(s) |
| ------- | -------------------------------------------- | ---------- | ------------ |
| 0.1–5.3 | Core Platform → Draft PRs                    | ✅ Done    | 001–029      |
| 6.1–6.3 | Protected Branches → Code Review Suggestions | ✅ Done    | 030–031      |
| 7.1–7.3 | Auto-merge, Issue & PR Templates, Reactions  | ✅ Done    | 032–033      |
| 8.1     | TOTP Two-Factor Authentication               | ✅ Done    | 034          |
| 8.2     | Audit Log                                    | ⬜ Planned | 035          |
| 8.3     | LDAP / SAML SSO                              | ⬜ Planned | 036          |
| 9.1     | Project Boards / Kanban                      | ✅ Done    | 036          |
| 9.2     | Wiki                                         | ✅ Done    | —            |
| 9.3     | Issue Pinning & Locking                      | ⬜ Planned | 039          |
| 10.1    | Repository Insights & Stats                  | ⬜ Planned | —            |
| 10.2    | @Mentions in Comments                        | ⬜ Planned | 040          |
| 10.3    | Saved Replies                                | ⬜ Planned | 041          |
| 11.1    | Email Notifications (SMTP)                   | ⬜ Planned | 042          |
| 11.2    | OAuth Apps / Third-party Clients             | ⬜ Planned | 043          |
| 11.3    | Webhook Improvements                         | ⬜ Planned | 044          |
| 12.1    | Watching                                     | ⬜ Planned | 045          |
| 12.2    | Activity Feed                                | ⬜ Planned | 046          |
| 12.3    | Discussions                                  | ⬜ Planned | 047          |
| 13.1    | Gists                                        | ⬜ Planned | 048          |
| 13.2    | Profile README                               | ⬜ Planned | —            |
| 13.3    | Repository Topics / Tags                     | ⬜ Planned | 049          |
| 14.1    | Private Issues                               | ⬜ Planned | 050          |
| 14.2    | Repository Archive & Templates               | ⬜ Planned | 051          |
| 14.3    | Soft-delete & Recovery                       | ⬜ Planned | 052          |
| 15.1    | Advanced Code Search                         | ⬜ Planned | 053          |
| 15.2    | Explore / Trending                           | ⬜ Planned | 054          |
| 15.3    | Dependency Graph                             | ⬜ Planned | 055          |

> Full specs for all phases: [docs/roadmap.md](./docs/roadmap.md). Implementation plans: [docs/superpowers/plans/](./docs/superpowers/plans/).

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
    frontend/      # Static files (embedded in binary)
      static/
        main.css             # Compiled Tailwind output
      htmx.min.js            # HTMX library
  cloudzilla/      # Admin CLI (cobra)
internal/
  config/          # Config loading (viper + YAML)
  db/              # DB connection + migration runner
    migrations/    # SQL migration files (embedded via embed.FS)
  model/           # Data structs (db + json tags)
  store/           # Store layer (raw SQL queries via sqlx)
  service/         # Business logic (calls stores)
  handler/         # HTTP handlers (page + API + git HTTP)
  middleware/      # Auth, logger, CORS, RequireSetup (first-run redirect)
  router/          # chi route registration
  ssh/             # SSH server for git operations (gliderlabs/ssh)
  view/            # Templ components (compiled to _templ.go files)
    layout/        # Base layout component
    pages/         # Page components (one per page)
    fragments/     # HTMX fragment components
tailwind/          # Tailwind CSS configuration
  input.css        # Tailwind directives
  tailwind.config.js         # Theme customization
docs/
  superpowers/
    plans/         # Implementation plans for phases 8.2–15.3
config.yaml        # Default configuration
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

| File                                      | Creates                                                                                                         |
| ----------------------------------------- | --------------------------------------------------------------------------------------------------------------- |
| `001_create_users.sql`                    | `users` table                                                                                                   |
| `002_create_repositories.sql`             | `repositories` table                                                                                            |
| `003_create_issues.sql`                   | `issues` table                                                                                                  |
| `004_create_pull_requests.sql`            | `pull_requests` table                                                                                           |
| `005_create_comments.sql`                 | `comments` table                                                                                                |
| `006_create_permissions.sql`              | `permissions` table                                                                                             |
| `007_create_ssh_keys.sql`                 | `ssh_keys` table                                                                                                |
| `008_oauth_users.sql`                     | Adds `oauth_provider`, `oauth_id` columns to `users`                                                            |
| `009_create_organizations.sql`            | `organizations` + `org_members` tables                                                                          |
| `010_repo_owner_name.sql`                 | Adds `owner_name` + `org_id` columns to `repositories`                                                          |
| `011_create_webhooks.sql`                 | `webhooks` + `webhook_deliveries` tables                                                                        |
| `012_create_notifications.sql`            | `notifications` table                                                                                           |
| `013_superadmin.sql`                      | Adds `is_superadmin` column to `users`                                                                          |
| `014_site_settings.sql`                   | `site_settings` table (seeded with `allow_registration=true`, `allow_login=true`)                               |
| `015_invitations.sql`                     | `invitations` table; adds `is_invited` column to `users`                                                        |
| `016_create_labels.sql`                   | `labels`, `issue_labels`, `pull_labels` tables                                                                  |
| `017_create_assignees.sql`                | `issue_assignees`, `pull_assignees` tables                                                                      |
| `018_create_stars.sql`                    | `stars` table + `idx_stars_repo`, `idx_stars_user` indexes                                                      |
| `019_add_fork_columns.sql`                | Adds `is_fork`, `fork_of_id`, `fork_count` columns to `repositories` + index                                    |
| `020_create_releases.sql`                 | `releases` table + `idx_releases_repo` index                                                                    |
| `021_create_commit_statuses.sql`          | `commit_statuses` table + `idx_commit_statuses_repo_sha` index                                                  |
| `022_create_milestones.sql`               | `milestones` table; adds `milestone_id` FK to `issues` and `pull_requests`                                      |
| `023_create_pr_reviews.sql`               | `pull_reviews` table with `UNIQUE(pull_id, author_id)` upsert constraint                                        |
| `024_create_pull_line_comments.sql`       | `pull_line_comments` table with path + line indexes                                                             |
| `025_search_indexes.sql`                  | `search_vector tsvector` columns + GIN indexes + triggers on repos/issues/PRs; `idx_users_username_lower` index |
| `026_fix_comments_author_name.sql`        | Backfills author name on existing comments                                                                      |
| `027_create_access_tokens.sql`            | `access_tokens` table for Personal Access Tokens (PATs) with scope array and optional expiry                    |
| `028_create_deploy_keys.sql`              | `deploy_keys` table; per-repo SSH keys with `read_only` flag                                                    |
| `029_add_draft_to_pulls.sql`              | Adds `is_draft` boolean column to `pull_requests`                                                               |
| `030_create_branch_protections.sql`       | `branch_protections` table with `pattern`, `require_review_count`, `require_status_checks`, `block_force_push`  |
| `031_add_suggestion_to_line_comments.sql` | Adds `is_suggestion` and `suggestion_body` columns to `pull_line_comments`                                      |
| `032_add_auto_merge_to_pulls.sql`         | Adds `auto_merge_strategy` column to `pull_requests` for auto-merge support                                     |
| `033_create_reactions.sql`                | `reactions` table for emoji reactions on issue/PR comments                                                       |
| `034_add_2fa_to_users.sql`               | Adds `totp_secret`, `totp_enabled`, `recovery_codes` columns to `users` for TOTP 2FA                           |

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
