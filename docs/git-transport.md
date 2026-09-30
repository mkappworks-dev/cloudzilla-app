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
- **Private repos**: Requires HTTP Basic Auth (PAT as password) or JWT cookie
- **OAuth-app tokens** (`Authorization: Bearer`): need `repo:read` (or any repo scope) to clone/fetch and `repo:write` to push; see [access-control](./access-control.md#oauth-app-scopes)
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

## Thin packs

Native `git push` produces *thin packs* by default — packs whose objects may be encoded as deltas against base objects already on the server. The server is expected to "fix" the thin pack by appending the missing bases.

Cloudzilla's transport is pure-Go and uses `go-git`'s `server.ReceivePack`. go-git's filesystem-backed storer takes a fast path inside `packfile.UpdateObjectStorage` that runs the pack parser **without** access to the storage, so REF_DELTAs whose base is only on disk (not in the pack) cannot be resolved. The receive fails with `reference delta not found` and a 500 is returned to the client.

To work around this without giving up the "no git binary required" invariant, both transports serve receive-pack through `gittransport.NewServer`, which routes the storer through `gittransport.WrapForReceive`. The wrapper hides the storer's `PackfileWriter` method via interface-embedding, which forces `UpdateObjectStorage` onto its slower `NewParserWithStorage` branch. That parser *can* see the storage, so external delta bases are resolved correctly.

**Trade-off:** received objects land loose under `objects/xx/yyy…` rather than packed. Native git treats this as routine; reclaim unreferenced loose objects with `cloudzilla gc` (see [Maintenance](#maintenance)).

**Observability:** each receive-pack that completes emits an `INFO` log line — `git-http: receive-pack complete` over HTTP, `ssh: receive-pack complete` over SSH — with `pack_bytes`, `duration_ms`, and `refs_ok`/`refs_failed` counts. "Complete" means the pack was ingested without a transport error; `refs_failed > 0` flags a push where some ref updates were rejected.

**Size limit:** the post-decompression pack size is capped by `git.max_pack_bytes` (default 2 GiB; `0` disables). Enforcing it after gzip inflation bounds both an oversized pack and a decompression bomb. An over-limit push is rejected — HTTP `413`, SSH error — rather than parsed in full.

**See also:** [`docs/superpowers/specs/2026-05-15-git-receive-thin-pack-fix-design.md`](./superpowers/specs/2026-05-15-git-receive-thin-pack-fix-design.md).

---

## Concurrent ref updates

go-git's receive-pack writes each pushed ref without comparing it to the command's old value. A branch that moved after the client read the ref advertisement — a web commit (merge, applied suggestion, file or wiki edit) or another push — would be overwritten: an unchecked force push that also skips `block_force_push`.

`gittransport.NewServer` turns each ref write into a compare-and-swap against the old value the client pushed from (`gitref.Move`, which web commits use too; see [pr-merge](./pr-merge.md)). A ref that moved is refused in the report status, and the rest of the push still applies:

```
 ! [remote rejected] main -> main (ref changed since it was read; fetch and push again)
```

`NewServer` also refuses what git would before the vet function runs: a new value whose history the repo doesn't wholly hold (see [Connectivity](#connectivity)), and a branch that doesn't point at a commit.

Branch protection is enforced at the same point, before the write. Both transports pass `NewServer` a vet function that calls `BranchProtectionService.CheckPushCommand`. go-git writes refs only after it has stored the pack, so the check can read the pushed commits to tell a force push from a fast-forward. It fails closed: under `block_force_push`, a push that it can't prove keeps every commit on the branch is refused as a force push. That includes deleting the branch, since delete-then-push is a force push in two steps; the web and API branch delete refuses it too (`BranchProtectionService.CheckDelete`, 422). A refused ref never moves, and the status carries the reason:

```
 ! [remote rejected] main -> main (force push blocked by branch protection)
```

The status is sent to the pusher, so an error from the server itself goes only to the server log: a failed rule lookup refuses the ref with `internal error checking branch protection`, and a failed ref write (a storer error, which names paths on the server) with `failed to update ref`.

The response is still HTTP 200 / SSH exit 0; the per-ref status is what tells the client. Webhooks, activity events, and post-receive run only for the refs that applied (`gittransport.AppliedCommands`).

**report-status:** go-git returns no status to a client that didn't request `report-status`, and it turns a refused ref into an error for the whole push. The session always requests it internally, so the handlers still know which refs applied, and it sends the status only to clients that asked for it. A client without it gets no per-ref result.

**Gap:** go-git can't create or delete a ref conditionally, so creates and deletes check the ref just before writing, not atomically with the write.

---

## Connectivity

go-git's receive-pack stores whatever pack it gets and writes the ref. A pack could leave out the pushed commit or any of its parents, trees, or blobs, and clone, fetch, the code browser, post-receive stats, and indexing would then fail with "object not found". `NewServer` runs git's `check_connected` itself: every object a create or update's new value reaches must be in the repo, or the ref is refused:

```
 ! [remote rejected] feature -> feature (missing necessary objects)
```

The walk stops at history that the repo's refs already reach, so it costs what the push added, not the repo's size:

- Commits are walked newest first from the new value and from every ref (`git rev-list <new> --not --all`), until nothing new is left.
- Each new commit's tree is compared with its parents' trees, so only the paths it changed are read.
- Submodule entries are skipped, since their commits live in another repo.
- When the push builds on the value its ref holds, and its pack carries every commit in between (a plain fast-forward), the walk ends there without reading the other refs.

The walk doesn't stop at an object only because it's present. go-git stores a pack before any ref is checked, so a refused push leaves its objects behind, and a later push could otherwise build on them. For the same reason, only the value a ref holds counts as a fast-forward's base, not whatever old value the client names.

A branch must point at a commit, as in git:

```
 ! [remote rejected] feature -> feature (trying to write non-commit object to branch)
```

Tags and other refs may point at any object.

---

## Maintenance

### Loose-object GC

receive-pack writes objects loose (see [Thin packs](#thin-packs)); rejected or churny pushes leave unreferenced objects that nothing reclaims automatically. The `cloudzilla gc` command prunes them:

```bash
cloudzilla gc                    # prune every repository under git.repos_root
cloudzilla gc --repo owner/name  # prune a single repository
cloudzilla gc --dry-run          # report what would be pruned, delete nothing
cloudzilla gc --grace 336h       # change the age threshold (default 14 days)
```

A loose object is removed only when it is unreachable from every ref **and** older than `--grace`. The grace period avoids racing a push that has written objects but not yet updated its ref. Packed objects are never touched. Safe to run on a schedule (e.g. cron).

---

## SSH Server

### Configuration

In `config.yaml`:

```yaml
git:
  repos_root: ./git-repos
  ssh_port: 2222 # SSH server port
  ssh_host_key: ./cloudzilla_host_key # Host key file (auto-generated if missing)
  ssh_max_session: 2h # Absolute connection lifetime (0 disables)
```

A connection idle for 60s is closed; `ssh_max_session` is the absolute cap that also bounds a slow client trickling bytes to defeat the idle timeout.

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
- `RepoService.IsOwner(ctx, repo, userID)` — a personal repo's owner or an owner of an org repo's org, never an org repo's creator as such (transfer, delete, archive)
- `RepoService.TransferRepo(ctx, repo, requestingUserID, newOwnerName)` — moves the git and wiki dirs on disk, updates `owner_id`/`org_id`/`owner_name`; the new owner is a user, or an org the requester owns
- Bare repository created with `go-git.PlainInit()`, fully compatible with git CLI
