# Signing keys: tables, services and API

Created: 2026-11-13
Category: enhancement
Status: done
Blocked by: none
Parallel with: none (everything else waits on it)

| Branch | Worktree | PR title | Effort |
| --- | --- | --- | --- |
| `feat/phase-17.2-signing-keys` | `.worktrees/feat+phase-17.2-signing-keys` | `feat(signing): GPG and SSH signing keys (17.2)` | L |

Spec: [spec.md](../spec.md). Parent: `../../2026-11-06-uiux-desktop-9.5/issues/16-signed-commits.md`.

## Problem

Nothing can store a key that signs commits. `ssh_keys` is the auth path (MD5 fingerprint, deploy-key shadow rule) and must stay untouched.

## Scope

- Migration (next free number; `130_create_signing_keys.sql` as of 3332dc7e): `gpg_keys`, `gpg_subkeys`, `gpg_key_emails`, `ssh_signing_keys` exactly as in the spec's Schema.
- Models in `internal/model/`, stores wired into `Stores`, `SigningKeyService` wired into `Services`.
- GPG add: parse the armored block with `ProtonMail/go-crypto` (promote it from indirect to direct in `go.mod`); take the primary key ID and fingerprint, the signing-capable subkeys with their expiry, revocation and UID emails; refuse a private key block, an expired or revoked key, a key with no signing-capable (sub)key, and a fingerprint or subkey ID already registered (`409`).
- SSH signing add: `ssh.ParseAuthorizedKey`, SHA256 fingerprint, same key-type allowlist as `parseSigningKey` (Ed25519, ECDSA, 2048-bit RSA, sk-); a fingerprint already registered by any account is `409`. Adding a key that is also in `ssh_keys` or `deploy_keys` is allowed.
- `GET/POST/DELETE /api/user/gpg_keys` and `/api/user/ssh_signing_keys` (list, add with `title` + `public_key`, delete by id), session-only like `/api/user/keys`, POST needs the password + TOTP step-up (`ConfirmFields` semantics), delete only your own. Audit entries `user.gpg_key.add|delete` and `user.ssh_signing_key.add|delete`.
- `revoke-credentials` also deletes the account's signing keys; the data export gains `gpg_keys.json` and `ssh_signing_keys.json` (public parts).
- API documented in `docs/api-reference.md`; route in `internal/router/router.go`.

## Files (hotspots in bold)

- `internal/db/migrations/13x_create_signing_keys.sql` (new), `internal/model/signing_key.go` (new)
- **`internal/store/stores.go`**, `internal/store/gpg_key_store.go`, `internal/store/ssh_signing_key_store.go` (new)
- **`internal/service/services.go`**, `internal/service/signing_key_service.go` (new), `internal/service/gpg_key_parse.go` (new)
- `internal/handler/signing_key_handler.go` (new), **`internal/router/router.go`**, `internal/model/audit_log.go`
- `internal/service/export_service.go`, `internal/store/export_data_store.go`, `internal/service/admin_user_service*.go` (revoke-credentials)
- `go.mod`, `docs/api-reference.md`, `docs/access-control.md` (session-only list)

## Acceptance criteria

- [x] A valid armored GPG public key is stored with its key ID, fingerprint, subkeys (with `can_sign`), expiry, revocation and UID emails; a private key block, expired key, revoked key, no signing-capable key and a duplicate each get the stated error (`400`/`409`/`422`)
- [x] A valid SSH public key is stored with its SHA256 fingerprint; the same key already in `ssh_keys` is accepted and `ssh_keys` is unchanged; the same key on another account is `409`
- [x] POST without the password + TOTP step-up is refused; a PAT and an OAuth-app token cannot call any of the six endpoints
- [x] Deleting a key removes its rows and subkeys/emails; you cannot delete another user's key
- [x] Audit entries are written for add and delete; `revoke-credentials` deletes signing keys; the export contains both files
- [x] Account deletion cascades both key tables
- [ ] `make lint` and `go test ./...` pass; `make test-integration` passes for the new store queries
- [x] Verified once against a throwaway server (scratch Postgres, unique `CZ_AUTH_COOKIE_NAME`, `admin@example.test` / `cloudzilla-seed`) with real `curl`/native git, not only unit tests

## Verification

Integration tests for the stores and service (testdata keys generated with a temp `GNUPGHOME` and `ssh-keygen`); handler tests for status codes and step-up. Throwaway server: scratch Postgres on a free port (see `docs/testing.md`), a unique `CZ_AUTH_COOKIE_NAME`, sign in as `admin@example.test` / `cloudzilla-seed`. Add one GPG and one SSH key with `curl` and a session cookie, list, delete.

## Comments

