# Suspension blocks every way in

Created: 2026-10-06
Category: enhancement
Status: done

Spec: [../spec.md](../spec.md)

Blocked by: none

## What to build

Add `users.suspended_at TIMESTAMPTZ` (next free migration number) and `model.User.SuspendedAt`, selected through `userColumns`/`scanUser`. Then refuse a suspended account at every way in the spec's "Ways in" table:

- `UserStore.SessionVersion` refuses a suspended user, so `sessionLive` drops their sessions on the next request.
- `UserService.generateJWT` and `SSOService.generateJWT` return `service.ErrAccountSuspended`, covering password, Google, LDAP, SAML and the TOTP step. `Authenticate` checks it after the password, so the message appears only after a correct first factor.
- `AccessTokenService.Validate` and `OAuthAppService.ResolveOAuthToken` return `ErrAccountSuspended`. `servePAT` and `serveOAuth` answer it with `403 {"error":"account_suspended"}` on both `Auth` and `OptionalAuth`, never anonymous. `resolveGitUser` answers it with `403`.
- `SSHKeyService.AuthenticatePublicKey` refuses suspended users. The SSH `sessionHandler` refuses a deploy key whose repo's personal owner is suspended.
- `wantsEmail` skips suspended users.
- Web sign-in pages (password, Google, LDAP, SAML, TOTP step) show "This account is suspended. Contact your administrator."; `POST /api/auth/login` answers `403 {"error":"account_suspended"}`.

## Acceptance criteria

- [x] A suspended user's existing web sessions stop working on their next request.
- [x] A suspended user can't start a session by password (web or API), Google, LDAP, SAML or the TOTP step, and sees the suspension message only after correct credentials.
- [x] A suspended user's PATs are refused on the API (`403 account_suspended`) and on git HTTP (`403`, not `401`).
- [x] A suspended user's OAuth app tokens are refused.
- [x] A suspended user's SSH keys are refused, and so are deploy keys on their personal repos; deploy keys on org repos still work.
- [x] Clearing `suspended_at` makes PATs, SSH keys, deploy keys and OAuth app tokens work again.
- [x] A suspended user gets no notification email.
- [x] `docs/access-control.md` gains a "Suspended accounts" section.

## Tests

- Store: `SessionVersion` errors for a suspended user.
- Service: `Authenticate` with the right password returns `ErrAccountSuspended`, with a wrong one "invalid credentials"; `GenerateTokenForUser`, `Validate`, `ResolveOAuthToken`, `SSHKeyService.AuthenticatePublicKey` refuse; each works again after clearing.
- Middleware: a PAT whose validator returns `ErrAccountSuspended` gets `403 account_suspended` under `Auth` and `OptionalAuth`.
- `wantsEmail` unit test.
