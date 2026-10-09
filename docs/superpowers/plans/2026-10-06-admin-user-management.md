# Admin user management — Implementation Plan

> For agentic workers: run this plan with superpowers:subagent-driven-development, one task at a time, test first.

**Goal:** Superadmins can list and search accounts, suspend and unsuspend them, promote and demote, reset 2FA, revoke tokens and keys, and delete accounts, with every action confirmed and audited. A suspended account loses every way in.

**Architecture:** One column, `users.suspended_at`. Enforcement sits at the existing chokepoints: the per-request session lookup, the two `generateJWT` functions, PAT and OAuth token resolution, and SSH key lookup. A new `AdminUserService` holds the actions; each update that can remove an active superadmin locks the active superadmin rows first.

**Tech Stack:** Go, chi, PostgreSQL, templ v0.3, HTMX, Alpine.js, Tailwind v4.

**Spec:** [`.scratch/2026-10-06-admin-user-management/spec.md`](../../../.scratch/2026-10-06-admin-user-management/spec.md); tickets in [`.scratch/2026-10-06-admin-user-management/issues/`](../../../.scratch/2026-10-06-admin-user-management/issues/).

## Global Constraints

- Stores → services → handlers. `context.Context` first.
- Edit `.templ` files, then `make generate-templ`. Commit each `_templ.go` with its source.
- Migration takes the next free number at commit time.
- Comments state only a *why*, one line by default.
- Stage files by explicit path. Never stage `.claude/`.
- Integration tests need `TEST_DATABASE_DSN`.

## File tree

```
internal/db/migrations/NNN_user_suspension.sql        new
internal/model/user.go                                 SuspendedAt, Suspended()
internal/model/audit_log.go                            admin.user.* actions
internal/store/user_store.go                           userColumns, SessionState, admin list/suspend/role/2FA/revoke, superadmin lock
internal/store/admin_user_store_test.go                new
internal/service/errors (user_service.go)              ErrAccountSuspended, ErrLastSuperadmin
internal/service/user_service.go                       generateJWT, Authenticate, DeleteUser, SessionState
internal/service/sso_service.go                        generateJWT
internal/service/access_token_service.go               Validate
internal/service/oauth_app_service.go                  ResolveOAuthToken
internal/service/ssh_key_service.go                    AuthenticatePublicKey
internal/service/email_service.go                      wantsEmail
internal/service/admin_user_service.go                 new
internal/service/services.go                           AdminUser
internal/middleware/auth.go                            SessionState, fresh IsSuperadmin, 403 account_suspended
internal/ssh/server.go                                 deploy key owner check
internal/handler/git_http.go                           403 for suspended
internal/handler/auth/sign-in handlers                 suspension message
internal/handler/page_settings_handler.go              ErrLastSuperadmin message
internal/handler/admin_user_handler.go                 new
internal/view/viewmodels_admin_users.go                new
internal/view/pages/admin_users.templ                  new (list + user page)
internal/view/pages/admin_nav.templ                    Users tab
internal/view/pages/user.templ                         Suspended badge
internal/router/router.go                              routes
docs/access-control.md, docs/api-reference.md
```

## Task 1 — suspension (ticket 01)

- [x] Migration `ALTER TABLE users ADD COLUMN suspended_at TIMESTAMPTZ`.
- [x] `SuspendedAt *time.Time` in the model, `userColumns` and `scanUser`.
- [x] Failing tests for each way in, then the checks.
- [x] `SessionVersion` → `SessionState` returning version and superadmin flag, refusing suspended users.
- [x] `ErrAccountSuspended` surfaced by `servePAT`/`serveOAuth` (403), `resolveGitUser` (403), sign-in pages and `POST /api/auth/login`.
- [x] SSH user and deploy keys; `wantsEmail`.

## Task 2 — fresh superadmin (ticket 02)

- [x] Middleware test, then overwrite `claims.IsSuperadmin` from `SessionState`.

## Task 3 — last superadmin (ticket 03)

- [x] `lockActiveSuperadmins(tx, excluding)` helper; `ErrLastSuperadmin`.
- [x] `DeleteWithOwnedRepos` runs it; `DeleteAccount` shows the message.

## Task 4 — list (ticket 04)

- [x] `ListForAdmin` store test, then the query.
- [x] Handler, view-model, templ, tab, route.

## Task 5 — user page, suspend, unsuspend, revoke (ticket 05)

- [x] Store methods and tests; `AdminUserService` with self and last-admin guards and audit.
- [x] Handler, templ with `ConfirmFields`, routes, profile badge.

## Task 6 — promote, demote, 2FA reset (ticket 06)

- [x] Store and service, tests, handler and buttons.

## Task 7 — delete (ticket 07)

- [x] Service and handler with typed-username check; docs.

## Task 8 — review

- [x] `go vet ./...`, `go test ./...` with `TEST_DATABASE_DSN`, `make generate-templ` leaves no diff.
- [x] Subagent review pass; fix findings.
- [x] Tick acceptance criteria; set `Status: done` on spec and tickets.
