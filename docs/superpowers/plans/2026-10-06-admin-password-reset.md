# Admin password reset — Implementation Plan

> For agentic workers: run this plan one task at a time, test first.

**Goal:** A superadmin can issue a password reset link for another account from `/admin/users/{username}` and hand it over, and no reset path works for a suspended account.

**Architecture:** No migration. Suspension joins `passwordResetUsable`, so every link check refuses it. `Request` and `IssueLink` refuse it up front. The admin action is `AdminUserService.IssuePasswordResetLink` on top of `PasswordResetService.IssueLink`. Its handler writes the audit entry synchronously and swaps the link into the Actions card instead of reloading the page.

**Tech Stack:** Go, chi, PostgreSQL, templ v0.3, htmx 4, Alpine.js, Tailwind v4.

**Spec:** [`.scratch/2026-10-06-admin-password-reset/spec.md`](../../../.scratch/2026-10-06-admin-password-reset/spec.md); tickets in [`.scratch/2026-10-06-admin-password-reset/issues/`](../../../.scratch/2026-10-06-admin-password-reset/issues/).

## Global Constraints

- Stores → services → handlers. `context.Context` first.
- Edit `.templ` files, then `make generate-templ`. Commit each `_templ.go` with its source.
- Comments state only a *why*, one line by default.
- Stage files by explicit path. Never stage `.claude/`.
- Integration tests need `TEST_DATABASE_DSN`.

## File tree

```
internal/store/password_reset_store.go          passwordResetUsable gains suspended_at IS NULL
internal/store/password_reset_store_test.go     new: Consume rechecks suspension under lock
internal/service/password_reset_service.go      Request skips, IssueLink refuses suspended accounts
internal/service/password_reset_service_test.go suspended cases
cmd/cloudzilla/password_reset_link.go           unsuspend hint
cmd/cloudzilla/password_reset_link_test.go      suspended case
internal/service/audit_service.go               RecordNow: synchronous Record
internal/service/admin_user_service.go          IssuePasswordResetLink, notice
internal/service/admin_user_service_test.go     refusals, link, notice
internal/service/services.go                    wire PasswordReset into AdminUser
internal/handler/admin_user_handler.go          AdminIssuePasswordResetLink, error mapping
internal/router/router.go                       POST /api/admin/users/{username}/password-reset-link
internal/router/admin_password_reset_test.go    new: end to end through the router
internal/view/viewmodels_admin.go (or similar)  AdminResetLinkFragData
internal/view/fragments/admin_reset_link.templ  new: shown-once panel
internal/view/pages/admin_users.templ           the Actions row
docs/access-control.md                          endpoint matrix, password-reset section
```

## Task 1: Suspended accounts (ticket 01)

1. Failing tests: `Request` for a suspended account mails nothing; a link reads invalid once the account is suspended (set `suspended_at` directly, so `session_version` doesn't do the work), and `Reset` refuses it; a link from before `UserStore.Suspend` stays invalid after `Unsuspend`; `IssueLink` returns `ErrUserSuspended`; `Consume` refuses when suspension lands after `Lookup`; the CLI prints the unsuspend hint.
2. Add `u.suspended_at IS NULL` to `passwordResetUsable`. Add the checks to `Request` and `IssueLink`. Map the error in the CLI.
3. Green, then commit.

## Task 2: Admin action (ticket 02)

1. Failing service tests: self → `ErrAdminSelf`; suspended → `ErrUserSuspended`; passwordless → `ErrPasswordResetNoPassword`; success returns a pending admin link and mails a notice without the token.
2. `AuditService.RecordNow` (synchronous, shares the entry with `Record`).
3. `IssuePasswordResetLink` and `WithPasswordResets`; wire in `services.go`.
4. Failing router tests: reauth required; non-superadmin 403; self 403; suspended 409; success renders the panel with the link and writes `user.password.reset_link` with `issued_by: admin`, actor the admin; the page doesn't show the link after a reload; the JSON path returns the link.
5. Handler, route, view-model, fragment, Actions row; `make generate-templ`.
6. Docs. Green, then commit.

## Task 3: Review

Subagent review of the diff against the spec and tickets; fix findings; tick the acceptance criteria; set `Status: done`.
