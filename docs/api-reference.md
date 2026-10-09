# API Reference

All JSON endpoints are under `/api/`. Authentication uses a JWT in an httpOnly cookie (`cz_token`) or an `Authorization: Bearer <token>` header. Personal access tokens (`czp_...`) and OAuth-app tokens are also accepted in the `Authorization` header; both are limited to the routes their scopes admit (see [Token Scopes](./access-control.md#token-scopes)).

## Rate limits

Every request except static assets counts against a budget per hour by default ([configuration](./configuration.md#rate-limits)). A signed-in user's browser sessions share one bucket, and all of their personal access tokens and OAuth-app tokens share another, so a busy CI token can't lock its owner out of the web UI. Requests without a valid credential count per IPv4 address or IPv6 /64, and so do requests made with a token bound to a signing key, so a leaked one can't spend its owner's budget. Each bucket has a separate budget for each resource:

| Resource  | Requests                                                                       | Signed in | Anonymous |
| --------- | ------------------------------------------------------------------------------ | --------- | --------- |
| `core`    | everything not listed below, including `POST /api/repos/{owner}/{repo}/archive` | 5000      | 1000      |
| `git`     | `GET …/info/refs`, `POST …/git-upload-pack`, `POST …/git-receive-pack`          | 1000      | 200       |
| `archive` | `GET /{owner}/{repo}/archive/…`                                                | 100       | 20        |
| `search`  | `GET /search`, `GET /search/code`                                              | 600       | 60        |

Counted responses carry:

| Header                  | Value                                         |
| ----------------------- | --------------------------------------------- |
| `X-RateLimit-Limit`     | The budget for this window                    |
| `X-RateLimit-Remaining` | Requests left in this window                  |
| `X-RateLimit-Reset`     | When the window resets, in Unix seconds       |
| `X-RateLimit-Resource`  | `core`, `git`, `archive` or `search`          |

Over budget, the response is `429` with `Retry-After` in seconds. `/api/*` answers `{"error":"rate limit exceeded"}`; git and pages answer plain text. An unlimited resource (budget `0`) sends no rate-limit headers. Sign-up and password login also have their own per-IP limits.

---

## Auth

| Method | Path                    | Auth | Description                                                                                                                                                                                                                    |
| ------ | ----------------------- | ---- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| POST   | `/api/auth/login`       | --   | Login (JSON `email`, `password`); sets `cz_token` cookie and returns token in body. A user with TOTP on gets `401 {"error":"totp_required"}` and must use a PAT ([details](./access-control.md#two-factor-authentication)). A suspended account gets `403 {"error":"account_suspended"}` once its password checks out     |
| POST   | `/api/auth/logout`      | --   | Clears auth cookie; 204, or form 303/HTMX `HX-Redirect` to `/`                                                                                                                                                                 |
| GET    | `/auth/google`          | --   | Begin Google OAuth flow (redirects to Google)                                                                                                                                                                                  |
| GET    | `/auth/google/callback` | --   | Google OAuth callback; sets `cz_token` cookie, redirects to `/`; links an existing account only when both Google and the account have verified the email; re-renders login with 403 if Google hasn't verified the email, 409 if the matching account's email is unverified or it is linked to another Google account; redirects to `/auth/2fa` when TOTP is on ([details](./access-control.md#google-oauth-sign-in)) |
| POST   | `/auth/ldap`            | --   | LDAP login (username + password)                                                                                                                                                                                               |
| GET    | `/auth/saml`            | --   | Initiate SAML SSO flow (redirects to IdP)                                                                                                                                                                                      |
| POST   | `/auth/saml/callback`   | --   | SAML assertion consumer service (ACS) callback                                                                                                                                                                                 |
| GET    | `/auth/saml/metadata`   | --   | SAML SP metadata XML                                                                                                                                                                                                           |

When the `oauth_link_state` cookie matches `state`, `/auth/google/callback` finishes [connecting Google](#connected-accounts) to the signed-in account instead: it never signs in, and redirects to `/settings#connected-accounts`.

## Confirmed actions

Actions that give lasting access take the account's `password`, plus `code` (the TOTP code) when 2FA is on, as form fields or in the JSON body. An account with neither sends its directory password (LDAP), the code a fresh sign-in left in the `cz_reauth` cookie (`POST /settings/reauth/{provider}`), or `email_code` from `POST /settings/confirm-code`. A wrong confirmation gets 403, and five wrong ones in 15 minutes get 429. A personal access token created with `repo:admin` skips it for repository and organization administration, never for changes to the account. The full list is in [access control](./access-control.md#confirming-sensitive-actions).

## Signed requests

A token bound to a signing key (every `repo:admin` token) works only for requests signed with the key's private half. A `repo:admin` token also works only on its targets; any other repository or organization gets `403 {"error":"this token isn't allowed for that repository or organization"}`. Anything else gets `401 {"error":"this token needs each request signed with its key"}`. Each request carries three headers besides `Authorization: Bearer czp_…`:

- `X-Cloudzilla-Timestamp`: Unix seconds, within 5 minutes of the server's clock.
- `X-Cloudzilla-Nonce`: 16 to 64 characters from `A-Z a-z 0-9 - _`, never reused with the token.
- `X-Cloudzilla-Signature`: an `ssh-keygen -Y sign -n cloudzilla-api` signature of the message below, without its `BEGIN`/`END` lines and line breaks.

The message is six lines, without a trailing newline: `cloudzilla-request-v1`, the method, the path and query as sent, the timestamp, the nonce, and the hex SHA-256 of the body (of nothing for a request without one). Bodies over 1 MiB are refused. With a hardware key, `ssh-keygen` asks for a touch for each request unless the token's key was registered with `no-touch-required`. A shell helper:

```sh
#!/bin/sh
# cz-signed METHOD PATH [BODY_FILE] [CONTENT_TYPE]
# Needs CZ_URL, CZ_TOKEN and CZ_KEY (the private key file).
set -eu
method=$1 path=$2 body=${3:-/dev/null} type=${4:-application/x-www-form-urlencoded}
ts=$(date +%s)
nonce=$(od -An -N16 -tx1 /dev/urandom | tr -d ' \n')
sum=$( (sha256sum 2>/dev/null || shasum -a 256) < "$body" | cut -d' ' -f1)
msg=$(mktemp)
trap 'rm -f "$msg" "$msg.sig"' EXIT
printf 'cloudzilla-request-v1\n%s\n%s\n%s\n%s\n%s' "$method" "$path" "$ts" "$nonce" "$sum" > "$msg"
ssh-keygen -Y sign -q -f "$CZ_KEY" -n cloudzilla-api "$msg"
sig=$(sed '1d;$d' "$msg.sig" | tr -d '\n')
curl -sS -X "$method" "$CZ_URL$path" \
  -H "Authorization: Bearer $CZ_TOKEN" -H "Content-Type: $type" \
  -H "X-Cloudzilla-Timestamp: $ts" -H "X-Cloudzilla-Nonce: $nonce" -H "X-Cloudzilla-Signature: $sig" \
  --data-binary @"$body"
```

For example, `printf 'username=bob&role=writer' > b; cz-signed POST /api/repos/acme/app/collaborators b`.

## Two-Factor Authentication (TOTP)

| Method | Path                     | Auth     | Description                                      |
| ------ | ------------------------ | -------- | ------------------------------------------------ |
| GET    | `/auth/2fa`              | --       | TOTP verification page (reads `cz_totp_pending`) |
| POST   | `/auth/2fa/verify`       | --       | Verify TOTP code or backup code; five wrong ones in 15 minutes refuse even the right one for the rest of the window |
| POST   | `/api/user/totp/enable`  | Required | Enable TOTP (`secret`, `code` from the new authenticator, and `password`); 303 to `/settings?profile_error=reauth_failed#security` on a wrong password |
| POST   | `/api/user/totp/disable` | Required | Disable TOTP (`code` and `password`)             |
| POST   | `/settings/confirm-code` | Required | Email a one-time `email_code` to an account with no password or 2FA ([confirming actions](./access-control.md#confirming-sensitive-actions)); one a minute and five an hour (429), 409 for accounts that confirm another way. HTMX gets the status text with a 200 |
| POST   | `/settings/reauth/{provider}` | Required | `google` or `saml`: sign in there again to confirm a change (`return_to` form field). Redirects to the provider; its callback sets the one-time `cz_reauth` cookie and returns to `return_to` |
| POST   | `/settings/password`     | Required | Change the password (`password`, `code` with 2FA, `new_password`, `new_password_confirm`); ends every session and sets a fresh cookie; 303 to `/settings?password_changed=1#password`, or `?password_error=<code>#password` |
| POST   | `/settings/sessions/revoke` | Required | Sign out every other session: ends all session JWTs issued before and sets a fresh cookie for this browser; 303 to `/settings?sessions_revoked=1#sessions` |

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
| POST   | `/api/user/keys`     | Required | Add a new SSH public key (`title`, `public_key`, plus `password` and, with 2FA, `code`; form or JSON). 403 on a wrong confirmation, 429 when throttled |
| DELETE | `/api/user/keys/:id` | Required | Delete an SSH key by ID              |

## Personal Access Tokens

| Method | Path                   | Auth     | Description                                                                                                                                                        |
| ------ | ---------------------- | -------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| POST   | `/api/user/tokens`     | Required | Create PAT (`name`, repeated `scopes` from `repo:read`, `repo:write`, `issues:write`, `pulls:write` and `repo:admin` — the last needs `signing_key`, `targets` and `expires_at` within 90 days — optional `expires_at`, optional `signing_key` (an SSH public key, hardware `sk-` keys included; see [signed requests](#signed-requests)), `targets` (`repo:admin` only: `owner/repo` or `org`, one per line or comma-separated, each one you administer), plus `password` and, with 2FA, `code` form fields); redirects to `/settings#tokens`, which shows the raw token once, via an HttpOnly cookie, or to `/settings?profile_error=reauth_failed#tokens` |
| DELETE | `/api/user/tokens/:id` | Required | Revoke a PAT by ID                                                                                                                                                 |

Raw token format: `czp_<32-byte hex>`. Use as `Authorization: Bearer czp_<token>`, or as the HTTP Basic password for git. Only the SHA-256 hash is stored; the raw value cannot be recovered after creation.

Scopes are those of [OAuth apps](#oauth-apps), with the same meaning and the same open routes. Creating a token without a scope, or with an unknown one, gets `400`. A PAT never reaches account, admin or repo-administration endpoints; a request outside its scopes gets `403 {"error":"insufficient_scope"}`, or a plain-text `403` from git over HTTP Basic.

## Commit Email Privacy

| Method | Path              | Auth     | Description                                                                                             |
| ------ | ----------------- | -------- | ------------------------------------------------------------------------------------------------------- |
| POST   | `/settings/email` | Required | Form: `keep_email_private=on` turns the setting on, omitting it turns it off. Redirects to `/settings#email` |

Commits made through the web UI (new files, wiki edits, merge and squash merges, applied suggestions) are authored as `<username> <email>`, where the email comes from `UserService.CommitAuthor`:

- Setting on (the default for every user), or no account email: `<user id>+<username>@users.noreply.<host>`, with `<host>` taken from `server.base_url`.
- Setting off: the account email.

Commits pushed over git keep whatever author the client set. Contributor stats resolve a noreply author back to its user when both the id and username match, under any host, so commits made before a `base_url` change stay credited. Legacy `username@localhost` authors are not resolved.

## Email Verification

| Method | Path                                   | Auth     | Description                                                                                                                                                  |
| ------ | -------------------------------------- | -------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| POST   | `/settings/email/resend-verification`  | Required | Issue a new verification link for the account's address; the email goes out in the background. HTMX: 204, or JSON `error` with 429 (a link went to the user or the address in the last minute), 409 (already verified), 503 (no SMTP), 500. Form: 303 to `/settings#email`, or to `/settings?profile_error=<code>#email` on failure |
| GET    | `/verify-email?token=…`                | --       | Check a link and show a confirm button naming the account; spends nothing. 410 when expired, 400 when invalid                                              |
| POST   | `/verify-email`                        | --       | Form `token`: verify the address the link was sent to. 200 on success, 410 when expired, 400 when invalid or used                                          |

See [Email Verification](./access-control.md#email-verification).

## Deploy Keys

| Method | Path                               | Auth      | Description                                         |
| ------ | ---------------------------------- | --------- | --------------------------------------------------- |
| GET    | `/api/repos/:owner/:repo/keys`     | CanManage | List deploy keys for a repository                   |
| POST   | `/api/repos/:owner/:repo/keys`     | CanManage | Add deploy key (`title`, `public_key`, `read_only`, plus `password` and, with 2FA, `code`; 403 on a wrong confirmation, 429 when throttled) |
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

Public user objects — returned by `GET /api/users/:username` and by `/api/repos/:owner/:repo/stargazers` (JSON only with `HX-Request: true`; otherwise it renders the stargazers page) — contain only `id`, `username`, `bio`, `avatar_url`, and `created_at`. Email addresses and notification preferences are never returned. `avatar_url` is the absolute URL of the uploaded avatar (`<server.base_url>/avatars/...`) when the user has one, and otherwise the picture stored at Google sign-up, or `""`. `GET /api/orgs/:org` fills `avatar_url` the same way.

## Repositories

| Method | Path                                | Auth     | Description                                                                         |
| ------ | ----------------------------------- | -------- | ----------------------------------------------------------------------------------- |
| GET    | `/api/repos/`                       | --       | List the repositories the caller can read; anonymous callers get public ones only   |
| POST   | `/api/repos/`                       | Required | Create a repository (`name`, `description`, `private`, plus the init options below) |
| GET    | `/api/repos/:owner/:repo`           | --       | Get repository details                                                              |
| POST   | `/api/repos/:owner/:repo/fork`      | Required | Fork the repository (see [Forks](#forks))                                           |
| POST   | `/api/repos/:owner/:repo/transfer`  | IsOwner  | Transfer repo to a user or an org (`new_owner`, plus `password` and, with 2FA, `code`; 403 on a wrong confirmation, 429 when throttled); an org target must be one you own. Another user must accept first (see [Repository Transfers](#repository-transfers)) |
| DELETE | `/api/repos/:owner/:repo/transfer`  | IsOwner  | Cancel the repo's pending transfer; 204, or 404 when none is pending                |
| POST   | `/api/repos/:owner/:repo/restore`   | IsOwner  | Restore a soft-deleted repository                                                   |
| POST   | `/api/repos/:owner/:repo/archive`   | IsOwner  | Archive a repository                                                                |
| POST   | `/api/repos/:owner/:repo/unarchive` | IsOwner  | Unarchive a repository                                                              |
| PATCH  | `/api/repos/:owner/:repo/template`  | IsOwner  | Toggle repository template flag                                                     |
| POST   | `/api/repos/from-template`          | Required | Create a new repo from a template                                                   |

Repository creation (here and under `/api/orgs/:org/repos`) accepts optional init options that seed an initial commit: `add_readme` (bool), `gitignore` (`Go`, `Node`, `Python`, `Rust`, `Java`, `C++`, `Ruby`), and `license` (`mit`, `apache-2.0`, `gpl-3.0`, `bsd-3-clause`, `unlicense`). An unknown template name leaves the repository empty rather than failing the request.

Creating a repository (here, under `/api/orgs/:org/repos`, or from a template) with a name already used in that namespace returns 422 `a repository with that name already exists` and creates nothing. A directory left on disk without a repository row counts as used, so a new repository never takes over an earlier holder's data; a fork skips such names the same way it skips existing repositories. `POST /api/repos/from-template` checks the name with the same rules as create and returns 422 `invalid repository name: ...` for one it rejects.

A transfer into an organization you own, or into your own account (an org repo you own), happens at once: the response redirects to the repository's new URL (`HX-Redirect` for HTMX). It is refused with 422 `transfer failed`, and nothing moves, when the new owner already has a repository with that name, or a `<name>.git` or `<name>.wiki.git` directory left on disk under it. It is refused the same way when `new_owner` names neither a user nor an org, names an org the requester does not own, or names the current owner.

## Repository Imports

| Method | Path                | Auth     | Description |
| ------ | ------------------- | -------- | ----------- |
| POST   | `/api/imports`      | Required | Start importing a Git repository (`clone_url`, `name`, optional `owner`, `description`, `private`, `auth_username` + `auth_token`, `mirror`, `mirror_interval`). Returns `202 {id, status, owner, name, status_url}` |
| GET    | `/api/imports/:id`  | Required | The import's `status` (`queued`, `running`, `done`, `failed`), `progress` and `error`. 404 for an unknown, expired or someone else's import |

`clone_url` must be `http://` or `https://`, at most 2048 bytes, with no query string (422); credentials in it are refused too (422). `auth_username` and `auth_token` go together (422). `owner` defaults to the caller; another owner must be an organization the caller owns (403). A name already used under the owner is 422 `a repository with that name already exists`. A sixth import while five are queued or running is 429. Personal access tokens and OAuth-app tokens are refused on both `/api/imports` endpoints (403 `insufficient_scope`); use a session. The import's owner is in the request body, so a token limited to particular repositories or organizations could not be confined to them. `status_url` in the response is the HTML status page (`/repos/import/{id}`); API clients poll `GET /api/imports/{id}` for JSON. Imports run in the background; see [repo-import](./repo-import.md).

`"mirror": true` makes the new repository a pull mirror that keeps syncing from `clone_url`. `mirror_interval` is a Go duration (`"8h"`), from `mirror.min_interval` to 30 days; it defaults to `mirror.default_interval`. A mirror with `auth_token` stores the token sealed, and needs `security.secret_key`. All of these are 422 when refused, as is `mirror` while `mirror.enabled` is off. See [repo-mirrors](./repo-mirrors.md).

## Repository Mirrors

| Method | Path | Auth | Description |
|--------|------|------|-------------|
| GET    | `/api/repos/:owner/:repo/mirror`      | Manage | `{remote_url, auth_username, has_token, interval, next_sync_at, last_sync_at, last_success_at, last_error, consecutive_failures}`. Never the token |
| PATCH  | `/api/repos/:owner/:repo/mirror`      | Manage | Change any of `remote_url`, `auth_username`, `auth_token`, `interval`; `clear_token: true` removes the token. An empty `auth_token` keeps the stored one. Returns the mirror |
| DELETE | `/api/repos/:owner/:repo/mirror`      | Manage | Stop mirroring: the repository becomes writable and the token is deleted. 204, or 409 while a sync runs |
| POST   | `/api/repos/:owner/:repo/mirror/sync` | Write  | Sync now. 202 `{status: "queued"}`, or 409 while mirroring is off or the mirror is archived |

A repository that isn't a mirror is 404. Invalid values are 422, with the same rules as on import. Scoped tokens need `repo:admin` for every mirror endpoint. A new URL, username or token makes the mirror sync at once; a new interval counts from the last sync. A pull mirror refuses pushes and every write to its branches, tags and default branch, and `POST …/pulls` into it is 422.

## Repository Transfers

A transfer to another user moves nothing until they accept it. `POST /api/repos/:owner/:repo/transfer` answers `202` with the pending transfer, notifies the recipient (notification type `repo_transfer`, emailed like other notifications), and lists it on their `/repos/transfers` page, which also shows the repository's collaborators, who keep their access if it is accepted. A repository has at most one pending transfer: a new one replaces it, and so does any move of the repository. A transfer expires after 7 days (`service.RepoTransferTTL`), and lapses if the requester stops owning the repository.

```json
{"id": 12, "repo_id": 40, "owner": "alice", "repo": "tools", "description": "", "private": true,
 "requester_id": 3, "requester": "alice", "recipient_id": 9, "recipient": "bob",
 "expires_at": "2026-10-08T12:00:00Z", "created_at": "2026-10-01T12:00:00Z"}
```

| Method | Path                               | Auth     | Description                                                                                                         |
| ------ | ---------------------------------- | -------- | ------------------------------------------------------------------------------------------------------------------- |
| GET    | `/api/user/transfers`              | Required | Transfers offered to you that you can still accept                                                                  |
| POST   | `/api/user/transfers/:id/accept`   | Required | Accept (`repo`: the `owner/repo` you were offered); 200 with the repository, now yours (`HX-Redirect` to it for HTMX) |
| POST   | `/api/user/transfers/:id/decline`  | Required | Decline; 204 (`HX-Refresh` for HTMX)                                                                                |
| DELETE | `/api/repos/:owner/:repo/transfer` | IsOwner  | Cancel your repository's pending transfer; 204 (`HX-Refresh` for HTMX)                                              |

Accepting or declining a transfer that isn't yours, has ended or has expired returns 404 `repository transfer not found or expired`. Accepting returns 409 when `repo` no longer matches the repository's `owner/name`, so a repository renamed or moved after you saw it is never accepted under its new name. It also returns 409, and the transfer stays pending, when you already have a repository with that name or a `<name>.git` or `<name>.wiki.git` directory left on disk: free the name, then accept again. Those name checks run only on accept, since refusing the request would tell the requester whether you hold a private repository of that name.

Repository JSON omits `owner_id` for an org repo, whose owner is `org_id`. `created_by`, when present, records who created the repo and grants no access.

Names ending in `.wiki` (in any case) are reserved, because `<name>.wiki.git` is the wiki of repository `<name>`: creating one returns 422 `invalid repository name: names ending in .wiki are reserved for wikis`, and a fork of such a repository gets a `-1` suffix. A `<name>.wiki` repository created before the reservation still works, but while it exists, even soft-deleted, `<name>` cannot be created in or transferred into its namespace (422 `a repository with that name already exists` on create, 409 on accepting a transfer), it cannot be transferred into a namespace that holds `<name>`, and an existing `<name>` has no wiki.

Deleting a repository moves its directories to `<name>.git.deleted.<unix_ts>` and `<name>.wiki.git.deleted.<unix_ts>`, with the same second stored in `deleted_at`. Restore and the 30-day purge act only on the copy whose suffix matches the row, never on another soft-deleted repository of the same name (a soft-deleted org repo does not hold its name, so several can exist). Restore returns 422 `a repository with that name already exists` while another repository, or a directory left on disk, holds the name. It returns 500 `restore failed`, and leaves the row deleted, when the row's copy is missing from disk. Deletes made before wikis moved with their repository left `<name>.wiki.git` in place; the purge removes such a wiki once no repository row, live or soft-deleted, names it, so the name can be reused.

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

| Method | Path                           | Auth     | Description                     |
| ------ | ------------------------------ | -------- | ------------------------------- |
| POST   | `/api/repos/:owner/:repo/fork` | Required | Fork the repository; see below  |

The body is optional JSON (`Content-Type: application/json`); any other body forks with the defaults.

| Field                 | Default           | Meaning                                                                 |
| --------------------- | ----------------- | ----------------------------------------------------------------------- |
| `owner`               | the caller        | The caller's username or an organization they own                       |
| `name`                | the source's name | With no name, a taken one gets `-1`, `-2`, …; a named fork fails instead |
| `description`         | the source's      | `""` clears it                                                          |
| `default_branch_only` | `false`           | Copy only the default branch; tags are always copied                    |

A fork keeps the source's visibility. Malformed JSON is a 400 `invalid request body`, and a source the caller can't read is a 404. A fork can't land in the account or organization that owns the source (422 `a repository can't be forked into the account or organization that owns it`). An organization the caller doesn't own is a 403 (`you can fork only into your account or an organization you own`). A taken or invalid name is a 422, as for create.

With `default_branch_only`, the fork's HEAD points at the copied branch. A source whose default branch setting names no branch is a 422 `the default branch doesn't exist, so it can't be the only branch copied`, and nothing is created; a source with no commits forks fine.

A JSON request gets 201 `{"owner", "name", "url"}`. Other requests are redirected to the fork, or get `HX-Redirect` from htmx. A token limited to targets must name the source repository, or the organization that owns it, because the path is checked against its targets first. It may then fork into the caller's account, or into an organization among its targets (otherwise 403 `this token isn't allowed for that repository or organization`).

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
| POST   | `/api/repos/:owner/:repo/pulls/:number/reviews` | Required | Submit or update a review (`state`, `body`); upserts per reviewer; `state=changes_requested` blocks all merge buttons until the reviewer changes it or deletes their account |

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

When `diff_side` is `right` (the default, and what the Files tab posts), `line` is a line number in the head branch's version of `path`; applying a suggestion replaces that line. When it is `left`, `line` is a line number in the base side of the Files tab's diff, the version of `path` at the branches' merge base, and applying returns `422`. Any other `diff_side` is rejected with `422`.

The Files tab shows each side's comments as a thread of their own, under the row that displays the line: a `right` thread under the added or context line with that head-file number, a `left` thread under the deleted or context line with that base-file number. A comment on a line outside the diff's hunks doesn't show there.

Applying a suggestion, merging a PR, and editing the wiki return `409` when a push moved the branch while the change was being committed; the push is kept and the client should reload and retry (see [pr-merge](./pr-merge.md#merge-flow)).

## Reactions

| Method | Path                                             | Auth     | Description                          |
| ------ | ------------------------------------------------ | -------- | ------------------------------------ |
| GET    | `/api/repos/:owner/:repo/comments/:id/reactions` | Optional | List reactions on a comment          |
| POST   | `/api/repos/:owner/:repo/comments/:id/reactions` | Required | Toggle a reaction emoji on a comment |

## Branch Protections

| Method | Path                                               | Auth      | Description                                                                                                                  |
| ------ | -------------------------------------------------- | --------- | ---------------------------------------------------------------------------------------------------------------------------- |
| GET    | `/api/repos/:owner/:repo/branches/protections`     | CanManage | List all branch protection rules for the repository                                                                          |
| POST   | `/api/repos/:owner/:repo/branches/protections`     | CanManage | Create a rule (`pattern`, `require_review_count`, `require_status_checks[]`, `block_force_push`, `require_pull_request`)     |
| PATCH  | `/api/repos/:owner/:repo/branches/protections/:id` | CanManage | Update an existing rule (same fields as POST except `pattern`; a field left out is reset, so `require_pull_request` turns off) |
| DELETE | `/api/repos/:owner/:repo/branches/protections/:id` | CanManage | Delete a protection rule                                                                                                     |

`require_pull_request` (default `false`) refuses every direct update of a matching branch, with no bypass for admins: a push (create, update or delete), a branch create or delete through the API, and a file created, edited, renamed or deleted from the browser. Changes reach the branch only by merging a pull request. The refusal is `422 pull request required by branch protection: rule "<pattern>"`; over git it is the per-ref status of the push ([git-transport](./git-transport.md#concurrent-ref-updates)).

## Branches & Tags

| Method | Path                                      | Auth     | Description                                                                                                     |
| ------ | ----------------------------------------- | -------- | --------------------------------------------------------------------------------------------------------------- |
| POST   | `/api/repos/:owner/:repo/branches`        | CanWrite | Create branch (`name`, `from` form fields; `from` defaults to default branch; 422 under `require_pull_request`) |
| DELETE | `/api/repos/:owner/:repo/branches?name=X` | CanWrite | Delete branch (400 for the default branch; 422 under `block_force_push` or `require_pull_request`)              |
| POST   | `/api/repos/:owner/:repo/tags`            | CanWrite | Create tag (`name`, `from` form fields)                                                                         |
| DELETE | `/api/repos/:owner/:repo/tags?name=X`     | CanWrite | Delete tag                                                                                                      |

All four endpoints require write access. For HTMX requests they return an HTML fragment; otherwise JSON.

Deleting a branch whose protection rule has `block_force_push` returns 422 `cannot delete a branch whose protection rule blocks force pushes`, as JSON for HTMX requests too, because deleting and pushing again is a force push in two steps. `git push --delete` is refused the same way ([git-transport](./git-transport.md#concurrent-ref-updates)).

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

The repository's Checks tab (`/{owner}/{repo}/checks`) lists the commits with reported statuses, most recently updated first.

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
| GET    | `/search` | Optional | Full-text search. Query params: `q` (search term), `type` (`all`, `repos`, `issues`, `pulls`, `users`). Returns only repos, issues and PRs the viewer can read, never from soft-deleted repos. |

## Webhooks

| Method | Path                                           | Auth      | Description                                |
| ------ | ---------------------------------------------- | --------- | ------------------------------------------ |
| GET    | `/api/repos/:owner/:repo/hooks/`               | --        | List webhooks for repository               |
| POST   | `/api/repos/:owner/:repo/hooks/`               | CanManage | Create webhook (JSON `url`, `secret`, `events`, plus `password` and, with 2FA, `code`; 400 for a URL that isn't http(s) or resolves to a private address, 403 on a wrong confirmation, 429 when throttled) |
| PATCH  | `/api/repos/:owner/:repo/hooks/:id`            | CanManage | Update webhook settings                    |
| DELETE | `/api/repos/:owner/:repo/hooks/:id`            | CanManage | Delete webhook                             |
| GET    | `/api/repos/:owner/:repo/hooks/:id/deliveries` | CanManage | List delivery history                      |
| POST   | `/api/repos/:owner/:repo/hooks/:id/redeliver`  | CanManage | Redeliver a webhook event                  |

Webhooks fire on `push`, `issues`, and `pull_request` events. Requests are signed with `X-Hub-Signature-256` when a secret is configured (GitHub-compatible HMAC-SHA256).

## Repository Collaborators

| Method | Path                                              | Auth      | Description                           |
| ------ | ------------------------------------------------- | --------- | ------------------------------------- |
| GET    | `/api/repos/:owner/:repo/collaborators`           | Optional  | List collaborators with usernames     |
| POST   | `/api/repos/:owner/:repo/collaborators`           | CanManage | Add collaborator or change their role (`username`, `role` of `reader`, `writer` or `admin`, else 400, plus `password` and, with 2FA, `code`; 403 on a wrong confirmation or when a non-owner grants `admin` or changes an admin's role, 429 when throttled) |
| DELETE | `/api/repos/:owner/:repo/collaborators?user_id=N` | CanManage | Remove collaborator by user ID (403 when a non-owner removes an admin) |

See [access-control.md](access-control.md) for the full permission model. `CanManage` requires owner, org owner, or `admin` collaborator role.

## Avatars

| Method | Path | Auth | Description |
| ------ | ---- | ---- | ----------- |
| POST   | `/settings/avatar`                   | Required | Upload the caller's avatar as multipart field `avatar`: PNG, JPEG, GIF or WebP, at most 2 MB and 4096 × 4096 px. Redirects (303) to `/settings`; 413 over 2 MB; 422 for anything else that isn't such an image |
| POST   | `/settings/avatar/delete`            | Required | Remove the caller's avatar |
| POST   | `/orgs/:org/settings/avatar`         | Required | Upload the org's avatar; owner only (403), 404 for an unknown org; writes `org.avatar.update` to the audit log |
| POST   | `/orgs/:org/settings/avatar/delete`  | Required | Remove the org's avatar; owner only; writes `org.avatar.remove` |
| GET    | `/avatars/:key`                      | --       | Serve a stored avatar with `Cache-Control: public, max-age=31536000, immutable` and an `ETag` (304 on `If-None-Match`); 404 for an unknown or malformed key |

With `HX-Request: true`, upload errors come back as a message for the form's error slot instead of a 4xx. See [storage](./storage.md).

## Organizations

| Method | Path                                    | Auth     | Description                                                                                                                         |
| ------ | --------------------------------------- | -------- | ----------------------------------------------------------------------------------------------------------------------------------- |
| POST   | `/api/orgs/`                            | Required | Create organization (`name`, `display_name`, `description`); 422 when `name` is invalid or taken                                    |
| GET    | `/api/orgs/:org`                        | --       | Get organization by name, including `website`, `location`, `contact_email`, `default_repo_visibility`, `default_branch_name`        |
| GET    | `/api/orgs/:org/members`                | --       | List organization members                                                                                                           |
| POST   | `/api/orgs/:org/members`                | Required | Add member (`username`, `role`); owner only; adding an owner also needs `password` and, with 2FA, `code`                             |
| DELETE | `/api/orgs/:org/members/:username`      | Required | Remove member; owner only, except a member may remove themselves; last owner blocked                                                |
| POST   | `/api/orgs/:org/members/:username/role` | Required | Change a member's role (`role` form field: `owner` or `member`); owner only; promoting to `owner` also needs `password` and, with 2FA, `code`; demoting the last owner is rejected |
| POST   | `/api/orgs/:org/repos`                  | Required | Create a repository under the organization (same body as `POST /api/repos/`); owner only                                            |
| POST   | `/api/orgs/:org/transfer`               | Required | Transfer org ownership (`new_owner`, optional `confirm_name`, plus `password` and, with 2FA, `code`; 403 on a wrong confirmation, 429 when throttled); owner only; demotes self to member; redirects to `/:org` |
| POST   | `/api/orgs/:org/profile`                | Required | Update profile (`display_name`, `description`, `website`, `location`, `contact_email` form fields); owner only                      |
| POST   | `/api/orgs/:org/repo-defaults`          | Required | Update repo defaults (`default_repo_visibility`, `default_branch_name` form fields); owner only                                     |
| POST   | `/api/orgs/:org/delete`                 | Required | Delete the organization (`confirm_name`, plus `password` and, with 2FA, `code`); owner only; 422 while the org still owns repositories |

Org avatars are uploaded outside `/api/orgs`, whose 1 MB body limit is smaller than an image; see [Avatars](#avatars).

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
| POST   | `/oauth/authorize`              | Required | Confirm authorization grant; approving needs `password` and, with 2FA, `code` |
| POST   | `/oauth/token`                  | --       | Exchange auth code for access token        |
| POST   | `/api/oauth/apps`               | Required | Register an OAuth application (HTMX-aware) |
| DELETE | `/api/oauth/apps/:id`           | Required | Delete an OAuth application (HTMX-aware)   |
| DELETE | `/api/oauth/authorizations/:id` | Required | Revoke an OAuth authorization (HTMX-aware) |

Registering an app returns `client_secret` once; only its bcrypt hash is stored. JSON callers get it in the body; HTMX form posts (`name`, `homepage_url`, `redirect_uri`, `description`) get the apps-list fragment with the secret revealed.

`POST /api/oauth/apps` takes `name`, `homepage_url`, `description` and `redirect_uris`, and returns the app with its `client_secret`, shown once. At least one redirect URI is required, each an absolute `http`/`https` URL with no fragment or comma; anything else returns `400`.

`/oauth/authorize` takes `client_id`, `redirect_uri`, `state`, and a space-delimited `scope`. `redirect_uri` must exactly match one the app registered; if it doesn't, or a scope is unknown, the response is `400` and nothing is redirected. Apps registered before redirect URIs were required have none, so they must be registered again. The consent page can't be framed (`X-Frame-Options: DENY`, `Content-Security-Policy: frame-ancestors 'none'`). Approving needs the account's password and TOTP code (see [access-control](./access-control.md#confirming-sensitive-actions)); a wrong one re-renders the consent page with `403`, or `429` once throttled. Approving redirects to `redirect_uri` with `code` and `state` added to its query, keeping any query it already has; denying needs no confirmation and adds `error=access_denied` and `state` instead.

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

A request outside the token's scopes gets `403 {"error":"insufficient_scope"}` with a `WWW-Authenticate: Bearer error="insufficient_scope", scope="..."` header naming the scope to request. Only listed routes are open to OAuth tokens and PATs; account, admin and repo-administration endpoints and HTML pages never are. Route list: [access-control](./access-control.md#token-scopes).

## Instance Admin (superadmin only)

| Method | Path                         | Auth       | Description                                               |
| ------ | ---------------------------- | ---------- | --------------------------------------------------------- |
| GET    | `/admin/settings`            | Superadmin | Admin panel: instance settings + invitation management    |
| POST   | `/api/admin/settings`        | Superadmin | Toggle a setting (`key`, `value`, plus `password` and, with 2FA, `code`; HTMX-aware) |
| POST   | `/api/admin/invitations`     | Superadmin | Create invitation (`email`, plus `password` and, with 2FA, `code`; HTMX-aware)        |
| DELETE | `/api/admin/invitations/:id` | Superadmin | Delete an invitation (HTMX-aware)                         |
| GET    | `/admin/users`               | Superadmin | Account list (`q` username or email prefix, `role` = `superadmin`/`user`, `status` = `active`/`suspended`, `page`; 50 per page) |
| GET    | `/admin/users/:username`     | Superadmin | One account's details and actions; 404 for an unknown username or the ghost |
| POST   | `/api/admin/users/:username/suspend` | Superadmin | Suspend (optional `reason`); ends the account's sessions. 409 if it would leave no active superadmin |
| POST   | `/api/admin/users/:username/unsuspend` | Superadmin | Unsuspend; tokens and keys work again, sessions don't |
| POST   | `/api/admin/users/:username/promote` | Superadmin | Make a superadmin; 409 while suspended |
| POST   | `/api/admin/users/:username/demote` | Superadmin | Remove superadmin; 409 if it would leave no active superadmin |
| POST   | `/api/admin/users/:username/reset-2fa` | Superadmin | Turn the account's 2FA off and mail it a notice |
| POST   | `/api/admin/users/:username/revoke-credentials` | Superadmin | Delete the account's PATs, SSH keys and OAuth app authorizations, end its sessions, and mail it a notice |
| POST   | `/api/admin/users/:username/password-reset-link` | Superadmin | Issue a 24-hour single-use password reset link, replacing any earlier one, and mail the account a notice without it. 200 with `{"link", "expires_at"}`, or for HTMX the shown-once panel; `Cache-Control: no-store`. 409 for a suspended or passwordless account |
| POST   | `/api/admin/users/:username/delete` | Superadmin | Delete the account (`confirm_username` must equal it; 400 otherwise). 409 for a sole organization owner or the last active superadmin. HTMX: `HX-Redirect` to `/admin/users` |
| POST   | `/api/admin/users/verify-email` | Superadmin | Mark a user's email verified (`username`, `email`, plus `password` and, with 2FA, `code`; `email` must be their current address, else 404; 400 when either is missing). HTMX: 204, form: 303 to `/admin/settings`; audit-logged |

The `/api/admin/users/:username/…` actions take `password` and, with 2FA, `code`, refuse the admin's own account with 403, and are audit-logged. HTMX requests get 204 with `HX-Refresh`, forms a 303 to the user page, except `password-reset-link`, which answers with the link. See [Managing accounts](./access-control.md#managing-accounts). A suspended account's personal access tokens and OAuth app tokens get `403 {"error":"account_suspended"}` on every endpoint.

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
