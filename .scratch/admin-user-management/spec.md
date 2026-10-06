# Admin user management

Created: 2026-10-06
Category: enhancement
Status: needs-triage

## Problem

A superadmin can't see or act on the instance's accounts. The admin area has three pages: `/admin/settings` (site settings, invitations, manual email verification), `/admin/audit-log` and `/admin/sso` (`internal/view/pages/admin_settings.templ`, `audit_log.templ`, `sso_settings.templ`, tabs in `admin_nav.templ`). `/api/admin` has only `POST /settings`, `POST`/`DELETE /invitations`, `POST /users/verify-email` and `POST /sso/{provider}/enabled` (`internal/router/router.go`).

Missing:

- **A user list.** No page lists accounts, and no store query lists them for an admin: `SearchStore.SearchUsers` is a username-prefix lookup for global search, capped by `limit`, with no email, role or status.
- **Suspension.** Nothing cuts an account off while keeping its content. When someone leaves, or an account is compromised, the admin's only lever is the database.
- **Admin delete.** Only the account itself can delete it (`POST /settings/delete-account` → `UserService.DeleteUser`).
- **Promote and demote.** `users.is_superadmin` (migration 013) is set only by `/setup` (`UserStore.CreateSuperadmin`). No code path changes it, and there's no CLI for it (`cmd/cloudzilla` has `gc`, `seed` and `stats`).
- **2FA reset.** `TOTPService.Disable` needs the user's own current code, so a user who lost their authenticator and backup codes is locked out for good.

Three gaps in what exists today bear on the design:

1. **`is_superadmin` is baked into the session JWT.** `UserService.generateJWT` and `SSOService.generateJWT` copy it into the `is_superadmin` claim, and `RequireSuperadmin` reads only the claim. A demoted admin keeps admin access until their JWT expires.
2. **Nothing stops the last superadmin from deleting their account.** `DeleteAccount` and `DeleteUser` check sole org ownership but not superadmin status, so an instance can be left with no admin. `/setup` runs only while no accounts exist, so there's no way back short of SQL.
3. **Sign-in has no account-state check.** Each way in loads the user and proceeds; see the table below.

## Ways in

A suspended account must lose every one of these. Each already funnels through a single place:

| Way in | Where it's checked today | Hook for suspension |
| --- | --- | --- |
| Web session (JWT cookie or bearer) | `authMW`/`optAuthMW` → `sessionLive` → `UserStore.SessionVersion`, one primary-key lookup per request | The same query refuses a suspended user |
| Password sign-in, web and `POST /api/auth/login` | `UserService.Authenticate` → `generateJWT` | Refuse after the password checks out |
| Google sign-in | `UserService.AuthenticateOAuth` → `generateJWT` | Same |
| LDAP and SAML sign-in | `SSOService.AuthenticateLDAP` / `HandleSAMLCallback` → `findOrProvisionUser` → `SSOService.generateJWT` | Same |
| TOTP second step | `VerifyTOTP` → `UserService.GenerateTokenForUser` → `generateJWT` | Same |
| Personal access token, API | `servePAT` → `AccessTokenService.Validate` (loads the user with `users.GetByID`) | `Validate` refuses |
| Personal access token, git HTTP (Basic password) | `resolveGitUser` (`internal/handler/git_http.go`) → `AccessTokenService.Validate` | Same, answered as a `403` so git keeps the stored credential |
| OAuth app token | `serveOAuth` → `OAuthAppService.ResolveOAuthToken` (loads the user) | `ResolveOAuthToken` refuses |
| SSH user key | `internal/ssh` `publicKeyHandler` → `SSHKeyService.AuthenticatePublicKey` (loads the user) | `AuthenticatePublicKey` refuses |
| SSH deploy key | `publicKeyHandler` → `DeployKeyService.AuthenticatePublicKey` | Refused when the key's repo is a personal repo of a suspended user; `deploy_keys` records no creator, so org repos' keys are unaffected |

Every session JWT is minted in one of the two `generateJWT` functions, so refusing there also covers ways in that are added later (such as the password-reset flow being built in parallel).

## Proposed design

### Data

One migration (next free number at commit time; several parallel branches add migrations):

```sql
ALTER TABLE users ADD COLUMN suspended_at TIMESTAMPTZ;
```

Who suspended the account, and why, go in the audit log, not the row. A `suspended_by` FK would need an `ON DELETE` action or a `ghostReassignments` entry (`TestGhostReassignments_CoverEveryUserFKWithoutDeleteAction`).

"Active superadmin" means `is_superadmin AND suspended_at IS NULL`, excluding the ghost.

### Admin pages

- **`/admin/users`**: a new admin tab. It lists accounts 50 per page, newest first, with username, name, email, role, status (active or suspended), 2FA, sign-in method and created date. It filters by role (all, superadmins) and status (all, active, suspended), and `q` matches a username or email prefix, case-insensitively, using the existing `lower(username)` and `lower(email)` indexes. The ghost is never listed. Offset pagination with a total count, like `/admin/audit-log`.
- **`/admin/users/{username}`**: one account, with its email and verification, role, status, 2FA, sign-in methods, created date, last web sign-in (latest `login` audit entry, via the `(actor_id, created_at)` index), and the organizations it solely owns. It also holds the actions, each in a confirmation dialog.

Layout choices get a mockup before any UI is built.

### Actions

Each action is a `POST` under `/api/admin/users/{username}/…` (HTMX-aware like the other admin forms). Each needs the admin's own password and, with 2FA, a code (`confirmAction`, as for every other superadmin change), and writes an audit entry with the admin as actor and the user as target.

| Action | Effect | Refused when |
| --- | --- | --- |
| Suspend (optional reason) | Sets `suspended_at` and bumps `session_version` in one `UPDATE`, so unsuspending doesn't revive old sessions | Self; it would leave no active superadmin |
| Unsuspend | Clears `suspended_at`. Tokens, keys and app grants work again; sessions don't | — |
| Promote to superadmin | `is_superadmin = TRUE` | The user is suspended |
| Demote | `is_superadmin = FALSE` | Self; it would leave no active superadmin |
| Reset 2FA | Clears the TOTP secret, `totp_enabled` and backup codes; mails the user a security notice | Self (use Settings) |
| Delete | `UserService.DeleteUser`: personal repos go, contributions elsewhere pass to the ghost. Also requires typing the username | Self (use Settings); it would leave no active superadmin; the user solely owns an organization (`ErrSoleOrgOwner`, naming the orgs) |

Audit actions: `admin.user.suspend` (metadata: reason), `admin.user.unsuspend`, `admin.user.promote`, `admin.user.demote`, `admin.user.2fa_reset`, `admin.user.delete` (metadata: email). `target_id` has no FK, so a deleted user's entries keep their `target_name`.

### Role changes take effect on the next request

`sessionLive` already reads the user row on every session request. It will read `is_superadmin` along with `session_version` and overwrite the JWT's claim, so a promotion or demotion applies to the user's next request without signing them out. `RequireSuperadmin` and every `claims.IsSuperadmin` check get the fresh value. Tokens never carry `IsSuperadmin`, so they are unaffected.

### Never zero active superadmins

Refusing self-destructive actions means one admin alone can't remove the last active superadmin. Two cases remain:

- **Concurrent actions.** Admins A and B each demote, suspend or delete the other at the same moment. Each check passes on its own. Each of these updates locks the active superadmin rows (`SELECT … FOR UPDATE`) and re-counts in the same transaction, so the second one fails.
- **Self-service deletion.** `DeleteUser` refuses (`ErrLastSuperadmin`, shown on Settings) when the account is the last active superadmin, under the same lock.

### What a suspended account shows

See open question 4. The proposed default changes nothing for other users: the profile, public repos, issues and comments stay. Collaborators keep their access to the suspended user's repos. Superadmins see a "Suspended" badge on the profile and in the list. The suspended user gets no notification email: `wantsEmail` skips them, which covers both immediate mail and digests.

A suspended user who signs in with correct credentials sees "This account is suspended. Contact your administrator." The message appears only after the first factor passes, so it doesn't tell a stranger the account exists. `POST /api/auth/login` answers `403 {"error":"account_suspended"}`, and so do API requests with a suspended user's token.

## Acceptance criteria

- [ ] `/admin/users` lists every account but the ghost, 50 per page, with total count, `q` prefix search on username and email, and role and status filters; non-superadmins get `403`.
- [ ] `/admin/users/{username}` shows the account's details and actions; an unknown or ghost username is a `404`.
- [ ] A suspended user's existing web sessions stop working on their next request, and stay dead after unsuspension.
- [ ] A suspended user can't start a session by password (web or API), Google, LDAP, SAML or the TOTP step, and sees the suspension message only after correct credentials.
- [ ] A suspended user's PATs are refused on the API (`403 account_suspended`) and on git HTTP (`403`, not `401`).
- [ ] A suspended user's OAuth app tokens are refused.
- [ ] A suspended user's SSH keys are refused, and so are deploy keys on their personal repos; deploy keys on org repos still work.
- [ ] After unsuspension, PATs, SSH keys, deploy keys and OAuth app tokens work again.
- [ ] Promote and demote take effect on the user's next request, without signing them out.
- [ ] Reset 2FA clears the secret, flag and backup codes, and mails the user.
- [ ] Admin delete removes the account like self-service deletion, and refuses a sole organization owner.
- [ ] No action applies to the acting admin's own account (except promote, which is moot).
- [ ] No sequence of actions, concurrent or not, and no self-service deletion leaves zero active superadmins.
- [ ] Every action needs the admin's password (and 2FA code) and writes its audit entry.
- [ ] A suspended user gets no notification email.
- [ ] `docs/access-control.md` (endpoint matrix, instance roles, a new "Suspended accounts" section) and `docs/api-reference.md` describe the new routes and behaviour.

## Relevant files

- `internal/db/migrations/` (new `users.suspended_at`)
- `internal/model/user.go`, `internal/model/audit_log.go`
- `internal/store/user_store.go`: `userColumns`, `SessionVersion`, new list, count, suspend, role, TOTP-reset and active-superadmin-lock queries
- `internal/service/user_service.go` (`generateJWT`, `Authenticate`, `DeleteUser`), `sso_service.go` (`generateJWT`), `access_token_service.go` (`Validate`), `oauth_app_service.go` (`ResolveOAuthToken`), `ssh_key_service.go`, `deploy_key_service.go`, `email_service.go` (`wantsEmail`), a new admin user service
- `internal/middleware/auth.go` (`sessionLive`, the `SessionVersions` interface)
- `internal/ssh/server.go`, `internal/handler/git_http.go` (`resolveGitUser`)
- `internal/handler/page_auth_handler.go`, `auth_handler.go`, the LDAP, SAML and Google handlers, `totp_handler.go`, `page_settings_handler.go` (`DeleteAccount`), a new admin users handler
- `internal/view/pages/admin_nav.templ`, new admin user templates, `user.templ` (badge), `internal/view/viewmodels_user.go`
- `internal/router/router.go`
- `docs/access-control.md`, `docs/api-reference.md`

## Open questions

1. **Suspend, delete, or both?** Suspension is reversible and keeps everything. Delete is final, removes personal repos, and hands contributions to the ghost. Proposed: both, with suspend as the everyday tool.
2. **v1 scope.** Proposed: list and search, user page, suspend and unsuspend, promote and demote, 2FA reset, delete. Admin-issued password reset waits for open question 3.
3. **Admin-issued password reset.** The parallel password-reset branch (`feat/password-reset`, nothing committed yet) owns reset tokens. Options: a follow-up that reuses its service once it lands; a "set a temporary password" action now, which means the admin knows the user's password; or calling its service from this branch before it merges. Proposed: follow-up.
4. **What do others see of a suspended account?** Options: nothing changes (badge for superadmins only); a public "Suspended" badge; or hide the profile and personal repos (404) from everyone but superadmins, which also cuts off collaborators. Proposed: nothing changes.
5. **Do tokens, keys and app grants survive suspension?** Proposed: they're refused while suspended and work again after. If the account was compromised, anything the attacker created comes back too. An "also revoke tokens, SSH keys and app authorizations" option on suspend would cover that.
6. **Sole organization owners.** Superadmins have no organization override (restoring a deleted repo is the only permission check that honours `claims.IsSuperadmin`). Suspending an org's only owner leaves it unmanageable; deleting one is refused. Proposed: allow suspension with a warning naming the orgs, and leave "superadmin takes over an org" as a follow-up.
