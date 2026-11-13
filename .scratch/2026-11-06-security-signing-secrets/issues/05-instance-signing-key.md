# Instance signing key and admin section

Created: 2026-11-13
Category: enhancement
Status: ready-for-agent
Blocked by: 02
Parallel with: 03, 04

| Branch | Worktree | PR title | Effort |
| --- | --- | --- | --- |
| `feat/phase-17.2-instance-signing-key` | `.worktrees/feat+phase-17.2-instance-signing-key` | `feat(signing): instance signing key (17.2)` | M |

Spec: [spec.md](../spec.md). Parent: `../../2026-11-06-uiux-desktop-9.5/issues/16-signed-commits.md`.

## Problem

Server-made commits need a key the instance holds (ADR 0002), and the verifier must trust it, including keys retired by a rotation.

## Scope

- Migration `instance_signing_keys` (next free number after ticket 01's; `131` as of 3332dc7e), model and store.
- `InstanceSigner`: generates an Ed25519 key on first need, seals the private half with `secretbox` (purpose `instance-signing`), `Sign(payload) (armoredSSHSig, error)`, `Rotate(ctx)`. With no `security.secret_key` it returns a sentinel `ErrNoSecretKey` and logs a startup warning; nothing else changes.
- Verifier: ticket 02's lookup consults `instance_signing_keys` (active and retired) before user keys; a match is `verified`, kind `instance`, signer "Cloudzilla".
- Admin: a "Commit signing" section on `/admin/settings` (superadmin only) showing the active fingerprint and public key with a copy button, the no-secret-key notice, and **Rotate key** behind the existing confirmation step-up; audit entry `admin.signing_key.rotate`. Mock it in `admin_settings.html` (not drawn yet).
- `docs/configuration.md`: the `security.secret_key` row mentions instance signing.

## Files (hotspots in bold)

- `internal/db/migrations/13x_create_instance_signing_keys.sql` (new), `internal/model/instance_signing_key.go` (new), `internal/store/instance_signing_key_store.go` (new)
- `internal/service/instance_signer.go` (new), **`internal/service/signature_service.go`**, **`internal/service/services.go`**, `internal/secretbox/secretbox.go` (purpose constant only)
- `internal/handler/admin_settings_handler.go`, `internal/view/pages/admin_settings.templ`, `internal/router/router.go`, `internal/model/audit_log.go`
- `mockups/admin_settings.html`, `docs/configuration.md`

## Acceptance criteria

- [x] The first call generates and seals a key; the private half in the table is not the plaintext key and fails to open under another `security.secret_key` (`ErrUndecryptable`)
- [x] Rotate retires the old row, creates a new active one, and a signature by the retired key still verifies as `verified`
- [x] Only one active row can exist (unique partial index; two concurrent first calls produce one key)
- [x] With no `security.secret_key`: `ErrNoSecretKey`, the startup warning, and the admin section explains why
- [x] Non-superadmins get `403` on the rotate route; rotation requires the step-up and writes an audit entry
- [x] Checked at desktop 1440 in **light and dark** on a throwaway server (scratch Postgres, unique `CZ_AUTH_COOKIE_NAME`, `admin@example.test` / `cloudzilla-seed`); evidence for both themes in the PR
- [x] The standing mocks in `mockups/` listed under Files are updated in the same PR (classes copied from the templ, `COVERAGE.md` / `index.html` where affected) and checked under `make mockups` in both themes
- [x] The scratch mock beside the spec (`commit.html`, `settings.html`, `mock.html`) matches the shipped page
- [ ] `make generate-templ` re-run and the `_templ.go` files committed; `make lint` and `go test ./...` pass

## Verification

Throwaway server: scratch Postgres on a free port (see `docs/testing.md`), a unique `CZ_AUTH_COOKIE_NAME`, sign in as `admin@example.test` / `cloudzilla-seed`. Rotate twice from `/admin/settings`; copy the public key into an `allowed_signers` file and confirm a test signature with `ssh-keygen -Y verify -n git`.

## Comments

