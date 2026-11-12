# Repository rename

Created: 2026-11-12
Category: enhancement
Status: ready-for-agent

Spec for ticket 15 of [`../2026-11-06-uiux-desktop-9.5/`](../2026-11-06-uiux-desktop-9.5/issues/15-repo-rename.md). Organization
name change is ticket 34's; this spec only leaves the alias table reusable for it.

## Problem

Repo settings render the name read-only ("Renaming from settings is not yet available", `repo_settings.templ`). Nothing
remembers a previous name, so a path change would 404 every link, README badge and `git remote`. Ownership transfer
(`RepoService.TransferRepo`) already changes the path and already leaves nothing behind.

## How the path is used today

A repo's identity is `repositories.id`; its path is the pair `owner_name` + `name` on that row, and every `/{owner}/{repo}`
request is resolved by `RepoStore.GetByOwnerName` (`RepoService.Get`).

- **Database:** `repositories.owner_name` and `name`, unique per owner (`UNIQUE(owner_id, name)`, and
  `idx_repos_org_name_live` for org repos, partial on `deleted_at IS NULL`). A soft-deleted personal repo still holds
  its name; a soft-deleted org repo does not. `RepoStore.NameHeld` checks case-insensitively, soft-deleted rows included.
- **Disk:** the bare repo is `<git.repos_root>/<owner>/<name>.git` and the wiki `<name>.wiki.git`, from
  `repoDirs` (`internal/service/repo_dirs.go`). Local disk only; the S3 backend holds avatars and attachments, which are
  keyed by id or token (attachment URLs carry no repo name, per `docs/storage.md`).
- **`.wiki` collision:** a repo `<x>.wiki` (from before the reservation) owns `<x>`'s wiki path, so `moveRepo` checks
  `NameHeld(owner, wikiPartner(name))` and `ownWikiDir`.
- **Transfer** is the nearest precedent: `moveRepo` renames both dirs, runs the DB update, and `revertDirs` undoes the
  moves if the update fails. A crash between the two steps is an accepted window. `AcceptTransfer` already refuses with
  `ErrTransferChanged` when the offered `owner/name` no longer matches.
- **Routes:** about 190 `/{owner}/{repo}/...` routes in `internal/router/router.go`, plus `/api/repos/{owner}/{repo}`.
  `PATCH /api/repos/{owner}/{repo}` (`UpdateRepo`) edits the repo today.
- **Git:** HTTP `GitInfoRefs`/`GitUploadPack`/`GitReceivePack` (`internal/handler/git_http.go`) and the SSH server
  (`internal/ssh/server.go`) both call `RepoService.Get(owner, name)` and answer a miss, or a repo the caller can't read,
  identically ("Private Repos Look Missing", `docs/git-transport.md`).

### What keys on the id (survives a rename untouched)

Webhooks and deliveries, pull mirrors (`repo_mirrors.repo_id`; the upstream URL is not ours), forks (`fork_of_id`; the
"forked from" label is derived at read time by `enrichForkInfo`), issues, PRs, stars, collaborators, deploy keys,
branch protection, code-search and dependency indexes, attachments, avatars (user/org keyed), quotas.

### What stores the name as text (stale after a rename)

- `events.owner_name`/`repo_name` and `notifications.owner_name`/`repo_name`/`subject_url` (built in
  `notification_service.go`). Both tables have `repo_id`.
- `events.payload` JSON and any email already sent.
- Webhook payloads built after the rename use the current name (`repository.full_name`); `repository.id` is the stable
  key. Receivers that key on `full_name` see a new name from the next delivery.
- Markdown in issue and PR bodies, wikis and READMEs that link to the old path: not rewritten, covered by the redirect.
- `idx_repos_owner_name_lower` already serves case-insensitive lookups.

## Decisions

All settled with the maintainer on 2026-11-12.

1. **Redirect lifetime.** An old path redirects until a new repo claims the name; the new repo then wins and the alias
   is deleted. Otherwise aliases live forever. Renaming a repo back to a name it once had always works (its own alias is
   dropped). No quarantine window.
2. **Git.** HTTP answers `301` on `info/refs` for both services and `308` for a bare `POST` to an old
   `git-upload-pack`/`git-receive-pack` path. SSH resolves the alias itself and prints a stderr hint
   (`warning: repository moved to acme/core; update your remote`). Pushes through an old path are allowed and audited
   under the repo's real id. An alias resolves only for a caller who can read the target repo; anyone else gets exactly
   what a missing repo gets.
3. **Who.** `RepoService.CanManage` (owner, org owner, `admin` collaborator), behind `confirmGrant` step-up. Transfer
   stays `IsOwner`.
4. **Transfer** also writes an alias (old owner/name to the repo id). For a transfer to another user, the alias is
   written on accept. Rename and transfer stay separate actions. A rename while a transfer is pending is allowed;
   `ErrTransferChanged` already makes the recipient re-review.
5. **Failure story.** Reuse `moveRepo` as it is: validate, check paths on disk and in the DB, rename both dirs, then one
   DB transaction (update the row, insert the old-path alias, delete any alias at the new name, update the denormalized
   text); on failure rename the dirs back. The crash window between the dir move and the commit is accepted, as for
   transfer, and documented with the manual recovery. No intent journal.
6. **Web and API.** One router middleware, before the `/{owner}/{repo}` routes, resolves an unknown `owner/repo` through
   the alias table: `301` for GET/HEAD (sub-path and query kept; `HX-Redirect` for an HTMX GET), `308` for everything
   else. Same visibility rule as git.
7. **Stored text.** Rename updates `events` and `notifications` names and the `subject_url` prefix for the repo id in
   the rename transaction. `events.payload` and sent emails are not rewritten.
8. **Webhooks.** No new `repository` event in this spec. Docs state that `repository.id` is the stable key.
9. **Delete and restore.** Aliases follow the repo: not resolved while it is soft-deleted, resolved again on restore
   unless a live repo or newer alias took the name (restore then drops that alias and succeeds), deleted with the repo
   on purge (`ON DELETE CASCADE`). A live repo or rename always beats an alias, including one belonging to a trashed repo.
10. **Eligibility.** A rename is refused with `409` while a mirror sync holds its lease or an import is running.
    Archived repos and mirrors can be renamed (no refs or commits change). In-flight pushes aren't blocked; one that
    loses the race fails with the existing "ref changed" rejection. Names are validated with `ValidateRepoName`
    (`.wiki` stays reserved); an unchanged name is `422`. Case-only renames are allowed, so `moveRepo`'s "path taken"
    check must skip the repo's own dirs.
11. **UI and API.** The General card's name field becomes editable with its own **Rename** button (a separate save from
    the rest of General, because it moves directories). It opens the danger-confirm dialog stating the new URL and that
    old links and remotes keep redirecting. Route `POST /{owner}/{repo}/settings/rename`; `name` is also accepted on
    `PATCH /api/repos/{owner}/{repo}`, and a request that mixes `name` with other fields is `422`. New audit action
    `repo.rename` recording the old and new name. After a rename the UI redirects to the new settings URL.

## Design

### Alias table (migration 122)

```sql
CREATE TABLE repo_aliases (
    id          BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    repo_id     BIGINT NOT NULL REFERENCES repositories(id) ON DELETE CASCADE,
    owner_name  TEXT   NOT NULL,
    name        TEXT   NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE UNIQUE INDEX idx_repo_aliases_path ON repo_aliases (lower(owner_name), lower(name));
CREATE INDEX idx_repo_aliases_repo ON repo_aliases (repo_id);
```

- Rows point at a `repo_id`, never at another alias, so a chain `a → b → c` is flat: resolution is one lookup and the
  alias path of an earlier name keeps working after a later rename.
- The unique index is case-insensitive like `NameHeld`. A path is either a live repo, an alias, or free.
- Resolution joins `repositories` with `deleted_at IS NULL`, giving decision 9 without extra state.
- Generic on purpose: ticket 34 can add an org-level alias (`owner_name` only) or reuse this table for org renames.

### Service surface

- `RepoStore`/`RepoService`: `ResolveAlias(ctx, owner, name) (*model.Repository, error)`, `AddAlias`, `DropAlias`.
- `RepoService.RenameRepo(ctx, repo, requesterID, newName)`: checks `CanManage`, `ValidateRepoName`, the 409 guards,
  then `moveRepo`-style dir move and one transaction. `RepoService.Get` stays an exact lookup; callers that redirect
  call `ResolveAlias` only after a miss, so the live path pays nothing.
- Creating a repo (`claimRepo`), forking, importing and restoring consult the alias table and drop a conflicting alias in
  the same transaction as the insert.

### Redirect behaviour

| Request                                      | Old path, caller can read target | Otherwise              |
| -------------------------------------------- | -------------------------------- | ---------------------- |
| `GET`/`HEAD` page, raw, archive              | `301`, sub-path and query kept   | the missing-repo reply |
| `POST`/`PATCH`/... form or `/api/repos/...`  | `308`                            | the missing-repo reply |
| Git HTTP `info/refs` (either service)        | `301` to the new `info/refs`     | `401`/`404` as today   |
| Git HTTP bare `POST` to `git-*-pack`         | `308`                            | `401`/`404` as today   |
| SSH `git-upload-pack`/`git-receive-pack`     | served from the real repo, stderr hint | `repository not found` |

## Slicing

| #   | Ticket                                                                       | Blocked by | Parallel with |
| --- | ---------------------------------------------------------------------------- | ---------- | ------------- |
| 01  | [Alias table and lifecycle rules](./issues/01-alias-table.md)                | none       |               |
| 02  | [Rename service, API and stored text](./issues/02-rename-service.md)         | 01         | 04, 05        |
| 03  | [Rename UI and mock](./issues/03-rename-ui.md)                               | 02         | 04, 05, 06    |
| 04  | [Web and API redirects](./issues/04-web-redirects.md)                        | 01         | 02, 05        |
| 05  | [Git transport redirects](./issues/05-git-redirects.md)                      | 01         | 02, 04        |
| 06  | [Transfer writes aliases](./issues/06-transfer-aliases.md)                   | 02         | 03, 04, 05    |
| 07  | [Docs, ROADMAP and end-to-end check](./issues/07-docs-and-e2e.md)            | 02–06      |               |

Shared files: 02 and 06 both edit `moveRepo`'s update closure area in `repo_transfer.go`/`repo_dirs.go`, hence 06 follows
02. 01 owns migration 122 and the `stores.go`/`services.go` wiring; nobody else adds a migration. 04 owns `router.go`
and `middleware/setup.go`; 05 owns `git_http.go` and `ssh/server.go`; 03 owns `repo_settings.templ` and `mockups/`.
Re-check the migration number after merging main (ticket 34 may add one).

## Testing

- Service tests (real DB, `make test-integration`): alias creation and flattening, claim and restore conflicts,
  case-only rename, 409 guards, dir revert on a failed transaction, denormalized text update.
- Router tests (throwaway `newVerificationRouter` harness): `301`/`308`, query kept, private repo gets the missing reply.
- Git: `TestGitHTTP_PrivateRepo_LooksLikeMissingRepo` extended to an alias; an `internal/ssh/server_test.go` clone and
  push through an old path with native git.
- UI: Playwright check of the rename dialog at 1440 in light and dark, as for every UI ticket.

## Verification

A throwaway server on a scratch DB and unique `auth.cookie_name`, signed in as `admin@example.test` /
`cloudzilla-seed`: rename a repo with an open PR, a webhook and a wiki; confirm web, `git clone`/`fetch`/`push` over HTTP
and SSH from the old path, the activity feed and notifications labels, and a second user's private-repo 404. Rename,
transfer, delete, restore and reuse the old name in turn.

## Out of scope

- A `repository` webhook event (`renamed`/`transferred`); follow-up, see decision 8.
- Organization and user name change (ticket 34).
- Rewriting markdown or `events.payload` text; a quarantine window for old names; a crash-recovery journal.
- Mobile.

## Comments
