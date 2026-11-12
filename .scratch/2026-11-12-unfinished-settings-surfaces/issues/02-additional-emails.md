# Additional emails

Created: 2026-11-12
Category: enhancement
Status: ready-for-agent
Blocked by: —
Parallel: safe with 01 and 04. Shares `settings.templ` with 03, which waits for this.

| Branch | Worktree | PR title |
| --- | --- | --- |
| `feat/additional-emails` | `.worktrees/feat+additional-emails` | `feat(settings): additional email addresses` |

## What

Build the Email section's "Add email" per the [spec](../spec.md#additional-emails).

- Migration: `user_emails(id, user_id, email, verified_at, created_at)`; unique index on `lower(email)`; the add path
  also refuses an address that equals any `users.email` (`users_email_lower_key` is the model).
- Store/service (`EmailVerificationService` is the base): add (sends the verification link), verify, remove, list.
  Only verified rows are used.
- `userByAuthorEmail` in `internal/service/commit_email.go`: after the noreply and primary lookups, match a verified
  additional address. Update `docs/access-control.md` (commit credit) in this PR.
- Adding and removing need the re-authentication used for the primary email change (`ConfirmFields`).
- Without SMTP hide "Add email" and say verification needs email. The primary row has no remove control.
- `settings.templ`: replace the badge and disabled button with a working add form and a list of extra addresses with
  Verified/Unverified chips and a remove button. Delete `comingSoonBadge` and `comingSoonButton` if 03 already merged;
  otherwise leave them for 03.
- Routes in `internal/router/router.go`; the verify link route must not need a session if the primary one does not.

## Tests

Service: add refuses a duplicate of another User's primary or extra address, case-insensitive; unverified address never
credits a commit; verified one does; verifying with a stale or foreign token fails; remove stops crediting. Handler:
re-auth required; no SMTP hides the form. Integration against `TEST_DATABASE_DSN`.

## Mocks

`mockups/settings.html` (Email section: list, add form, unverified chip, no-SMTP note). Light and dark at 1440.

## Acceptance criteria

- [ ] A verified additional address credits commits pushed with it; an unverified one does not
- [ ] Duplicate addresses are refused across all Users
- [ ] Add/remove re-authenticate; no-SMTP state reads correctly
- [ ] Docs and mock updated, light and dark

## Verification

Throwaway server on a scratch database at 1440×900, light and dark, per the harness in the [uiux spec](../../2026-11-06-uiux-desktop-9.5/spec.md#verification-harness). Sign in as `admin@example.test` / `cloudzilla-seed`; `paper-planes` is the seeded organization. Push a commit with the extra address as author and see the avatar on the commit list.

## Comments
