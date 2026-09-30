# API Reference

All JSON endpoints are under `/api/`. Authentication uses a JWT in an httpOnly cookie (`cz_token`) or an `Authorization: Bearer <token>` header. Personal access tokens (`czp_...`) are also accepted in the `Authorization` header, as are OAuth-app tokens, which are limited to the routes their scopes admit (see [OAuth Apps](#oauth-apps)).

---

## Auth

| Method | Path                    | Auth | Description                                                                                                                                                                                                                    |
| ------ | ----------------------- | ---- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| POST   | `/api/auth/login`       | --   | Login (JSON `email`, `password`); sets `cz_token` cookie and returns token in body. A user with TOTP on gets `401 {"error":"totp_required"}` and must use a PAT ([details](./access-control.md#two-factor-authentication))     |
| POST   | `/api/auth/logout`      | --   | Clears auth cookie; 204, or form 303/HTMX `HX-Redirect` to `/`                                                                                                                                                                 |
| GET    | `/auth/google`          | --   | Begin Google OAuth flow (redirects to Google)                                                                                                                                                                                  |
| GET    | `/auth/google/callback` | --   | Google OAuth callback; sets `cz_token` cookie, redirects to `/`; re-renders login with 403 if Google hasn't verified the email, 409 if an account already has that email ([details](./access-control.md#google-oauth-sign-in)) |
| POST   | `/auth/ldap`            | --   | LDAP login (username + password)                                                                                                                                                                                               |
| GET    | `/auth/saml`            | --   | Initiate SAML SSO flow (redirects to IdP)                                                                                                                                                                                      |
| POST   | `/auth/saml/callback`   | --   | SAML assertion consumer service (ACS) callback                                                                                                                                                                                 |
| GET    | `/auth/saml/metadata`   | --   | SAML SP metadata XML                                                                                                                                                                                                           |

When the `oauth_link_state` cookie matches `state`, `/auth/google/callback` finishes [connecting Google](#connected-accounts) to the signed-in account instead: it never signs in, and redirects to `/settings#connected-accounts`.

## Two-Factor Authentication (TOTP)

| Method | Path                     | Auth     | Description                                      |
| ------ | ------------------------ | -------- | ------------------------------------------------ |
| GET    | `/auth/2fa`              | --       | TOTP verification page (reads `cz_totp_pending`) |
| POST   | `/auth/2fa/verify`       | --       | Verify TOTP code or backup code                  |
| POST   | `/api/user/totp/enable`  | Required | Enable TOTP (submit code to confirm setup)       |
| POST   | `/api/user/totp/disable` | Required | Disable TOTP                                     |

## Connected Accounts

Browser form posts from Account settings. Both need the current `password`, plus `code` (the TOTP code) when two-factor authentication is on. Errors redirect (303) to `/settings?profile_error=google_…#connected-accounts`. OAuth-app tokens are refused. See [access control](./access-control.md#connecting-google-to-an-existing-account).

| Method | Path                                             | Auth     | Description                                                                                                                                                                                               |
| ------ | ------------------------------------------------ | -------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| POST   | `/settings/connected-accounts/google`            | Required | Re-authenticates, sets the `oauth_link_state` cookie and redirects (303) to Google. Refused when Google OAuth isn't configured or the account is already linked. The callback writes `user.oauth.connect` |
| POST   | `/settings/connected-accounts/google/disconnect` | Required | Re-authenticates and unlinks Google. Refused for an account without a password. Writes `user.oauth.disconnect` to the audit log                                                                           |

## SSH Keys

| Method | Path                 | Auth     | Description                          |
| ------ | -------------------- | -------- | ------------------------------------ |
| GET    | `/api/user/keys`     | Required | List SSH keys for authenticated user |
| POST   | `/api/user/keys`     | Required | Add a new SSH public key             |
| DELETE | `/api/user/keys/:id` | Required | Delete an SSH key by ID              |

## Personal Access Tokens

| Method | Path                   | Auth     | Description                                                                                                                                                        |
| ------ | ---------------------- | -------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| POST   | `/api/user/tokens`     | Required | Create PAT (`name`, repeated `scopes`, optional `expires_at` form fields); redirects to `/settings#tokens`, which shows the raw token once, via an HttpOnly cookie |
| DELETE | `/api/user/tokens/:id` | Required | Revoke a PAT by ID                                                                                                                                                 |

Raw token format: `czp_<32-byte hex>`. Use as `Authorization: Bearer czp_<token>`. Only the SHA-256 hash is stored; the raw value cannot be recovered after creation.

## Commit Email Privacy

| Method | Path              | Auth     | Description                                                                                             |
| ------ | ----------------- | -------- | ------------------------------------------------------------------------------------------------------- |
| POST   | `/settings/email` | Required | Form: `keep_email_private=on` turns the setting on, omitting it turns it off. Redirects to `/settings#email` |

Commits made through the web UI (new files, wiki edits, merge and squash merges, applied suggestions) are authored as `<username> <email>`, where the email comes from `UserService.CommitAuthor`:

- Setting on (the default for every user), or no account email: `<user id>+<username>@users.noreply.<host>`, with `<host>` taken from `server.base_url`.
- Setting off: the account email.

Commits pushed over git keep whatever author the client set. Contributor stats resolve a noreply author back to its user when both the id and username match, under any host, so commits made before a `base_url` change stay credited. Legacy `username@localhost` authors are not resolved.

## Deploy Keys

| Method | Path                               | Auth      | Description                                         |
| ------ | ---------------------------------- | --------- | --------------------------------------------------- |
| GET    | `/api/repos/:owner/:repo/keys`     | CanManage | List deploy keys for a repository                   |
| POST   | `/api/repos/:owner/:repo/keys`     | CanManage | Add deploy key (`title`, `public_key`, `read_only`) |
| DELETE | `/api/repos/:owner/:repo/keys/:id` | CanManage | Delete a deploy key by ID                           |

Deploy keys authenticate via SSH using the key's MD5 fingerprint. A `read_only` key cannot push; a read-write key can. Each key is scoped to a single repository.

## Users

| Method | Path                                  | Auth     | Description                                                                                           |
| ------ | ------------------------------------- | -------- | ----------------------------------------------------------------------------------------------------- |
| GET    | `/api/users/:username`                | --       | Get a user's public profile                                                                           |
| GET    | `/api/users/:username/repos`          | --       | List user's repositories                                                                              |
| POST   | `/api/users/:id/pinned-repos/:repoID` | Required | Pin a repo to the profile; idempotent; 422 past 6 pins; 404 if the repo is not readable by the caller |
| DELETE | `/api/users/:id/pinned-repos/:repoID` | Required | Unpin a repo from the profile; no-op if it is not pinned                                              |

`:id` is a numeric user ID and must be the caller's own (403 otherwise). Both pin endpoints return `{"ok": true}`.

Public user objects — returned by `GET /api/users/:username` and by `/api/repos/:owner/:repo/stargazers` (JSON only with `HX-Request: true`; otherwise it renders the stargazers page) — contain only `id`, `username`, `bio`, `avatar_url`, and `created_at`. Email addresses and notification preferences are never returned.

## Repositories

| Method | Path                                | Auth     | Description                                                                         |
| ------ | ----------------------------------- | -------- | ----------------------------------------------------------------------------------- |
| GET    | `/api/repos/`                       | --       | List all repositories                                                               |
| POST   | `/api/repos/`                       | Required | Create a repository (`name`, `description`, `private`, plus the init options below) |
| GET    | `/api/repos/:owner/:repo`           | --       | Get repository details                                                              |
| POST   | `/api/repos/:owner/:repo/fork`      | Required | Fork into authenticated user's namespace                                            |
| POST   | `/api/repos/:owner/:repo/transfer`  | IsOwner  | Transfer repo to another user                                                       |
| POST   | `/api/repos/:owner/:repo/restore`   | IsOwner  | Restore a soft-deleted repository                                                   |
| POST   | `/api/repos/:owner/:repo/archive`   | IsOwner  | Archive a repository                                                                |
| POST   | `/api/repos/:owner/:repo/unarchive` | IsOwner  | Unarchive a repository                                                              |
| PATCH  | `/api/repos/:owner/:repo/template`  | IsOwner  | Toggle repository template flag                                                     |
| POST   | `/api/repos/from-template`          | Required | Create a new repo from a template                                                   |

Repository creation (here and under `/api/orgs/:org/repos`) accepts optional init options that seed an initial commit: `add_readme` (bool), `gitignore` (`Go`, `Node`, `Python`, `Rust`, `Java`, `C++`, `Ruby`), and `license` (`mit`, `apache-2.0`, `gpl-3.0`, `bsd-3-clause`, `unlicense`). An unknown template name leaves the repository empty rather than failing the request.

Creating a repository (here, under `/api/orgs/:org/repos`, or from a template) with a name already used in that namespace returns 422 `a repository with that name already exists` and creates nothing. A directory left on disk without a repository row counts as used, so a new repository never takes over an earlier holder's data; a fork skips such names the same way it skips existing repositories. `POST /api/repos/from-template` checks the name with the same rules as create and returns 422 `invalid repository name: ...` for one it rejects.

A transfer is refused with 422 `transfer failed`, and nothing moves, when the new owner already has a repository with that name, or a `<name>.git` or `<name>.wiki.git` directory left on disk under it.

Names ending in `.wiki` (in any case) are reserved, because `<name>.wiki.git` is the wiki of repository `<name>`: creating one returns 422 `invalid repository name: names ending in .wiki are reserved for wikis`, and a fork of such a repository gets a `-1` suffix. A `<name>.wiki` repository created before the reservation still works, but while it exists, even soft-deleted, `<name>` cannot be created in or transferred into its namespace (422 `a repository with that name already exists` on create), it cannot be transferred into a namespace that holds `<name>`, and an existing `<name>` has no wiki.

Deleting a repository moves its directories to `<name>.git.deleted.<unix_ts>` and `<name>.wiki.git.deleted.<unix_ts>`, with the same second stored in `deleted_at`. Restore and the 30-day purge act only on the copy whose suffix matches the row, never on another soft-deleted repository of the same name (org repos are unique per creator, so several can exist). Restore returns 422 `a repository with that name already exists` while another repository, or a directory left on disk, holds the name. It returns 500 `restore failed`, and leaves the row deleted, when the row's copy is missing from disk. Deletes made before wikis moved with their repository left `<name>.wiki.git` in place; the purge removes such a wiki once no repository row, live or soft-deleted, names it, so the name can be reused.

`POST /api/repos/` returns `422` with the naming rule when `name` isn't a valid repository name. It returns `422` with a different message when the caller's username predates the owner-name rule and isn't a valid path segment (`service.ValidateName`).

`POST /api/repos/from-template` takes form fields `template_repo_id` and `name`, and redirects to the new repository. A `name` that isn't a valid repository name returns `422` with the naming rule. The same `422` comes back, with a different message, when the caller's username predates the owner-name rule and isn't a valid path segment.

## Issues

| Method | Path                                                  | Auth     | Description                              |
| ------ | ----------------------------------------------------- | -------- | ---------------------------------------- |
| GET    | `/api/repos/:owner/:repo/issues/`                     | --       | List issues                              |
| POST   | `/api/repos/:owner/:repo/issues/`                     | Required | Create an issue                          |
| GET    | `/api/repos/:owner/:repo/issues/:number`              | --       | Get issue details                        |
| PATCH  | `/api/repos/:owner/:repo/issues/:number`              | CanWrite | Update issue (open/close)                |
| PATCH  | `/api/repos/:owner/:repo/issues/:number/pin`          | CanWrite | Pin or unpin an issue                    |
| PATCH  | `/api/repos/:owner/:repo/issues/:number/lock`         | CanWrite | Lock or unlock an issue                  |
| GET    | `/api/repos/:owner/:repo/issues/:number/comments`     | --       | List comments                            |
| POST   | `/api/repos/:owner/:repo/issues/:number/comments`     | Required | Add a comment                            |
| PATCH  | `/api/repos/:owner/:repo/issues/:number/comments/:id` | Required | Edit a comment body (author only)        |
| DELETE | `/api/repos/:owner/:repo/issues/:number/comments/:id` | Required | Delete a comment (author or repo writer) |

## Labels

| Method | Path                                                     | Auth     | Description                                   |
| ------ | -------------------------------------------------------- | -------- | --------------------------------------------- |
| GET    | `/api/repos/:owner/:repo/labels/`                        | --       | List all labels for a repository              |
| POST   | `/api/repos/:owner/:repo/labels/`                        | CanWrite | Create label (`name`, `color`, `description`) |
| DELETE | `/api/repos/:owner/:repo/labels/:id`                     | CanWrite | Delete label by ID                            |
| POST   | `/api/repos/:owner/:repo/issues/:number/labels/:labelID` | CanWrite | Add label to issue (HTMX-aware)               |
| DELETE | `/api/repos/:owner/:repo/issues/:number/labels/:labelID` | CanWrite | Remove label from issue (HTMX-aware)          |
| POST   | `/api/repos/:owner/:repo/pulls/:number/labels/:labelID`  | CanWrite | Add label to pull request (HTMX-aware)        |
| DELETE | `/api/repos/:owner/:repo/pulls/:number/labels/:labelID`  | CanWrite | Remove label from pull request (HTMX-aware)   |

## Assignees

| Method | Path                                                          | Auth     | Description                                                   |
| ------ | ------------------------------------------------------------- | -------- | ------------------------------------------------------------- |
| POST   | `/api/repos/:owner/:repo/issues/:number/assignees`            | CanWrite | Add assignee to issue (`username` in body; HTMX-aware)        |
| DELETE | `/api/repos/:owner/:repo/issues/:number/assignees?username=X` | CanWrite | Remove assignee from issue (HTMX-aware)                       |
| POST   | `/api/repos/:owner/:repo/pulls/:number/assignees`             | CanWrite | Add assignee to pull request (`username` in body; HTMX-aware) |
| DELETE | `/api/repos/:owner/:repo/pulls/:number/assignees?username=X`  | CanWrite | Remove assignee from pull request (HTMX-aware)                |

## Stars

| Method | Path                                 | Auth     | Description                           |
| ------ | ------------------------------------ | -------- | ------------------------------------- |
| POST   | `/api/repos/:owner/:repo/star`       | Required | Star a repository (HTMX-aware)        |
| DELETE | `/api/repos/:owner/:repo/star`       | Required | Unstar a repository (HTMX-aware)      |
| GET    | `/api/repos/:owner/:repo/stargazers` | --       | List users who starred the repository |

## Watching

| Method | Path                            | Auth     | Description                       |
| ------ | ------------------------------- | -------- | --------------------------------- |
| PUT    | `/api/repos/:owner/:repo/watch` | Required | Watch a repository (HTMX-aware)   |
| DELETE | `/api/repos/:owner/:repo/watch` | Required | Unwatch a repository (HTMX-aware) |
| GET    | `/api/repos/:owner/:repo/watch` | Optional | Get watch button state            |

## Forks

| Method | Path                           | Auth     | Description                                                                        |
| ------ | ------------------------------ | -------- | ---------------------------------------------------------------------------------- |
| POST   | `/api/repos/:owner/:repo/fork` | Required | Fork the repository into the authenticated user's namespace; redirects to fork URL |

## Pull Requests

| Method | Path                                    | Auth     | Description                                                                                                                                |
| ------ | --------------------------------------- | -------- | ------------------------------------------------------------------------------------------------------------------------------------------ |
| GET    | `/api/repos/:owner/:repo/pulls/`        | --       | List pull requests                                                                                                                         |
| POST   | `/api/repos/:owner/:repo/pulls/`        | Required | Create a pull request                                                                                                                      |
| GET    | `/api/repos/:owner/:repo/pulls/:number` | --       | Get PR details                                                                                                                             |
| PATCH  | `/api/repos/:owner/:repo/pulls/:number` | CanWrite | Update PR state. `state=merged` merges using `merge_strategy` (`ff` default, `merge`, or `squash`); `state=closed` closes without merging. |

## PR Reviews

| Method | Path                                            | Auth     | Description                                                                                                           |
| ------ | ----------------------------------------------- | -------- | --------------------------------------------------------------------------------------------------------------------- |
| GET    | `/api/repos/:owner/:repo/pulls/:number/reviews` | Optional | List all reviews for a pull request                                                                                   |
| POST   | `/api/repos/:owner/:repo/pulls/:number/reviews` | Required | Submit or update a review (`state`, `body`); upserts per reviewer; `state=changes_requested` blocks all merge buttons |

Valid `state` values: `approved`, `changes_requested`, `commented`, `pending`.

## PR Line Comments

| Method | Path                                                            | Auth     | Description                                                    |
| ------ | --------------------------------------------------------------- | -------- | -------------------------------------------------------------- |
| GET    | `/api/repos/:owner/:repo/pulls/:number/line_comments`           | Optional | List all line comments for a pull request                      |
| POST   | `/api/repos/:owner/:repo/pulls/:number/line_comments`           | Required | Create a line comment (`path`, `line`, `body`, `diff_side`)    |
| GET    | `/api/repos/:owner/:repo/pulls/:number/line_comments/form`      | Required | Returns inline comment form HTML fragment (`?path=...&line=N`) |
| PATCH  | `/api/repos/:owner/:repo/pulls/:number/line_comments/:id`       | Required | Edit a line comment body (author only)                         |
| DELETE | `/api/repos/:owner/:repo/pulls/:number/line_comments/:id`       | Required | Delete a line comment (author or repo writer only)             |
| POST   | `/api/repos/:owner/:repo/pulls/:number/line_comments/:id/apply` | CanWrite | Apply a code review suggestion                                 |

Applying a suggestion, merging a PR, and editing the wiki return `409` when a push moved the branch while the change was being committed; the push is kept and the client should reload and retry (see [pr-merge](./pr-merge.md#merge-flow)).

## Reactions

| Method | Path                                             | Auth     | Description                          |
| ------ | ------------------------------------------------ | -------- | ------------------------------------ |
| GET    | `/api/repos/:owner/:repo/comments/:id/reactions` | Optional | List reactions on a comment          |
| POST   | `/api/repos/:owner/:repo/comments/:id/reactions` | Required | Toggle a reaction emoji on a comment |

## Branch Protections

| Method | Path                                               | Auth      | Description                                                                                      |
| ------ | -------------------------------------------------- | --------- | ------------------------------------------------------------------------------------------------ |
| GET    | `/api/repos/:owner/:repo/branches/protections`     | CanManage | List all branch protection rules for the repository                                              |
| POST   | `/api/repos/:owner/:repo/branches/protections`     | CanManage | Create a rule (`pattern`, `require_review_count`, `require_status_checks[]`, `block_force_push`) |
| PATCH  | `/api/repos/:owner/:repo/branches/protections/:id` | CanManage | Update an existing rule (same fields as POST; only provided fields are changed)                  |
| DELETE | `/api/repos/:owner/:repo/branches/protections/:id` | CanManage | Delete a protection rule                                                                         |

## Branches & Tags

| Method | Path                                      | Auth     | Description                                                                   |
| ------ | ----------------------------------------- | -------- | ----------------------------------------------------------------------------- |
| POST   | `/api/repos/:owner/:repo/branches`        | CanWrite | Create branch (`name`, `from` form fields; `from` defaults to default branch) |
| DELETE | `/api/repos/:owner/:repo/branches?name=X` | CanWrite | Delete branch (default branch rejected with 400)                              |
| POST   | `/api/repos/:owner/:repo/tags`            | CanWrite | Create tag (`name`, `from` form fields)                                       |
| DELETE | `/api/repos/:owner/:repo/tags?name=X`     | CanWrite | Delete tag                                                                    |

All four endpoints require write access. For HTMX requests they return an HTML fragment; otherwise JSON.

## Topics

| Method | Path                             | Auth     | Description                          |
| ------ | -------------------------------- | -------- | ------------------------------------ |
| PUT    | `/api/repos/:owner/:repo/topics` | CanWrite | Set repository topics (replaces all) |
| GET    | `/api/repos/:owner/:repo/topics` | Optional | Get topics fragment (HTMX-aware)     |

## Releases

| Method | Path                                      | Auth     | Description                                                                |
| ------ | ----------------------------------------- | -------- | -------------------------------------------------------------------------- |
| GET    | `/api/repos/:owner/:repo/releases`        | --       | List releases                                                              |
| POST   | `/api/repos/:owner/:repo/releases`        | CanWrite | Create a release (`tag_name`, `name`, `body`, `is_prerelease`, `is_draft`) |
| GET    | `/api/repos/:owner/:repo/releases/latest` | --       | Get the latest non-draft release                                           |
| GET    | `/api/repos/:owner/:repo/releases/:id`    | --       | Get release by ID                                                          |
| PATCH  | `/api/repos/:owner/:repo/releases/:id`    | CanWrite | Update a release                                                           |
| DELETE | `/api/repos/:owner/:repo/releases/:id`    | CanWrite | Delete a release; HTMX requests return `HX-Redirect` to the releases list  |

## Commit Statuses

| Method | Path                                          | Auth     | Description                                                                  |
| ------ | --------------------------------------------- | -------- | ---------------------------------------------------------------------------- |
| POST   | `/api/repos/:owner/:repo/statuses/:sha`       | Required | Create or update a status (`state`, `context`, `target_url`, `description`)  |
| GET    | `/api/repos/:owner/:repo/statuses/:sha`       | --       | List all statuses for a commit SHA                                           |
| GET    | `/api/repos/:owner/:repo/commits/:sha/status` | --       | Get combined status (`{state, statuses:[]}`) -- aggregated from all contexts |

Valid `state` values: `pending`, `success`, `failure`, `error`. Combined state uses worst-case: `error` > `failure` > `pending` > `success`.

## Milestones

| Method | Path                                               | Auth     | Description                                                    |
| ------ | -------------------------------------------------- | -------- | -------------------------------------------------------------- |
| GET    | `/api/repos/:owner/:repo/milestones`               | --       | List all milestones (with open/closed issue counts)            |
| POST   | `/api/repos/:owner/:repo/milestones`               | CanWrite | Create a milestone (`title`, `description`, `due_date`)        |
| GET    | `/api/repos/:owner/:repo/milestones/:number`       | --       | Get milestone by number                                        |
| PATCH  | `/api/repos/:owner/:repo/milestones/:number`       | CanWrite | Update or change state (`state=closed`/`open` to close/reopen) |
| DELETE | `/api/repos/:owner/:repo/milestones/:number`       | CanWrite | Delete a milestone (issues/PRs have `milestone_id` cleared)    |
| POST   | `/api/repos/:owner/:repo/issues/:number/milestone` | CanWrite | Set or remove milestone on an issue (`milestone_id` in body)   |
| POST   | `/api/repos/:owner/:repo/pulls/:number/milestone`  | CanWrite | Set or remove milestone on a pull request                      |

## Projects (Kanban)

| Method | Path                                                  | Auth     | Description                 |
| ------ | ----------------------------------------------------- | -------- | --------------------------- |
| POST   | `/api/repos/:owner/:repo/projects`                    | Required | Create a project board      |
| DELETE | `/api/repos/:owner/:repo/projects/:id`                | Required | Delete a project board      |
| POST   | `/api/repos/:owner/:repo/projects/:id/columns`        | Required | Create a column             |
| DELETE | `/api/repos/:owner/:repo/projects/:id/columns/:colID` | Required | Delete a column             |
| POST   | `/api/repos/:owner/:repo/projects/:id/cards`          | Required | Create a card               |
| PATCH  | `/api/repos/:owner/:repo/projects/:id/cards/:cardID`  | Required | Move a card between columns |
| DELETE | `/api/repos/:owner/:repo/projects/:id/cards/:cardID`  | Required | Delete a card               |

## Wiki

| Method | Path                                 | Auth     | Description                  |
| ------ | ------------------------------------ | -------- | ---------------------------- |
| POST   | `/api/repos/:owner/:repo/wiki/:slug` | Required | Create or update a wiki page |
| DELETE | `/api/repos/:owner/:repo/wiki/:slug` | Required | Delete a wiki page           |

Wiki pages are stored as files in a bare git repository (`<repo>.wiki.git`) that moves with the repository when it is deleted, restored, purged or transferred. Page content is Markdown. When a repository named `<repo>.wiki` from before that suffix was reserved holds the path, `<repo>`'s wiki pages return 404 and the directory stays with `<repo>.wiki`.

## Discussions

| Method | Path                                                      | Auth     | Description                         |
| ------ | --------------------------------------------------------- | -------- | ----------------------------------- |
| POST   | `/api/repos/:owner/:repo/discussions`                     | Required | Create a discussion                 |
| POST   | `/api/repos/:owner/:repo/discussions/:number/replies`     | Required | Reply to a discussion               |
| PATCH  | `/api/repos/:owner/:repo/discussions/:number`             | Required | Mark a reply as the accepted answer |
| DELETE | `/api/repos/:owner/:repo/discussions/:number/replies/:id` | Required | Delete a reply                      |

## Gists

| Method | Path                  | Auth     | Description                  |
| ------ | --------------------- | -------- | ---------------------------- |
| POST   | `/api/gists`          | Required | Create a gist                |
| PATCH  | `/api/gists/:id`      | Required | Update a gist                |
| DELETE | `/api/gists/:id`      | Required | Delete a gist                |
| GET    | `/api/gists/file-row` | Required | Add file row (HTMX fragment) |

## Search

| Method | Path      | Auth     | Description                                                                                                                                        |
| ------ | --------- | -------- | -------------------------------------------------------------------------------------------------------------------------------------------------- |
| GET    | `/search` | Optional | Full-text search. Query params: `q` (search term), `type` (`all`, `repos`, `issues`, `pulls`, `users`). Private repos visible only to their owner. |

## Webhooks

| Method | Path                                           | Auth      | Description                                |
| ------ | ---------------------------------------------- | --------- | ------------------------------------------ |
| GET    | `/api/repos/:owner/:repo/hooks/`               | --        | List webhooks for repository               |
| POST   | `/api/repos/:owner/:repo/hooks/`               | CanManage | Create webhook (`url`, `secret`, `events`) |
| PATCH  | `/api/repos/:owner/:repo/hooks/:id`            | CanManage | Update webhook settings                    |
| DELETE | `/api/repos/:owner/:repo/hooks/:id`            | CanManage | Delete webhook                             |
| GET    | `/api/repos/:owner/:repo/hooks/:id/deliveries` | CanManage | List delivery history                      |
| POST   | `/api/repos/:owner/:repo/hooks/:id/redeliver`  | CanManage | Redeliver a webhook event                  |

Webhooks fire on `push`, `issues`, and `pull_request` events. Requests are signed with `X-Hub-Signature-256` when a secret is configured (GitHub-compatible HMAC-SHA256).

## Repository Collaborators

| Method | Path                                              | Auth      | Description                           |
| ------ | ------------------------------------------------- | --------- | ------------------------------------- |
| GET    | `/api/repos/:owner/:repo/collaborators`           | Optional  | List collaborators with usernames     |
| POST   | `/api/repos/:owner/:repo/collaborators`           | CanManage | Add collaborator (`username`, `role`) |
| DELETE | `/api/repos/:owner/:repo/collaborators?user_id=N` | CanManage | Remove collaborator by user ID        |

See [access-control.md](access-control.md) for the full permission model. `CanManage` requires owner, org owner, or `admin` collaborator role.

## Organizations

| Method | Path                                    | Auth     | Description                                                                                                                         |
| ------ | --------------------------------------- | -------- | ----------------------------------------------------------------------------------------------------------------------------------- |
| POST   | `/api/orgs/`                            | Required | Create organization (`name`, `display_name`, `description`); 422 when `name` is invalid or taken                                    |
| GET    | `/api/orgs/:org`                        | --       | Get organization by name, including `website`, `location`, `contact_email`, `default_repo_visibility`, `default_branch_name`        |
| GET    | `/api/orgs/:org/members`                | --       | List organization members                                                                                                           |
| POST   | `/api/orgs/:org/members`                | Required | Add member (`username`, `role`); owner only                                                                                         |
| DELETE | `/api/orgs/:org/members/:username`      | Required | Remove member; owner only, except a member may remove themselves; last owner blocked                                                |
| POST   | `/api/orgs/:org/members/:username/role` | Required | Change a member's role (`role` form field: `owner` or `member`); owner only; demoting the last owner is rejected                    |
| POST   | `/api/orgs/:org/repos`                  | Required | Create a repository under the organization (same body as `POST /api/repos/`); owner only                                            |
| POST   | `/api/orgs/:org/transfer`               | Required | Transfer org ownership (`new_owner`, optional `confirm_name` form fields); owner only; demotes self to member; redirects to `/:org` |
| POST   | `/api/orgs/:org/profile`                | Required | Update profile (`display_name`, `description`, `website`, `location`, `contact_email` form fields); owner only                      |
| POST   | `/api/orgs/:org/repo-defaults`          | Required | Update repo defaults (`default_repo_visibility`, `default_branch_name` form fields); owner only                                     |
| POST   | `/api/orgs/:org/delete`                 | Required | Delete the organization (`confirm_name` form field); owner only; 422 while the org still owns repositories                          |

Add, remove, and role-change requests sent with `HX-Request: true` respond with the refreshed members-list fragment.

`POST /api/orgs/:org/repos` applies the org's `default_repo_visibility` when `private` is omitted (`private` for an org whose defaults were never changed), and uses the org's `default_branch_name` as the initial branch.

`confirm_name` must equal the org name. It is required by `delete`; `transfer` checks it only when sent. `default_repo_visibility` is `public` or `private`; `default_branch_name` must be non-empty with no whitespace. `website` must be an `http`/`https` URL, and a bare host such as `acme.dev` is stored as `https://acme.dev`.

The profile, repo-defaults, and delete endpoints are browser form posts: they redirect (303) to `/orgs/:org/settings`, `/orgs/:org/settings#repo-defaults`, and `/organizations` respectively, and each writes an audit-log entry (`org.profile.update`, `org.defaults.update`, `org.delete`).

`POST /api/orgs/` takes JSON `name`, `display_name` and `description`. A `name` that breaks the owner-name rule returns `422` with the rule; a `name` that a user or organization already holds, in any case, returns `422` `{"error":"That name is already taken"}`. See [access-control](./access-control.md) for the rule.

`POST /api/orgs/:org/repos` returns `422` with the repository naming rule when `name` isn't a valid repository name, and `422` with a different message when the organization's name predates the owner-name rule and isn't a valid path segment.

## Notifications

| Method | Path                              | Auth     | Description                        |
| ------ | --------------------------------- | -------- | ---------------------------------- |
| GET    | `/api/notifications/unread-count` | Required | Returns `{"count": N}`             |
| PATCH  | `/api/notifications/:id`          | Required | Mark a single notification as read |
| POST   | `/api/notifications/read-all`     | Required | Mark all notifications as read     |

## Saved Replies

| Method | Path                    | Auth     | Description                   |
| ------ | ----------------------- | -------- | ----------------------------- |
| GET    | `/api/user/replies`     | Required | List saved replies (fragment) |
| POST   | `/api/user/replies`     | Required | Create a saved reply          |
| PATCH  | `/api/user/replies/:id` | Required | Update a saved reply          |
| DELETE | `/api/user/replies/:id` | Required | Delete a saved reply          |

## OAuth Apps

| Method | Path                            | Auth     | Description                                |
| ------ | ------------------------------- | -------- | ------------------------------------------ |
| GET    | `/oauth/authorize`              | Optional | OAuth authorization page                   |
| POST   | `/oauth/authorize`              | Required | Confirm authorization grant                |
| POST   | `/oauth/token`                  | --       | Exchange auth code for access token        |
| POST   | `/api/oauth/apps`               | Required | Register an OAuth application (HTMX-aware) |
| DELETE | `/api/oauth/apps/:id`           | Required | Delete an OAuth application (HTMX-aware)   |
| DELETE | `/api/oauth/authorizations/:id` | Required | Revoke an OAuth authorization (HTMX-aware) |

Registering an app returns `client_secret` once; only its bcrypt hash is stored. JSON callers get it in the body; HTMX form posts (`name`, `homepage_url`, `redirect_uri`, `description`) get the apps-list fragment with the secret revealed.

`POST /api/oauth/apps` takes `name`, `homepage_url`, `description` and `redirect_uris`, and returns the app with its `client_secret`, shown once. At least one redirect URI is required, each an absolute `http`/`https` URL with no fragment or comma; anything else returns `400`.

`/oauth/authorize` takes `client_id`, `redirect_uri`, `state`, and a space-delimited `scope`. `redirect_uri` must exactly match one the app registered; if it doesn't, or a scope is unknown, the response is `400` and nothing is redirected. Apps registered before redirect URIs were required have none, so they must be registered again. The consent page can't be framed (`X-Frame-Options: DENY`, `Content-Security-Policy: frame-ancestors 'none'`). Approving redirects to `redirect_uri` with `code` and `state` added to its query, keeping any query it already has; denying adds `error=access_denied` and `state` instead.

`/oauth/token` exchanges `code` (with `grant_type=authorization_code` and the `redirect_uri` sent to `/oauth/authorize`) for `{"access_token": "...", "token_type": "bearer"}`. The client authenticates with HTTP Basic (`Authorization: Basic base64(client_id:client_secret)`, each part form-encoded first, per RFC 6749 §2.3.1) or with `client_id` and `client_secret` in the form body, not both; the endpoint needs no CSRF token. A code is single-use, expires after 5 minutes, and can be redeemed only by the app it was issued to with the same `redirect_uri`; a refused attempt leaves it redeemable by its own app. Errors follow RFC 6749 §5.2: the body is `{"error": "<code>"}` and nothing else. Every response, success or error, carries `Cache-Control: no-store` (RFC 6749 §5.1).

| Status | `error`                  | When                                                                                                                          |
| ------ | ------------------------ | ----------------------------------------------------------------------------------------------------------------------------- |
| 400    | `invalid_request`        | `grant_type` or `code` missing, a malformed Basic header, or credentials in both the header and the body                      |
| 400    | `unsupported_grant_type` | `grant_type` is anything but `authorization_code`                                                                             |
| 401    | `invalid_client`         | Unknown `client_id`, wrong `client_secret`, or no credentials; adds `WWW-Authenticate: Basic realm="oauth"` if Basic was used |
| 400    | `invalid_grant`          | Code unknown, expired, already redeemed, issued to another app, or `redirect_uri` doesn't match                               |
| 500    | `server_error`           | Anything else; the cause is logged server-side                                                                                |

| Scope          | Grants                                                                                         |
| -------------- | ---------------------------------------------------------------------------------------------- |
| `repo:read`    | Read repos, issues, pulls, releases, orgs and user profiles; git clone/fetch                   |
| `repo:write`   | `repo:read`, plus repo content writes, creating repos, merging, git push                       |
| `issues:write` | Reads, plus writes under `/api/repos/:owner/:repo/issues/**`                                   |
| `pulls:write`  | Reads, plus writes under `/api/repos/:owner/:repo/pulls/**`, except merging and applying suggestions |

A request outside the token's scopes gets `403 {"error":"insufficient_scope"}` with a `WWW-Authenticate: Bearer error="insufficient_scope", scope="..."` header naming the scope to request. Only listed routes are open to OAuth tokens; account, admin and repo-administration endpoints and HTML pages never are. Route list: [access-control](./access-control.md#oauth-app-scopes).

## Instance Admin (superadmin only)

| Method | Path                         | Auth       | Description                                               |
| ------ | ---------------------------- | ---------- | --------------------------------------------------------- |
| GET    | `/admin/settings`            | Superadmin | Admin panel: instance settings + invitation management    |
| POST   | `/api/admin/settings`        | Superadmin | Toggle a setting (`key`, `value` form fields; HTMX-aware) |
| POST   | `/api/admin/invitations`     | Superadmin | Create invitation (`email` form field; HTMX-aware)        |
| DELETE | `/api/admin/invitations/:id` | Superadmin | Delete an invitation (HTMX-aware)                         |

## Setup & Invitations

| Method | Path             | Auth | Description                                         |
| ------ | ---------------- | ---- | --------------------------------------------------- |
| GET    | `/setup`         | --   | First-run wizard (redirects to `/` when setup done) |
| POST   | `/setup`         | --   | Submit setup form (creates superadmin, sets cookie) |
| GET    | `/invite/:token` | --   | Invitation acceptance form                          |
| POST   | `/invite/:token` | --   | Accept invitation (creates user, sets auth cookie)  |

## Git Operations (HTTP Smart Protocol)

| Method | Path                                              | Auth      | Description               |
| ------ | ------------------------------------------------- | --------- | ------------------------- |
| GET    | `/:owner/:repo/info/refs?service=git-upload-pack` | Depends\* | List refs (clone/fetch)   |
| POST   | `/:owner/:repo/git-upload-pack`                   | Depends\* | Upload pack (clone/fetch) |
| POST   | `/:owner/:repo/git-receive-pack`                  | Depends\* | Receive pack (push)       |

\*Public repos: no auth for clone/fetch; push always requires auth. Private repos: requires HTTP Basic Auth or JWT cookie for all operations. Push requires write permission (owner or `writer`/`admin` role). Unauthenticated receive-pack requests receive `401` with `WWW-Authenticate: Basic` challenge. Both HTTP and SSH pushes fire `push` webhooks.

## HTMX Fragments

| Method | Path                                              | Description                                  |
| ------ | ------------------------------------------------- | -------------------------------------------- |
| GET    | `/fragments/:owner/:repo/issues/:number/comments` | Returns rendered HTML fragment for HTMX swap |

## Auth Levels Reference

| Level      | Meaning                                                                               |
| ---------- | ------------------------------------------------------------------------------------- |
| --         | No authentication required                                                            |
| Optional   | Unauthenticated OK; authenticated users may see more data                             |
| Required   | Must be authenticated                                                                 |
| CanWrite   | Authenticated + write permission (owner, org owner, or `writer`/`admin` collaborator) |
| CanManage  | Authenticated + manage permission (owner, org owner, or `admin` collaborator)         |
| IsOwner    | Authenticated + repo owner or org owner only                                          |
| Superadmin | Instance superadmin only                                                              |
