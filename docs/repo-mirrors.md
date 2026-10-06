# Repository mirrors

A **pull mirror** is a read-only repository that keeps its branches and tags equal to an upstream's: a copy of `https://github.com/go-git/go-git` that builds and browsing can use, and that survives the upstream going away. Push mirrors are not built yet; see the spec in `.scratch/repo-mirrors/spec.md`.

## Creating one

A mirror starts life as an [import](./repo-import.md) with **Keep this repository in sync** ticked, or `"mirror": true` on `POST /api/imports`.

- The interval is picked from presets (10 minutes, 1 hour, 8 hours, 1 day, 1 week) that fit `mirror.min_interval`, plus `mirror.default_interval`. The API takes any Go duration from `mirror.min_interval` to 30 days.
- The username and token are sealed with `security.secret_key` (see [Credentials](#credentials)). A token with no key configured is refused before the import starts.
- `RepoService.createFromImport` writes the `repo_mirrors` row right after the repo row. If that fails, the import is abandoned, so there is never a mirror without its row.
- The new mirror is indexed for code search and its dependencies parsed once. Plain imports skip this.
- The import is audited as `repo.mirror.create`.

An existing repository can't become a mirror.

## Syncing

`MirrorService.Sync` (`internal/service/mirror_service.go`) fetches into the live bare repo:

- **Refs:** `+refs/heads/*:refs/heads/*` and `+refs/tags/*:refs/tags/*`, forced, with `Prune`. The mirror follows force pushes and deleted branches and tags. GitHub's `refs/pull/*` is never fetched.
- **Guard:** the fetch goes through the import's SSRF guard (`importGuard`): private-network addresses are refused unless `mirror.allow_local_networks` is set, and the pack and ref advertisement are capped as for imports.
- **Packs:** go-git's client strips `thin-pack` from what it asks for, so fetched packs are self-contained and keep the storer's packfile fast path. See [git-transport: Thin packs](./git-transport.md#thin-packs).
- **Default branch:** when the upstream's HEAD points elsewhere, the mirror's HEAD and `default_branch` follow. An upstream that doesn't say is handled as an import handles it.
- **Side effects:** the refs that moved are diffed into push commands, and the sync runs what a push would: push webhooks for updated branches (with an empty pusher name), `OnPostReceive` (contributor stats, open PRs' head SHAs), code search re-indexing and dependency parsing. It records no activity events and sends no notifications.
- **Failures:** returned as `MirrorSyncError`, with a message for the repo's admins that never carries the token: a private address, a size cap, the timeout, rejected credentials, an empty upstream, or a generic "check that the source is reachable". The full error is logged.

## Scheduling

`MirrorService.Run`, started from `cmd/server/main.go` when `mirror.enabled` is set:

- **Claiming:** every 30 s, or when woken, it claims due mirrors with `MirrorStore.ClaimDue`, an `UPDATE … WHERE repo_id IN (SELECT … FOR UPDATE SKIP LOCKED)`. Each claim sets a lease of `mirror.timeout` plus a minute. Two instances never claim the same mirror, and a crashed instance's lease expires.
- **Concurrency:** at most `mirror.max_concurrent` syncs run at once on each instance. When a sync finishes it wakes the loop, so a mirror waiting for a slot starts at once.
- **Shutdown:** each sync runs under `mirror.timeout`, in a context that shutdown doesn't cancel, so a fetch is never cut off half-way.
- **Success:** sets `next_sync_at` to now plus the interval.
- **Failure:** stores the message and backs off. The first failure waits one interval, and each further one doubles it, up to 24 h but never below the interval.
- **Skipped:** archived and soft-deleted repos.
- **Sync now:** (`POST /api/repos/{owner}/{repo}/mirror/sync`, `CanWrite`) makes the mirror due and wakes the loop. A sync already holding the lease finishes first.

All schedule times come from the database clock.

## Read-only

`Repository.IsMirror` is derived with `EXISTS` on `repo_mirrors` in every repo query, and `Repository.ContentReadOnly()` is true for mirrors and archived repos alike. `service.CheckContentWritable` refuses with `ErrRepoMirror` on every git-content write:

- HTTP and SSH pushes, which get `Repository is a mirror and is read-only.`
- branch and tag create and delete
- merge and auto-merge
- applying suggestions
- web file edits
- release-created tags
- changing the default branch

The UI hides the matching controls. Creating a pull request into a mirror is refused with `Pull mirrors are read-only; open the pull request upstream.`

Issues, discussions, the wiki and forks keep working. A fork of a mirror is an ordinary repository.

The code page opens with a strip naming the source, the last sync and **Sync now**. While syncs fail it turns red, with the error, the next try and a link to the mirror settings.

## Managing

The settings page's **Mirror** section (`CanManage`) shows the sync status and edits the source URL, username, token and interval.

- **Token:** write-only. An empty field keeps the stored token, and **Remove the stored token** clears it.
- **Rescheduling:** a new source or new credentials make the mirror due at once. A new interval counts from the last sync.
- **Stop mirroring:** deletes the row and its token. The repository keeps its branches and becomes writable. It can't be turned back into a mirror.
- **Audit:** changes are audited as `repo.mirror.update` (the changed field names and the URL, never the token) and `repo.mirror.delete`. Background syncs aren't audited.

## Credentials

Tokens are sealed by `internal/secretbox`:

- AES-256-GCM, under a key derived with HKDF-SHA256 from `security.secret_key`, with the info string `cloudzilla/mirror-credential`.
- A version byte prefixes each value, and the purpose is bound as additional data.
- A stored token is never returned by the UI or API, and never logged.

Losing or changing the key makes stored tokens unreadable. Syncs then fail with "re-enter the token in the mirror settings".

## Configuration

| Key | Default | Meaning |
| --- | --- | --- |
| `mirror.enabled` | `true` | Run the sync loop and offer mirroring. When off, existing mirrors stay read-only |
| `mirror.allow_local_networks` | `false` | Let mirrors reach private-network sources |
| `mirror.min_interval` | `10m` | Shortest sync interval |
| `mirror.default_interval` | `8h` | Interval for new mirrors |
| `mirror.max_concurrent` | `3` | Syncs at once on each instance |
| `mirror.timeout` | `30m` | Time limit for one sync |
| `security.secret_key` | `""` | Seals stored tokens; at least 32 bytes |

See [configuration](./configuration.md) for the env vars.
