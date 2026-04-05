# API Reference

All JSON endpoints are under `/api/`. Authentication uses a JWT in an httpOnly cookie (`cz_token`) or an `Authorization: Bearer <token>` header. Personal access tokens (`czp_...`) are also accepted in the `Authorization` header.

---

## Auth

| Method | Path                    | Auth | Description                                                     |
| ------ | ----------------------- | ---- | --------------------------------------------------------------- |
| POST   | `/api/auth/login`       | --   | Login; sets `cz_token` cookie and returns token in body         |
| POST   | `/api/auth/logout`      | --   | Clears the auth cookie                                          |
| GET    | `/auth/google`          | --   | Begin Google OAuth flow (redirects to Google)                   |
| GET    | `/auth/google/callback` | --   | Google OAuth callback; sets `cz_token` cookie, redirects to `/` |
| POST   | `/auth/ldap`            | --   | LDAP login (username + password)                                |
| GET    | `/auth/saml`            | --   | Initiate SAML SSO flow (redirects to IdP)                       |
| POST   | `/auth/saml/callback`   | --   | SAML assertion consumer service (ACS) callback                  |
| GET    | `/auth/saml/metadata`   | --   | SAML SP metadata XML                                            |

## Two-Factor Authentication (TOTP)

| Method | Path                     | Auth     | Description                                      |
| ------ | ------------------------ | -------- | ------------------------------------------------ |
| GET    | `/auth/2fa`              | --       | TOTP verification page (reads `cz_totp_pending`) |
| POST   | `/auth/2fa/verify`       | --       | Verify TOTP code or backup code                  |
| POST   | `/api/user/totp/enable`  | Required | Enable TOTP (submit code to confirm setup)       |
| POST   | `/api/user/totp/disable` | Required | Disable TOTP                                     |

## SSH Keys

| Method | Path                 | Auth     | Description                          |
| ------ | -------------------- | -------- | ------------------------------------ |
| GET    | `/api/user/keys`     | Required | List SSH keys for authenticated user |
| POST   | `/api/user/keys`     | Required | Add a new SSH public key             |
| DELETE | `/api/user/keys/:id` | Required | Delete an SSH key by ID              |

## Personal Access Tokens

| Method | Path                   | Auth     | Description                                                                                 |
| ------ | ---------------------- | -------- | ------------------------------------------------------------------------------------------- |
| GET    | `/settings/tokens`     | Required | Page: list tokens + create form; shows raw token once via `?new_token=...` after creation   |
| POST   | `/api/user/tokens`     | Required | Create PAT (`name`, `scopes[]`, optional `expires_at`); returns `{"token": "czp_..."}` once |
| DELETE | `/api/user/tokens/:id` | Required | Revoke a PAT by ID                                                                          |

Raw token format: `czp_<32-byte hex>`. Use as `Authorization: Bearer czp_<token>`. Only the SHA-256 hash is stored; the raw value cannot be recovered after creation.

## Deploy Keys

| Method | Path                               | Auth      | Description                                         |
| ------ | ---------------------------------- | --------- | --------------------------------------------------- |
| GET    | `/api/repos/:owner/:repo/keys`     | CanManage | List deploy keys for a repository                   |
| POST   | `/api/repos/:owner/:repo/keys`     | CanManage | Add deploy key (`title`, `public_key`, `read_only`) |
| DELETE | `/api/repos/:owner/:repo/keys/:id` | CanManage | Delete a deploy key by ID                           |

Deploy keys authenticate via SSH using the key's MD5 fingerprint. A `read_only` key cannot push; a read-write key can. Each key is scoped to a single repository.

## Users

| Method | Path                         | Auth | Description              |
| ------ | ---------------------------- | ---- | ------------------------ |
| GET    | `/api/users/:username`       | --   | Get user profile         |
| GET    | `/api/users/:username/repos` | --   | List user's repositories |

## Repositories

| Method | Path                                | Auth     | Description                              |
| ------ | ----------------------------------- | -------- | ---------------------------------------- |
| GET    | `/api/repos/`                       | --       | List all repositories                    |
| POST   | `/api/repos/`                       | Required | Create a repository                      |
| GET    | `/api/repos/:owner/:repo`           | --       | Get repository details                   |
| POST   | `/api/repos/:owner/:repo/fork`      | Required | Fork into authenticated user's namespace |
| POST   | `/api/repos/:owner/:repo/transfer`  | IsOwner  | Transfer repo to another user            |
| POST   | `/api/repos/:owner/:repo/restore`   | IsOwner  | Restore a soft-deleted repository        |
| POST   | `/api/repos/:owner/:repo/archive`   | IsOwner  | Archive a repository                     |
| POST   | `/api/repos/:owner/:repo/unarchive` | IsOwner  | Unarchive a repository                   |
| PATCH  | `/api/repos/:owner/:repo/template`  | IsOwner  | Toggle repository template flag          |
| POST   | `/api/repos/from-template`          | Required | Create a new repo from a template        |

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

Wiki pages are stored as files in a bare git repository (`<repo>.wiki.git`). Page content is Markdown.

## Discussions

| Method | Path                                                      | Auth     | Description                         |
| ------ | --------------------------------------------------------- | -------- | ----------------------------------- |
| POST   | `/api/repos/:owner/:repo/discussions`                     | Required | Create a discussion                 |
| POST   | `/api/repos/:owner/:repo/discussions/:number/replies`     | Required | Reply to a discussion               |
| PATCH  | `/api/repos/:owner/:repo/discussions/:number`             | Required | Mark a reply as the accepted answer |
| DELETE | `/api/repos/:owner/:repo/discussions/:number/replies/:id` | Required | Delete a reply                      |
| POST   | `/api/repos/:owner/:repo/discussions/categories`          | Required | Create a discussion category        |
| DELETE | `/api/repos/:owner/:repo/discussions/categories/:id`      | Required | Delete a discussion category        |

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

| Method | Path                               | Auth     | Description                                                                         |
| ------ | ---------------------------------- | -------- | ----------------------------------------------------------------------------------- |
| POST   | `/api/orgs/`                       | Required | Create organization                                                                 |
| GET    | `/api/orgs/:org`                   | --       | Get organization by name                                                            |
| GET    | `/api/orgs/:org/members`           | --       | List organization members                                                           |
| POST   | `/api/orgs/:org/members`           | Required | Add member (`username`, `role`); owner only                                         |
| DELETE | `/api/orgs/:org/members/:username` | Required | Remove member; owner only; last owner blocked                                       |
| POST   | `/api/orgs/:org/repos`             | Required | Create a repository under the organization; owner only                              |
| POST   | `/api/orgs/:org/transfer`          | Required | Transfer org ownership (`new_owner` form field); owner only; demotes self to member |

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

| Method | Path                            | Auth     | Description                         |
| ------ | ------------------------------- | -------- | ----------------------------------- |
| GET    | `/oauth/authorize`              | Optional | OAuth authorization page            |
| POST   | `/oauth/authorize`              | Required | Confirm authorization grant         |
| POST   | `/oauth/token`                  | --       | Exchange auth code for access token |
| POST   | `/api/oauth/apps`               | Required | Register an OAuth application       |
| DELETE | `/api/oauth/apps/:id`           | Required | Delete an OAuth application         |
| DELETE | `/api/oauth/authorizations/:id` | Required | Revoke an OAuth authorization       |

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
