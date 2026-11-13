# Repository rename and redirects

A repository's identity is `repositories.id`; its path is the pair `owner_name` + `name` on that row. Renaming changes
the name, a [transfer](./access-control.md#repository-transfers) changes the owner, and both leave the old path behind
as an **alias** that redirects to the repository. Nothing that keys on the id moves: pull requests, issues, stars,
collaborators, deploy keys, webhooks, mirrors, forks and the wiki all carry over.

## Who may rename

`RepoService.RenameRepo` needs `CanManage`: the owner, an org owner, or an `admin` collaborator. Transfer stays
`IsOwner`. The request also passes `confirmGrant`, the same step-up as a transfer (password, plus the 2FA code when
enabled; a `repo:admin` token skips it, a plain token can't rename).

| Surface  | Request                                                                                                   |
| -------- | --------------------------------------------------------------------------------------------------------- |
| Settings | **Rename** on the General card, which opens a confirmation dialog; `POST /{owner}/{repo}/settings/rename` |
| API      | `PATCH /api/repos/{owner}/{repo}` with `name` (see the [API reference](./api-reference.md))               |

`name` on the API can't be combined with `description`, `website` or `license`: that is `422`, because a rename moves
directories and the other fields don't. After a settings rename the browser lands on the new settings URL. Each rename
is audited as `repo.rename` with `from` and `to`. `cz` has no rename command and, signing no requests, can't make one
(see [cz](./cli.md#what-cz-cant-do)).

## What is refused

| Reason                                                             | Status |
| ------------------------------------------------------------------ | ------ |
| Invalid name, a name ending in `.wiki`, or the name it already has | `422`  |
| The name is held in the namespace, or a directory is left on disk  | `409`  |
| A mirror sync holds its lease                                      | `409`  |
| Reader or writer                                                   | `403`  |
| The caller can't read the repo                                     | `404`  |

"Held" is `RepoStore.NameHeld`: case-insensitive, soft-deleted personal repos included, and a repo named `<name>.wiki`
counts because its git dir would be this repo's wiki. A case-only rename (`Core` to `core`) is allowed and writes no
alias. Archived repos and mirrors can be renamed: no refs or commits change. A repository import has no row until it
publishes, so there is nothing to rename mid-import. The sync check is a point-in-time read; a sync that claims the
lease just after it fails on its stale directory, as does a push that loses the race (`ref changed`).

## Alias rules

`repo_aliases` (migration 122) maps a former `owner_name` + `name` to a `repo_id`.

- Rows point at a repo, never at another alias, so a chain `a → b → c` stays flat and `a` keeps working after the rename
  to `c`. Resolution is one lookup.
- The unique index is on `lower(owner_name), lower(name)`: a path is a live repo, an alias, or free.
- **A live repo or a rename always beats an alias.** Creating, forking, importing, renaming or transferring a repo onto
  an aliased path deletes the alias in the same statement or transaction. Renaming a repo back to a name it once had
  drops its own alias.
- No quarantine: the old name is reusable at once, and the new repo owns it from then on.
- **Delete and restore.** Aliases follow the repo. An alias of a soft-deleted repo doesn't resolve, resolves again on
  restore, and goes with the row on purge (`ON DELETE CASCADE`). A restore succeeds even if a newer repo or alias took
  the path meanwhile: `RepoStore.Restore` drops the conflicting aliases.
- Only the repo's current path appears in the UI and in new links. Markdown that links to an old path is not rewritten;
  the redirect covers it.

## Redirects

An alias resolves only for a caller who can read the target. Anyone else, an anonymous visitor included, gets exactly
what a missing repo gets, so an alias never confirms a private repo.

| Request                                              | Old path, caller can read the target     | Otherwise              |
| ---------------------------------------------------- | ---------------------------------------- | ---------------------- |
| `GET`/`HEAD` page, raw, archive                      | `301`, sub-path and query kept           | the missing-repo reply |
| HTMX `GET`                                           | `204` with `HX-Redirect`                 | the missing-repo reply |
| Any other method, form or `/api/repos/...`           | `308`                                    | the missing-repo reply |
| Git HTTP `info/refs` (either service)                | `301` to the new `info/refs`             | `401`/`404` as today   |
| Git HTTP `POST` to `git-upload-pack`/`-receive-pack` | `308`                                    | `401`/`404` as today   |
| SSH `git-upload-pack`/`git-receive-pack`             | served from the real repo, stderr notice | `repository not found` |

The web and API redirect is the `RepoAlias` middleware (`internal/middleware/repo_alias.go`). It lets the request run,
holds back a `404`, and only then asks `ResolveAlias`, so a live path pays nothing; git transport paths are left to
their own handlers. Git HTTP resolves in `gitReadableRepo`, SSH in the session handler; see
[git-transport](./git-transport.md#renamed-repositories).

Git follows the `301` on `info/refs` for the rest of the operation and prints `warning: redirecting to ...`. It does not
rewrite the stored remote, so clones keep working through the old URL until `git remote set-url`. SSH prints
`warning: repository moved to <owner>/<name>; update your remote` on stderr. A push through an old path is allowed and
audited, logged and dispatched under the repo's real id and name.

## Transfers

A transfer writes an alias for the old `owner/name`, and so does the rename of a transferred repo. A transfer to another
user writes it when they accept. A rename while a transfer is pending is allowed: the offer shows the old `owner/name`,
so accepting is refused with `409` (`ErrTransferChanged`) until the recipient reviews it again.

## Stored names

One transaction updates the row, adds the alias, drops any alias at the new path, and rewrites the names kept as text:
`events.owner_name`/`repo_name`, `notifications.owner_name`/`repo_name` and the `subject_url` prefix. Not rewritten:
`events.payload`, emails already sent, and markdown in issues, pull requests, wikis and READMEs.

## Webhooks

Deliveries after a rename carry the current `repository.name`; `repository.id` never changes, so a receiver should key
on it. Only release payloads carry a `full_name` (`owner/name`). There is no `renamed` event yet. See
[webhooks](./webhooks.md).

## Failure and manual recovery

`moveRepo` renames `<name>.git` and `<name>.wiki.git`, then runs the database transaction, and renames the directories
back if it fails. That is the same shape as a transfer. The window between the directory move and the commit is
accepted, with no journal: a crash there leaves the directories at the new name while the row still has the old one, and
the repository answers as broken until fixed.

To recover, stop the server (or accept that the repo is unavailable meanwhile) and move the directories back under
`git.repos_root`:

```bash
cd "$CZ_GIT_REPOS_ROOT/<owner>"
mv <new>.git <old>.git
mv <new>.wiki.git <old>.wiki.git   # only if it exists
```

then repeat the rename. For a crashed transfer the directory sits under `<new owner>/` instead; move it back under
`<old owner>/`. To finish the rename instead, update `repositories.name`, add the `repo_aliases` row and rewrite the
`events` and `notifications` names by hand; moving the directories back is the safer repair.

## Tests

`internal/service/repo_rename_test.go`, `repo_alias_test.go` and `repo_transfer_alias_test.go` cover the service rules
against a real database; `internal/router/repo_alias_redirect_test.go` the web and API statuses; `TestGitHTTP_Alias_*`
in `internal/handler/git_http_access_test.go` and `TestSessionHandler_Alias` in `internal/ssh/server_test.go` the git
transports. [testing](./testing.md) explains which of them CI runs.
