# Git Transport — HTTP Smart Protocol & SSH Server

## SSH Key Management

Users add SSH public keys for git operations via the API.

### API

```bash
# Add a key
curl -X POST http://localhost:8080/api/user/keys \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"title": "laptop", "public_key": "ssh-ed25519 AAAA..."}'

# List keys
GET /api/user/keys

# Delete a key
DELETE /api/user/keys/{id}
```

### Storage

- Public keys stored in `ssh_keys` table with MD5 fingerprints
- Fingerprints used for fast public key lookups during SSH handshakes
- Users can have multiple keys with different titles

---

## Git HTTP Smart Protocol

### Endpoints

- `GET /{owner}/{repo}/info/refs?service=git-upload-pack` — List refs (clone/fetch)
- `POST /{owner}/{repo}/git-upload-pack` — Upload pack (clone/fetch data)
- `POST /{owner}/{repo}/git-receive-pack` — Receive pack (push data)

### Authentication

- **Public repos**: No authentication required
- **Private repos**: Requires HTTP Basic Auth or JWT cookie
- Permissions enforced: read access for clone/fetch, write access for push

### Example

```bash
# Clone a public repo
git clone http://localhost:8080/admin/my-project.git

# Clone a private repo (with basic auth)
git clone http://user:password@localhost:8080/admin/private-repo.git

# Push requires write access
git push origin main
```

---

## SSH Server

### Configuration

In `config.yaml`:

```yaml
git:
  repos_root: ./git-repos
  ssh_port: 2222 # SSH server port
  ssh_host_key: ./cloudzilla_host_key # Host key file (auto-generated if missing)
```

### SSH Git Operations

```bash
# Add an SSH key first (see above)

# Clone via SSH
git clone ssh://git@localhost:2222/owner/repo.git

# Standard git operations
git push origin main
git fetch
git pull
```

### How SSH Auth Works

1. Client initiates SSH connection to port 2222
2. Server presents host public key
3. Client sends user's SSH public key
4. Server computes MD5 fingerprint and looks up matching SSH key in database
5. If found, extracts user ID from key owner
6. User is authenticated and context is populated
7. `git-upload-pack` or `git-receive-pack` command is dispatched with user context
8. Repository permissions are checked (read for upload-pack, write for receive-pack)

## Repository Permission Rules

All git operations (HTTP and SSH) respect the same permission rules.

**Read Access** (`git clone`, `git fetch`, `git pull`):

- Public repositories: Always allowed (no auth required)
- Private repositories: Requires authentication + one of:
  - User is the repository owner
  - User has a permission record with any role (`reader`, `writer`, or `admin`)
  - A deploy key with matching fingerprint exists for this repo

**Write Access** (`git push`):

- Requires authentication + one of:
  - User is the repository owner
  - User has a permission record with role `writer` or `admin`
  - A read-write deploy key with matching fingerprint exists for this repo

**Service API:**

- `RepoService.CanRead(ctx, repo, userID)` — checks public/private + permissions
- `RepoService.CanWrite(ctx, repo, userID)` — owner, org owner, or `writer`/`admin` role
- `RepoService.CanManage(ctx, repo, userID)` — owner, org owner, or `admin` collaborator (settings, collabs, branch protection)
- `RepoService.IsOwner(ctx, repo, userID)` — owner or org owner only (transfer, delete, archive)
- `RepoService.TransferRepo(ctx, repo, requestingUserID, newOwnerUsername)` — moves git dir on disk, updates `owner_id`/`owner_name`; personal repos only
- Bare repository created with `go-git.PlainInit()`, fully compatible with git CLI
