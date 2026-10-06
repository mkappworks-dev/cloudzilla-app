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

### One fingerprint, one identity

A fingerprint belongs to either a user SSH key or deploy keys, never both. `SSHKeyService.AddKey` refuses a key already in `deploy_keys` ("this key is already registered as a deploy key"), and `DeployKeyService.Add` refuses one already in `ssh_keys` ("this key is already registered as a user SSH key"). Both treat only `sql.ErrNoRows` as absent; any other lookup error fails the add.

This matters because the SSH server's `publicKeyHandler` tries user keys first, then deploy keys: a key in both tables would authenticate as the user, shadowing the deploy key.

The check is in the services, not the schema, so it has two gaps:

- Rows added before both checks existed (`AddKey` had none) may already collide.
- Two concurrent adds of the same key, one to each table, can both pass. Closing that needs a trigger or a shared fingerprint table.

To find existing collisions:

```sql
SELECT s.fingerprint, s.id AS ssh_key_id, s.user_id, d.id AS deploy_key_id, d.repo_id
FROM ssh_keys s
JOIN deploy_keys d USING (fingerprint);
```

Resolve each by deleting whichever key the owner no longer wants; nothing removes them automatically.

---

## Git HTTP Smart Protocol

### Endpoints

- `GET /{owner}/{repo}/info/refs?service=git-upload-pack` — List refs (clone/fetch)
- `POST /{owner}/{repo}/git-upload-pack` — Upload pack (clone/fetch data)
- `POST /{owner}/{repo}/git-receive-pack` — Receive pack (push data)

### Authentication

- **Public repos**: No authentication required
- **Private repos**: Requires HTTP Basic Auth (PAT as password), a Bearer token, or JWT cookie
- **Tokens** (PATs, and OAuth-app tokens via `Authorization: Bearer`): need any repo scope to clone/fetch and `repo:write` to push. A PAT sent as the Basic password and lacking the scope gets a plain-text `403`, not `401`, so git keeps the stored credential; see [access-control](./access-control.md#token-scopes)
- Permissions enforced: read access for clone/fetch, write access for push

### Responses

A caller who can't read a repo gets exactly what a missing repo gets, as on GitHub, so a private repo's existence is never confirmed (see [Private Repos Look Missing](./access-control.md#private-repos-look-missing)). The handlers check in this order, and every check before the repo lookup answers the same whether the repo exists or not:

| Check                                                                      | Response                                                                         |
| -------------------------------------------------------------------------- | -------------------------------------------------------------------------------- |
| `info/refs` without a `service` of `git-upload-pack` or `git-receive-pack` | `400 invalid service`                                                            |
| Owner or repo name invalid                                                 | `400 invalid repository path`                                                    |
| Basic PAT lacks the scope or is bound to a signing key                     | `403` naming the reason                                                          |
| Repo missing, or the caller can't read it, anonymous                       | `401` with `WWW-Authenticate: Basic realm="git"`, so git prompts for credentials |
| Repo missing, or the caller can't read it, signed in                       | `404 repository not found`                                                       |
| Push, anonymous                                                            | `401` challenge                                                                  |
| Push by a reader without write access                                      | `403 access denied`, not `401`, so git keeps the credential                      |
| Push to an archived repo                                                   | `403`                                                                            |

An invalid or expired PAT counts as anonymous. `TestGitHTTP_PrivateRepo_LooksLikeMissingRepo` compares the status, challenge and body for a private and a missing repo on all three endpoints.

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

Branch protection is enforced at the same point, before the write. Both transports pass `NewServer` a vet function that calls `BranchProtectionService.CheckPushCommand`. go-git writes refs only after it has stored the pack, so the check can read the pushed commits to tell a force push from a fast-forward. It reads them only when a `block_force_push` rule matches the branch, and then only the history since the old and new values diverged (`isAncestor`, git's merge-base walk). It fails closed: under `block_force_push`, a push that it can't prove keeps every commit on the branch is refused as a force push. That includes deleting the branch, since delete-then-push is a force push in two steps; the web and API branch delete refuses it too (`BranchProtectionService.CheckDelete`, 422). A refused ref never moves, and the status carries the reason:

```
 ! [remote rejected] main -> main (force push blocked by branch protection)
```

The status is sent to the pusher, so an error from the server itself goes only to the server log: a failed rule lookup refuses the ref with `internal error checking branch protection`, and a failed ref write (a storer error, which names paths on the server) with `failed to update ref`. A pack that can't be stored fails the whole push (HTTP 500, SSH error) with `failed to store pushed objects`, unless the fault is in the pack itself: a malformed or truncated pack, or a thin pack whose base the repo lacks, keeps go-git's reason. A corrupt zlib stream gets the generic reason, because go-git words it like a failed read of the repo's own packs.

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

## Delete-only pushes

git sends no pack when every command is a delete (`git push origin :branch`). go-git's receive-pack parses a pack whenever the request carries one, and decoding a request always attaches the rest of the stream, so `NewServer` drops it for a delete-only push. Otherwise the HTTP body fails as `empty packfile` (HTTP 500), and an SSH push hangs, since the client holds the stream open until it reads the status. Branch protection still vets each delete.

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
6. User is authenticated and context is populated; with no user key, a matching deploy key is bound instead, to its one repo (see [Repository Permission Rules](#repository-permission-rules))
7. `git-upload-pack` or `git-receive-pack` command is dispatched with user context
8. Repository permissions are checked (read for upload-pack, write for receive-pack)

### Errors

Errors go to stderr, with exit status 1: an unsupported command, a bad path, a missing repository or no access to it, a deploy key used outside its repository or to push read-only, a push to an archived repository, or a failure during the transfer. git prints stderr as-is; stdout carries only the pack protocol, where git would read a message's first four bytes as a pkt-line length.

A user who can't read a repo, and a deploy key for another repo that isn't public, get `repository not found`, the same as for a missing repo. `access denied` goes only to a reader who may not push, and `deploy key not authorized for this repository` only for a public repo.

When git needs nothing — `ls-remote`, a fetch or push that's already up to date, a clone of an empty repository — it sends a lone flush-pkt instead of a request. go-git rejects that as malformed, so the server checks for it first and exits with status 0.

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

**Deploy keys belong to one repo.** The SSH handshake authenticates a deploy key before the command names a repository, so the key is bound to its repo then, and the command is checked against that binding. `DeployKeyService.Add` therefore refuses a key that is already a deploy key on any repo, this one included ("this key is already registered as a deploy key"), as GitHub does; a deploy key needing several repos should be a separate key per repo, or a user key.

The rule is in the service, not the schema (migration 028 only makes `(repo_id, fingerprint)` unique), so rows added before it may share a fingerprint, and two concurrent adds of the same key can both pass. `DeployKeyStore.GetByFingerprint` resolves a shared fingerprint to the oldest row (`ORDER BY id`), so the key keeps working on the repo it was first added to, with that row's `read_only`, and is refused elsewhere, as if the later adds had been refused. To find shared fingerprints:

```sql
SELECT fingerprint, array_agg(repo_id ORDER BY id) AS repo_ids, array_agg(id ORDER BY id) AS deploy_key_ids
FROM deploy_keys
GROUP BY fingerprint
HAVING count(*) > 1;
```

The first repo listed is the one the key authenticates to. Resolve each by deleting the later rows and giving those repos their own keys; nothing removes them automatically.

**Service API:**

- `RepoService.CanRead(ctx, repo, userID)` — checks public/private + permissions
- `RepoService.CanWrite(ctx, repo, userID)` — owner, org owner, or `writer`/`admin` role
- `RepoService.CanManage(ctx, repo, userID)` — owner, org owner, or `admin` collaborator (settings, collabs, branch protection)
- `RepoService.IsOwner(ctx, repo, userID)` — a personal repo's owner or an owner of an org repo's org, never an org repo's creator as such (transfer, delete, archive)
- `RepoService.TransferRepo(ctx, repo, requestingUserID, newOwnerName)` — moves the git and wiki dirs on disk, updates `owner_id`/`org_id`/`owner_name`; the new owner is an org the requester owns or the requester. For any other user it returns a pending `*model.RepoTransfer` and moves nothing until `AcceptTransfer`
- Bare repository created with `go-git.PlainInit()`, fully compatible with git CLI
