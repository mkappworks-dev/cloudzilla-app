# 2026-10-02 — Repository import

**Status:** Design approved.
**Branch:** `feat/repo-import`
**Affected subsystems:** new `ImportService` (`internal/service/import_service.go`, `import_transport.go`), `RepoService` (resolve + publish helpers), config (new `import` section), handlers (`repo_import_handler.go`), Templ pages under `internal/view/pages/`, router, audit actions, docs.

---

## Background

The dashboard's "Import" button and the "Import a repository →" link on `/repos/new` (currently `href="#"`) point at `/repos/import`, which has no route. Nothing in the codebase clones from a remote: go-git is used only to serve and write local bare repos.

An import makes the server fetch from a user-supplied URL. That adds two risks the rest of the app doesn't have: SSRF (the server probing hosts on its own network) and local file access (go-git resolves a bare path such as `/data/repos/alice/secret.git` to its `file` transport).

The request can't run synchronously: `server.write_timeout` defaults to 15s, so any import longer than that would have its connection cut.

## Locked decisions

1. **Git data only.** All branches and tags, with HEAD set to the source's default branch. No issues, PRs, wiki, releases, LFS objects, submodules, or ongoing mirroring. This matches GitHub's importer.
2. **HTTP(S) sources only, optional credentials.** An optional username + token are sent as HTTP basic auth for this one clone and never stored. SSH sources are out of scope.
3. **Private networks blocked by default.** Loopback, private, link-local and similar addresses are refused at connect time unless `import.allow_local_networks` is set (Gitea's model).
4. **Background job, published on success.** The clone runs in a goroutine into a temp dir; the repo row and final directory are created only when the clone succeeds. Jobs live in memory; a restart drops in-flight imports.

---

## Flow

| Route | Behaviour |
| --- | --- |
| `GET /repos/import` | Import form. `?url=`, `?owner=`, `?name=` prefill it (used by "Try again"). `owner` is honoured only when it is the viewer or an org they own, the same rule as `/repos/new`. |
| `POST /api/repos/import` | JSON `{clone_url, auth_username, auth_token, owner, name, description, private}`. Validates, starts the job, returns `202 {id, status_url}`. |
| `GET /repos/import/{id}` | Status page. With `HX-Request` it renders only the status fragment. A finished job answers the fragment request with `HX-Redirect: /{owner}/{name}`. Unknown, expired, or another user's job: 404. |
| `GET /api/repos/import/{id}` | JSON `{id, status, owner, name, progress, error}` for API clients. Same 404 rule. |

All four routes require auth (`authMW`).

### Start (synchronous, before the 202)

- **`clone_url`:** trimmed, parsed with `net/url`. Scheme must be `http` or `https`, host non-empty. URLs with userinfo (`https://user:token@…`) are rejected with "Put credentials in the username and token fields", so a token can't reach logs or error text. This also rules out local paths and `file://`.
- **Credentials:** `auth_username` and `auth_token` must be both set or both empty.
- **`name`:** required, `ValidateRepoName`.
- **`owner`:** empty or the actor's username means a personal repo (`personalOwner`); otherwise an org the actor owns. Anything else is a 403.
- **Name precheck:** an existing `owner/name` fails fast with `ErrRepoNameTaken` (422). `claimRepo` re-checks at publish.
- **Per-user cap:** at most 5 jobs queued or running per user, else 429.
- The handler records an audit entry `repo.import` (new `AuditActionRepoImport`) with metadata `{owner, source_url, job_id}`. It records the attempt, not the outcome, because a refused SSRF probe is exactly what the audit log should show.

### Job

- **ID:** 16 random bytes, hex. Only the creating user can read it.
- **Status:** `queued` → `running` → `done` | `failed`.
- **Concurrency:** a server-wide semaphore of 3; jobs past it stay `queued`.
- **Context:** `context.WithTimeout(context.Background(), cfg.Import.Timeout)`, detached from the request, carrying the dial policy (below).
- **Progress:** the last non-empty line of the source's sideband progress (split on `\r` and `\n`, max 200 chars), e.g. `Counting objects: 45% (900/2000)`.
- **Retention:** finished jobs are evicted 1h after they finish, swept on each `Start`/`Get`.
- **Startup:** `ReposRoot/.import-tmp` is removed.

### Clone

1. `tmp := ReposRoot/.import-tmp/<job id>`. Owner names must start with `[A-Za-z0-9]`, so `.import-tmp` can't collide with an owner dir, and the job dir has no `.git` suffix, so `cloudzilla gc` skips it. Keeping it under `ReposRoot` keeps the final rename on one filesystem.
2. `PlainInit(tmp, bare=true)`, then a remote `origin` with fetch refspecs `+refs/heads/*:refs/heads/*` and `+refs/tags/*:refs/tags/*`. Not `Mirror: true`, which would also pull GitHub's `refs/pull/*`.
3. `remote.ListContext` reads the advertisement. Default branch: HEAD's target if it is a branch, else `main` if present, else the first branch by name. No branches at all → "The source repository is empty".
4. `remote.FetchContext` with `Tags: NoTags` (the refspec already covers tags) and the progress writer.
5. Set the bare repo's HEAD to `refs/heads/<default>`, then delete the `origin` remote so the source URL doesn't stay in the repo's config.

### Publish

`RepoService.CreateFromImport` handles personal and org owners (it already has `personalOwner` and `isOrgOwner`):

1. Re-check the owner permission; it may have changed during a long import.
2. `claimRepo(owner, name)` creates the empty `gitDir`.
3. `CreateWithOwnerName` inserts the row (`OwnerID` or `OrgID`, `CreatedBy` = actor, description, visibility, default branch from the clone).
4. `os.Remove(gitDir)`, then `os.Rename(tmp, gitDir)`. Go's `os.Rename` refuses to replace an existing directory, so the empty claim goes first; the row already exists, so a concurrent `claimRepo` still sees the name as taken.
5. Any failure after step 2 calls `abandonNewRepo`. The job always ends with `os.RemoveAll(tmp)`, a no-op after a successful rename.

The primary language fills in lazily from the NULL column, as it does for forks. Contributor stats aren't backfilled, also like forks.

---

## Transport guard

go-git picks its transport from the process-global `client.Protocols`. `NewImportService` installs, once (`sync.Once`), an HTTP client for `http` and `https` built from `http.DefaultTransport.Clone()` with three hooks. Each acts only when the request context carries the import policy, so every other go-git HTTP use behaves as before.

- **`DialContext`:** resolves the host with `net.DefaultResolver.LookupIPAddr(ctx, host)`. Unless `allow_local_networks`, it refuses when any resolved address is loopback, private (incl. IPv6 ULA), link-local, multicast, unspecified, `0.0.0.0/8` or `100.64.0.0/10` (CGNAT, which some cloud metadata services use). It then dials the vetted IP directly, so no second lookup can rebind. TLS verification still uses the request's host name. Redirect hops go through the same dialer.
- **`Proxy`:** `nil` for import requests, `ProxyFromEnvironment` otherwise. Imports don't use `HTTP(S)_PROXY`; a proxy on a private address would be refused by the dial guard anyway.
- **Response size:** a `RoundTripper` wrapper counts response body bytes against `git.max_pack_bytes` (the knob that caps pushes) when it is non-zero, and fails the read past it.

Credentials exist only in the job goroutine's `transport.BasicAuth`. Logs carry the job ID, owner/name and the source host, never the token.

## Errors shown on the status page

| Cause | Message |
| --- | --- |
| `transport.ErrAuthenticationRequired`, `ErrAuthorizationFailed`, `ErrRepositoryNotFound` | "Repository not found, or it needs a username and token." |
| Dial guard refusal | "<host> resolves to a private network address. An administrator can allow this with `import.allow_local_networks`." |
| No branches | "The source repository is empty — create a new repository instead." |
| Size cap | "The repository is larger than this instance's limit of <size>." |
| Timeout | "The import took longer than <timeout> and was stopped." |
| Name taken at publish | "<owner>/<name> was created while the import ran." |
| Anything else | "The import failed." Details go to `slog.Error` with the job ID. |

A failed status shows "Try again", linking to `/repos/import?url=…&owner=…&name=…` without credentials.

## UI

- **`pages/repo_import.templ`:** same layout and account subnav as `RepoNew`. Source URL; a "This repository needs credentials" disclosure (Alpine) with username and token (`type="password"`), noting "Used once for this import and not stored"; owner picker (reuses `repoNewCustomSelect`), name, description, visibility. The name follows the URL's last path segment (minus `.git`) until the user edits it. Submit "Begin import" posts JSON like the create form and navigates to `status_url`. The aside lists what is and isn't imported.
- **Status page + fragment:** source URL, target `owner/name`, status, progress line. While `queued`/`running` the fragment carries `hx-get` to itself with `hx-trigger="every 2s"` and `hx-swap="outerHTML"`.
- `repo_new.templ`'s "Import a repository →" link points at `/repos/import`.

## Config

```yaml
import:
  allow_local_networks: false   # CZ_IMPORT_ALLOW_LOCAL_NETWORKS
  timeout: 30m                  # CZ_IMPORT_TIMEOUT
```

## Docs

New `docs/repo-import.md` (linked from `CLAUDE.md`'s subsystem docs); `docs/configuration.md` and `docs/api-reference.md` updated.

## Testing

- **Unit:** URL validation table (schemes, userinfo, local paths, empty host); address classifier table; dial guard refuses a loopback target with the policy and passes it without; progress-line extraction.
- **Integration (`TEST_DATABASE_DSN`):** the source is served by Cloudzilla's own smart-HTTP handler on an `httptest` server, with `allow_local_networks` on.
  - Branches, tags and a non-`main` HEAD arrive; the bare repo has no `origin` remote.
  - A private source needs basic auth: fails without credentials, succeeds with them.
  - With `allow_local_networks` off, a `127.0.0.1` source fails with the guard's message.
  - A name taken mid-import fails the job and leaves no row or directory behind.
  - Another user's `Get` returns not-found; a sixth concurrent job is refused.
- **Router page check:** `/repos/import` renders 200; another user's `/repos/import/{id}` is 404.
- **Manual:** import a public GitHub repo in the browser and watch the status page redirect.

## Out of scope

Issues/PRs/wiki/releases, LFS objects, SSH sources, pull mirrors, submodules, resuming after a restart, and per-instance toggles to disable importing.
