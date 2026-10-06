# Repository mirrors

Created: 2026-10-06
Category: enhancement
Status: ready-for-agent

## Problem

An import (`/repos/import`) copies a repository once, and the copy never tracks its source again (`docs/repo-import.md`; the import design listed pull mirrors as out of scope). Two common needs go unmet:

- **Pull mirror:** keep a read-only local copy of an upstream repository, such as `https://github.com/go-git/go-git`, current. Builds and browsing then run against this instance and survive the upstream going away.
- **Push mirror:** keep a copy of a Cloudzilla repository on another host, such as a public GitHub mirror of an internal project or an offsite backup, without a cron job running `git push --mirror`.

Apart from the one-shot import, nothing in `internal/` fetches from or pushes to a remote.

## Current state

Checked on `origin/main` at 4d1e48d7, 2026-10-06.

### Import

The code is in `internal/service/import_service.go`, `import_clone.go`, `import_source.go` and `import_guard.go`.

- An import is a background job held in memory. At most 3 run at once server-wide (`importConcurrency`, a buffered channel), and a user may have 5 queued or running. A restart drops jobs in flight.
- `cloneForImport` fetches `+refs/heads/*:refs/heads/*` and `+refs/tags/*:refs/tags/*` into a temporary bare repo. It deliberately avoids a mirror's `+refs/*:refs/*`, which would copy GitHub's `refs/pull/*`. It then sets HEAD to the source's default branch and deletes the `origin` remote.
- `ParseImportURL` accepts only `http` and `https` URLs, with no userinfo, no query string, and at most 2048 bytes.
- Credentials are a username and token. They are sent as HTTP basic auth for the one clone and never stored or logged (`importAuth`). The form says "Used once for this import and not stored."
- The SSRF guard works like this:
  - `installImportTransport` replaces go-git's process-wide `http`/`https` client once.
  - When a request's context carries an `importGuard`, the dialer resolves the host and refuses loopback, private, link-local, multicast, unspecified, `0.0.0.0/8` and `100.64.0.0/10` addresses, unless `import.allow_local_networks` is set. It then dials the vetted IP.
  - Proxies are ignored for guarded requests.
  - Response bodies are capped: the pack at `git.max_pack_bytes`, the ref advertisement at 64 MiB, and error bodies at 64 KiB.
  - The guard only applies to calls that pass the context: `ListContext`, `FetchContext` and `PushContext`.
- go-git's SSH transport bypasses that client entirely. It dials with `context.Background()`, honours `ALL_PROXY`, reads the server user's `~/.ssh/config` and `known_hosts`, and falls back to the SSH agent.
- go-git's client never asks for `thin-pack`: `transport.FilterUnsupportedCapabilities` strips it from every advertisement before `packp/ulreq.go:90` could request it. Every fetched pack is self-contained, so a fetch can keep the storer's `PackfileWriter` fast path, which a push can't (see [git-transport: Thin packs](../../docs/git-transport.md#thin-packs)).

### Background work

- There is no scheduler and no job table.
- `cmd/server/main.go` runs two tickers on `workerCtx`: webhook retries every 60 s (`WebhookService.RetryPending`) and the 24 h repo purge.
- Only the purge claims its rows (`FOR UPDATE SKIP LOCKED`). The webhook retry query has no claim, so two instances would both deliver.
- On shutdown, `main.go` cancels `workerCtx` and waits for nothing else.

### Secrets at rest

- Nothing is encrypted. Webhook secrets (`webhooks.secret`) and TOTP secrets are stored as plaintext. Tokens and OAuth client secrets are hashed.
- The only key in config is `auth.jwt_secret`, which signs JWTs.

### Read-only enforcement

`IsArchived` is checked in five places:

- HTTP push: `gitCanPush` (`internal/handler/git_http.go:114`)
- SSH push: `internal/ssh/server.go:219`
- the new-file form: `repo_files_handler.go:198`
- the profile README: `profile_readme_handler.go:43`
- creating a repo from a template: `repo_service.go:1065`

These paths write to an archived repo without checking:

- branch and tag create and delete (`code_service_refs.go`)
- PR merge (`code_service_merge.go`) and auto-merge (`tryAutoMerge` in `pull_handler.go`)
- applying a suggestion
- creating a release, which creates its tag (`release_service.go:82`)
- changing the default branch (`RepoService.UpdateGeneral`)

`feat/web-file-edit` (only a spec so far) adds a web edit path that checks archived itself.

### After a push

There is no shared pipeline; each transport runs its own list.

- **HTTP** (`git_http.go:371-431`) runs:
  1. a push webhook per updated branch (not for tags or deletes)
  2. activity events, when there is a human pusher
  3. `RepoService.OnPostReceive`: contributor stats by author email, open PRs' `head_sha`, and the primary language
  4. `IndexService.IndexRepo`
  5. `DependencyService.ParseAndStore`
- **SSH** (`ssh/server.go:245-281`) runs the same list without indexing and dependencies.
- Activity events need a real user (`events.actor_id` is `NOT NULL`). Deploy-key pushes skip them and send an empty pusher name in the webhook.
- No notifications fire on push.
- Imports run none of these steps.

### Token scopes

`/api/repos/{owner}/{repo}/<sub>` is closed to scoped tokens unless `<sub>` is listed in `internal/middleware/scope.go`. `scope_test.go:58-59` uses `/mirror` as its example of an unlisted path.

## Proposed design

This PR ships pull mirrors and the pieces push mirrors will reuse: the scheduler, encrypted credentials and the read-only guard. Push mirrors are a follow-up; see [Later: push mirrors](#later-push-mirrors). The choices behind this design are listed under [Decisions](#decisions).

### Data model

Add migration `NNN_repo_mirrors.sql`, taking the next free number at commit time (105 once main's 102–104 landed).

- **`repo_mirrors`** holds the pull mirror; a repo has at most one. Its columns:
  - `repo_id`: the primary key, referencing `repositories` with `ON DELETE CASCADE`
  - `remote_url`, `auth_username`, `auth_token_enc BYTEA NULL`
  - `interval_seconds`, `next_sync_at`, `lease_until NULL`
  - `last_sync_at`, `last_success_at`, `last_error`, `consecutive_failures`
  - `created_by`, `created_at`, `updated_at`
- An index on `next_sync_at` serves the scheduler.
- `Repository.IsMirror` is derived (`EXISTS` on `repo_mirrors`) in the repo store's SELECTs, rather than kept as a second column that could drift.

### Scheduler

`MirrorService.Run(ctx)` is a single loop that `main.go` starts on `workerCtx`, like the webhook retries. Unlike them, it is safe to run on more than one instance.

- **Claiming:** every 30 s, or when "Sync now" wakes it, the loop claims due mirror rows:

  ```sql
  UPDATE … SET lease_until = NOW() + <timeout>
  WHERE id IN (
    SELECT …
    WHERE next_sync_at <= NOW()
      AND (lease_until IS NULL OR lease_until < NOW())
    FOR UPDATE SKIP LOCKED LIMIT n
  )
  RETURNING …
  ```

  The lease keeps other instances off a row, and it expires if the instance holding it crashes.
- **Running:** syncs run under a semaphore of `mirror.max_concurrent`. Each runs under `mirror.timeout`, in a context that shutdown doesn't cancel, as imports do.
- **Success:** set `next_sync_at = NOW() + interval`, clear `last_error`, and reset `consecutive_failures`.
- **Failure:** store a user-facing message, mapped the way `ImportService.failureMessage` maps import errors. Then back off to `next_sync_at = NOW() + max(interval, min(interval × 2^(failures−1), 24h))`, so a revoked token doesn't hammer the upstream.
- **Skipped:** archived and soft-deleted repos. With `mirror.enabled: false` the loop doesn't run at all.
- **Sync now** sets `next_sync_at = NOW()` and wakes the loop. Its `consecutive_failures` stays, so a manual retry of a broken mirror backs off again if it fails. Manual and scheduled syncs share one path and can't overlap.

### Pull mirrors

- **Creating:** the import form gains a "Keep this repository in sync" checkbox and an interval. The clone runs as it does today. On success, the publish step also writes the mirror row, with the token encrypted. An existing repository can't be turned into a mirror.
- **Syncing:** the sync fetches into the live repo:
  - It uses the import's refspecs (`+refs/heads/*:refs/heads/*`, `+refs/tags/*:refs/tags/*`, both forced) with `Prune: true`.
  - It calls `FetchContext` with a guard in the context.
  - It keeps the storer's packfile fast path: go-git never requests thin packs, and a test fails if that changes.
  - It snapshots the refs before and after, and diffs them into `[]*packp.Command` for the post-push steps.
  - When upstream's HEAD target changes, the repo's HEAD and `default_branch` follow it.
- **Read-only:**
  - One predicate, `service.CheckContentWritable(repo) error` (backed by `Repository.ContentReadOnly()`), returns `ErrRepoArchived` or `ErrRepoMirror`. Push and every unchecked write path listed under [Read-only enforcement](#read-only-enforcement) call it.
  - An HTTP push gets `403 Repository is a mirror and is read-only.`; an SSH push gets the same text on stderr. Web and API writes get a 403 or 422 JSON error.
  - The UI hides the Add file menu, branch and tag create/delete, the merge box, apply suggestion, new release and the default-branch field.
  - The repo header shows a banner: "Mirror of `<url>` · synced 5 minutes ago · Sync now".
- **Pull requests:** creating a PR whose base repo is a pull mirror is refused, because it could never merge. Issues, discussions, the wiki and forks stay available. The wiki isn't mirrored.
- **Stop mirroring:** a settings action deletes the mirror row, which deletes its credentials. The repo becomes a regular repository with its branches as they were.
- **Side effects of a sync:**
  - Push webhooks fire for updated branches, with an empty pusher name, as deploy-key pushes do.
  - `OnPostReceive`, `IndexRepo` and `ParseAndStore` run.
  - No activity events (there's no actor) and no notifications.
  - The mirror's creation also runs `IndexRepo` and `ParseAndStore` once, which plain imports skip.

### Shared

- **Transport:** only HTTPS and HTTP remotes, under the `ParseImportURL` rules. SSH remotes would need their own dialer, host-key pinning and key storage.
- **Credentials:**
  - Tokens are encrypted with AES-256-GCM under a key from new config `security.secret_key` (`CZ_SECURITY_SECRET_KEY`, at least 32 bytes). A separate key is derived from it with HKDF for each purpose, and each ciphertext starts with a version byte so the key can be rotated later.
  - Without a key, mirrors of public URLs still work. Adding credentials is refused with a message that names the setting.
  - Tokens are write-only in the UI and API: shown only as "set", and they can be replaced or cleared but never read back.
  - Tokens are never logged.
- **Permissions:** creating, editing and deleting mirrors needs `CanManage`; "Sync now" needs `CanWrite`. The API routes are added to `repoAdminResources`, so only `repo:admin` tokens reach them.
- **Audit:** `repo.mirror.create`, `repo.mirror.update` and `repo.mirror.delete`. Background syncs aren't audited, because `AuditService.Record` needs a request.
- **Config:**

  ```yaml
  mirror:
    enabled: true               # CZ_MIRROR_ENABLED
    allow_local_networks: false # CZ_MIRROR_ALLOW_LOCAL_NETWORKS
    min_interval: 10m           # CZ_MIRROR_MIN_INTERVAL
    default_interval: 8h        # CZ_MIRROR_DEFAULT_INTERVAL
    max_concurrent: 3           # CZ_MIRROR_MAX_CONCURRENT
    timeout: 30m                # CZ_MIRROR_TIMEOUT
  security:
    secret_key: ""              # CZ_SECURITY_SECRET_KEY
  ```

  Intervals must be at least `min_interval` and at most 30 days. Turning `enabled` off stops syncing and hides the mirror options, but keeps existing mirrors read-only.

### API

| Method | Path | Purpose |
| --- | --- | --- |
| `POST` | `/api/imports` | Gains `mirror` (bool) and `mirror_interval` (a duration string) |
| `GET` | `/api/repos/{owner}/{repo}/mirror` | Pull-mirror status and settings (never the token) |
| `PATCH` | `/api/repos/{owner}/{repo}/mirror` | Change the URL, credentials or interval |
| `DELETE` | `/api/repos/{owner}/{repo}/mirror` | Stop mirroring |
| `POST` | `/api/repos/{owner}/{repo}/mirror/sync` | Sync now (202) |

## Acceptance criteria

- [ ] An import with "Keep this repository in sync" checked creates a pull mirror. Changes upstream reach it on the next sync: new commits, force pushes, new and deleted branches and tags, and a changed default branch. `refs/pull/*` is never copied.
- [ ] An incremental sync succeeds, and a test fails if go-git starts requesting thin packs.
- [ ] Creating a pull request whose base repo is a pull mirror is refused.
- [ ] A pull mirror refuses HTTP and SSH pushes, and refuses web and API writes on every path in Read-only enforcement. Archived repos are refused on the same paths.
- [ ] The UI hides write controls on a pull mirror and shows the source, the last sync time, the last error and "Sync now".
- [ ] A sync fires push webhooks for updated branches and updates open PRs' head SHAs, contributor stats, the primary language, code search and dependencies. It records no activity events.
- [ ] "Stop mirroring" turns a pull mirror into a regular, writable repository and deletes its stored credentials.
- [ ] Mirror URLs follow the import's rules. Private-network targets are refused unless `mirror.allow_local_networks` is set.
- [ ] Tokens are stored encrypted, never returned by the UI or API, and never logged. Without `security.secret_key`, credentialed mirrors are refused with a clear message.
- [ ] Two server instances never run the same mirror's sync at once. Syncs never exceed `mirror.max_concurrent`, and a failing mirror backs off up to 24 h.
- [ ] `docs/repo-mirrors.md` is linked from `CLAUDE.md`, and `docs/configuration.md` and `docs/api-reference.md` are updated.

## Tickets

| # | Ticket | Blocked by |
| --- | --- | --- |
| 01 | [Content-writable guard; archived gaps](issues/01-content-writable-guard.md) | — |
| 02 | [`security.secret_key` and `secretbox`](issues/02-secret-key-encryption.md) | — |
| 03 | [Schema, model, store, config](issues/03-mirror-schema-store.md) | — |
| 04 | [Sync](issues/04-mirror-sync.md) | 02, 03 |
| 05 | [Scheduler](issues/05-mirror-scheduler.md) | 04 |
| 06 | [Import creates a mirror](issues/06-import-creates-mirror.md) | 02, 03 |
| 07 | [Read-only mirrors](issues/07-mirror-read-only.md) | 01, 03 |
| 08 | [Settings card and API](issues/08-mirror-settings-api.md) | 05, 06, 07 |
| 09 | [Docs](issues/09-mirror-docs.md) | 02–08 |

## Relevant files

- `internal/service/import_service.go`, `import_clone.go`, `import_source.go`, `import_guard.go`, `repo_import.go`: the clone, URL rules, guard and publish to reuse
- `internal/gittransport/storer.go` (`WrapForReceive`) and `internal/gitref/gitref.go` (`Move`)
- `internal/handler/git_http.go` and `internal/ssh/server.go`: the push checks and post-push steps
- `internal/service/repo_service.go`: `OnPostReceive`, `UpdateGeneral` and `PushSummaries`
- `internal/service/code_service_refs.go`, `code_service_merge.go`, `release_service.go`, `internal/handler/pull_handler.go` (`tryAutoMerge`): write paths that need the guard
- `internal/service/webhook_service.go`: `PushPayload` and `Dispatch`
- `cmd/server/main.go`: where the scheduler starts
- `internal/config/config.go` and `docs/configuration.md`
- `internal/middleware/scope.go` and `scope_test.go`
- `internal/model/audit_log.go`
- `internal/store/repo_store.go`: every SELECT that scans a `Repository`
- `internal/view/pages/repo_import.templ`, `repo_settings.templ`, `repo.templ`, `refs.templ`, and `fragments/pull_detail.templ`
- `internal/ssh/server_test.go`: helpers for tests that drive real pushes

## Decisions

Agreed with the maintainer on 2026-10-06.

1. **Pull mirrors first.** Push mirrors follow in a later PR.
2. **Archived gaps are closed here.** `CheckContentWritable` covers archived repos and pull mirrors on every write path. It lands first, as its own `fix(repo):` commit.
3. **Credentials use a new `security.secret_key`.** Deriving the key from `auth.jwt_secret` would make rotating the JWT secret destroy every stored token. Plaintext would expose upstream PATs in any database dump.
4. **Syncs fire machine side effects only:** push webhooks with an empty pusher name, `OnPostReceive`, `IndexRepo` and `ParseAndStore`. They record no activity events and send no notifications.
5. **PRs into a pull mirror are refused at creation,** not only at merge.
6. **HTTPS only.** The SSRF guard covers go-git's HTTP client only.
7. **Limits:** a 10m minimum interval, an 8h default and 3 concurrent syncs per instance, all configurable.
8. **UI** ([mockups](https://claude.ai/artifact/1qdQ3E9jfTCzmijzWhWRgq)): the import form gets a checkbox card under the URL. The repo page gets a strip above the header (option A), which turns red on failure. Settings gets its own Mirror section. The interval is picked from fixed choices; the API accepts any duration within the limits.

## Later: push mirrors

Kept here so the follow-up can reuse the scheduler and credentials without re-deriving them.

- A `push_mirrors` table, with any number per repo: `id`, `repo_id`, the same remote, credential, schedule and status columns, and `sync_on_push BOOL`.
- The sync runs `PushContext` with a guard, pushing `+refs/heads/*:refs/heads/*` and `+refs/tags/*:refs/tags/*` with `Prune: true`. That is `git push --mirror`, limited to branches and tags.
- With `sync_on_push`, these set `next_sync_at = NOW()`: an HTTP or SSH receive-pack, a pull-mirror sync, and a PR merge or auto-merge.
- go-git builds the whole outgoing pack in memory with no cap.
- Still open: whether to always overwrite the remote, or offer "keep divergent refs".

## Out of scope

Push mirrors (see above), LFS objects, wiki mirroring, SSH remotes, mirroring issues, PRs and releases, turning an existing repository into a pull mirror, notifying anyone when a mirror fails, and an admin page listing every mirror.
