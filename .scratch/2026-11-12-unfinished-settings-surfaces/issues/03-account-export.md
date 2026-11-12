# Account export

Created: 2026-11-12
Category: enhancement
Status: ready-for-agent
Blocked by: 02 (both edit `settings.templ`)
Parallel: safe with 04 and 01.

| Branch | Worktree | PR title |
| --- | --- | --- |
| `feat/account-export` | `.worktrees/feat+account-export` | `feat(settings): export your data` |

## What

Build the danger-zone "Export your data" per the [spec](../spec.md#account-export). Read `docs/storage.md` first.

- Migration: `account_exports(id, user_id, status requested|running|ready|failed, storage_key, error, requested_at,
  ready_at, expires_at)`.
- `ExportService`: `Request` (one running per User, one request per day), a background worker (follow the existing
  job/scheduler pattern, e.g. repo import), `Open` for download. The zip goes through the storage `Backend` under
  `exports/<user id>/<id>.zip`; keys must pass `storage.ValidKey`.
- Content is JSON files per [spec](../spec.md#account-export); stream into the zip so memory stays flat. Never include
  tokens, password hashes or TOTP secrets: add a test that greps the zip for them.
- Expiry sweep deletes the object and row after 7 days; account deletion deletes them too (`DeleteUser`).
- Download needs re-authentication. With SMTP on, email the User when ready.
- `settings.templ`: the row shows requested/ready/failed. Delete `comingSoonBadge` and `comingSoonButton` here if 02
  has merged (this ticket is the last user), and document the export in `docs/storage.md`.

## Tests

Service: zip holds the listed files only; another User's data is absent; one request per day; expiry sweep; account
deletion removes the object. Handler: download needs re-auth and refuses another User's export. Run against the local
backend; S3 behind `TEST_S3_*`.

## Mocks

`mockups/settings.html` danger zone: requested, ready, failed rows. Light and dark at 1440.

## Acceptance criteria

- [ ] Request → ready → download works and the zip contents match the spec
- [ ] No secrets in the zip; expiry and account deletion clean up
- [ ] No "Coming soon" left in `settings.templ`
- [ ] Docs and mock updated, light and dark

## Verification

Throwaway server on a scratch database at 1440×900, light and dark, per the harness in the [uiux spec](../../2026-11-06-uiux-desktop-9.5/spec.md#verification-harness). Sign in as `admin@example.test` / `cloudzilla-seed`; `paper-planes` is the seeded organization. Request, wait, download, unzip and inspect.

## Comments
