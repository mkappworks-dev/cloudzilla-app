# Signing keys: Remove keeps history, Revoke, in-place GPG update

Created: 2026-11-13
Category: enhancement
Status: ready-for-agent
Blocked by: none (01–08 are merged on the base)
Parallel with: 09

| Branch | Worktree | PR title | Effort |
| --- | --- | --- | --- |
| `tech/signing-key-revocation` | `.worktrees/tech+signing-key-revocation` | `feat(signing): remove keeps history, revoke and update signing keys (17.2)` | L |

Spec: [spec.md](../spec.md), decisions 7 and 14. Parent: `../../2026-11-06-uiux-desktop-9.5/issues/16-signed-commits.md`.

## Problem

Verification runs on every read with no stored results, so deleting a signing key turns all its commits Unverified at once. Maya retires a laptop and removes `SHA256:aaa`: 400 commits she signed over two years read `no_key`. The only way to keep them Verified is to leave the retired key registered, which keeps it valid for any commit made with it later.

A GPG key's revocation is read only when the key is added, and `parseGPGKey` refuses a revoked primary key, so `gpg_keys.revoked_at` is never set; only a revoked subkey is stored, and `gpgKeyState` then ignores the revocation date. There is no way to say "this key was compromised" as opposed to "I no longer use it", and a GPG key can be updated (new expiry, a revocation certificate) only by deleting and re-adding it, which flips its history to Unverified in between.

Decision 4 (verify on read, no cache) stays; no ADR. Neither GitHub nor GitLab re-fetches from keyservers, and neither do we.

## Decisions (spec decision 14)

- **Remove** is a soft delete (`removed_at`). A removed key still verifies commits whose committer date is before `removed_at`; later ones are `unverified` with the new reason `key_removed`. Back-dating a commit with a removed key is the cost decision 7 already accepts for expiry, extended to removed keys. The fingerprint stays reserved to the same account.
- **Revoke** (GPG and SSH) marks a key compromised: every commit by it, past and future, whatever its date, is `unverified` (`key_revoked`). One-way, no step-up (only the owner is hurt), works on active and removed keys.
- **Restore** re-activates a removed, non-revoked key. It needs the add step-up because it re-arms a key.
- **GPG update in place:** `POST /api/user/gpg_keys` with a fingerprint the account already owns refreshes the public key, subkeys, UID emails and expiry (idempotent, same step-up). Another account's fingerprint is `409`. An expired or revoked block is accepted for a key that is already yours. `revoked_at` and `removed_at` are never cleared by an update, so a stale copy cannot un-revoke a key.
- **Revocation reason mapping** for an uploaded OpenPGP revocation (primary key and each subkey): compromised or no reason → Revoke; superseded or retired → Remove at the revocation signature's creation time. Replaces the date-blind `RevokedAt != nil` in `gpgKeyState`.
- `revoke-credentials` and account deletion stay hard deletes (`no_key`); the data export gains `removed_at` and `revoked_at`.
- An SSH signing key added again by its owner while removed is a Restore; while revoked it is refused.

## Scope

- Migration `132_signing_key_removal.sql` (re-check the next free number on `origin/main` at build): `removed_at TIMESTAMPTZ` on `gpg_keys` and `ssh_signing_keys`, `revoked_at TIMESTAMPTZ` on `ssh_signing_keys` (`gpg_keys` and `gpg_subkeys` already have it). `gpg_subkeys` gets `removed_at` only if the superseded/retired mapping needs a per-subkey date there; otherwise store it in `revoked_at` with a `revoke_kind` text (`compromised` | `superseded`), decided at build and recorded in the PR.
- Models, `GPGKeyStore` / `SSHSigningKeyStore` (`Remove`, `Revoke`, `Restore`, upsert on an owned fingerprint; the lookups used by `SignatureService` must return removed and revoked keys, the settings lists must not mix them up).
- `SigningKeyService`: remove / revoke / restore / update-in-place, ownership checks, audit entries `user.gpg_key.remove|revoke|restore|update` and the SSH twins. `DeleteGPGKey` / `DeleteSSHKey` become the soft remove.
- `parseGPGKey`: take the revocation reason and time from the primary and subkey revocation signatures and set `revoked_at` / the removal time per the mapping; accept an expired or revoked block when the caller owns the fingerprint, still refuse it for a new key.
- `SignatureService` (`judgeGPG`, `judgeSSH`, `gpgKeyState`): state at the committer date: revoked → `key_revoked` always; removed with committer date ≥ `removed_at` → `key_removed`; otherwise as today. `ReasonKeyRemoved` added; popover text and "what to do" in `components.SignatureBadge`.
- API: `DELETE /api/user/{gpg_keys,ssh_signing_keys}/{id}` soft-removes (idempotent); new `POST …/{id}/revoke` and `POST …/{id}/restore` (restore needs the step-up); `GET` returns `status` (`active` | `removed` | `revoked`), `removed_at`, `revoked_at`, and lists removed keys only with `?include=removed`. Session-only throughout; HTMX variants as for ticket 03.
- Settings → Signing keys: revoked keys stay in the main list with a red **Revoked** badge and no actions; removed keys sit behind a "Removed keys (n)" disclosure with **Restore** and **Revoke**. Remove confirm: "Commits made before today stay Verified; later commits signed with this key won't. If the key is compromised, use Revoke instead." Revoke confirm: "All commits signed with this key, past and future, will show Unverified. This can't be undone." Toasts "Signing key removed" / "revoked" / "restored" / "updated".
- Docs: `docs/signed-commits.md` (Keys, expiry and removal; reasons table with `key_removed`; the "revocation is read when the key is added" sentence is replaced), `docs/api-reference.md`, `docs/access-control.md` if the session-only wording lists endpoints.

## Not in scope

- Fetching or refreshing keys from a keyserver; notifying anyone on revoke.
- Storing verification results (decision 4); a trusted timestamp for committer dates.
- Add-time ownership proof and anything about key squatting: that is ticket 09.
- Un-revoking a key.

## Files (hotspots in bold)

- **`internal/service/signing_key_service.go`**, **`internal/service/signature_service.go`**, **`internal/service/gpg_key_parse.go`**, `internal/service/signing_key_audit.go` (if audit actions live apart)
- **`internal/store/gpg_key_store.go`**, **`internal/store/ssh_signing_key_store.go`**, `internal/store/signing_key_lookup.go`, `internal/store/export_data_store.go`
- `internal/db/migrations/132_signing_key_removal.sql` (new), `internal/model/signing_key.go`
- **`internal/handler/signing_key_handler.go`**, `internal/router/router.go`
- **`internal/view/fragments/signing_keys.templ`**, `internal/view/components/signature_badge.templ`, `internal/view/viewmodels_settings.go`, `internal/view/pages/fragment_parity_test.go`
- `mockups/account_settings.html`, `mockups/settings.html`, `mockups/commit.html`, `mockups/commits.html`, `mockups/pr_commits.html`, `mockups/COVERAGE.md` (the mock-only states line), `.scratch/2026-11-06-security-signing-secrets/settings.html`, `commit.html`, `mock.html`
- `docs/signed-commits.md`, `docs/api-reference.md`

Shared with 09: `signing_key_service.go`, `gpg_key_store.go`, `signing_key_handler.go`, the Signing keys view-model. Whichever merges second reconciles; 10 must not change add-time ownership rules, and the upsert must be reachable only by the owner. Re-check the migration number against 09.

## Acceptance criteria

- [ ] Removing a key keeps commits dated before `removed_at` Verified and makes later ones `unverified` (`key_removed`), for GPG, GPG signing subkeys and SSH; restoring flips them back with nothing to clear
- [ ] Revoking a key (GPG and SSH) makes all its commits `unverified` (`key_revoked`) regardless of committer date, including a key that was already removed; revoke cannot be undone and a revoked key cannot be restored or re-added
- [ ] Posting an updated GPG block for an owned fingerprint updates keys, subkeys, UID emails and expiry in place; a foreign fingerprint is `409`; an update never clears `revoked_at` / `removed_at`
- [ ] An uploaded revocation maps by reason on the primary key and per subkey: compromised or no reason → `key_revoked`; superseded or retired → date-bounded at the revocation time (table-driven tests with real revocation certificates made by native gpg, committed as testdata so CI needs no gpg)
- [ ] `gpgKeyState` no longer treats a stored revocation as date-blind; existing 02 cases still pass
- [ ] A page of 30 commits still does a constant number of key queries (removed and revoked keys included)
- [ ] `revoke-credentials` and account deletion still hard-delete signing keys; the data export carries `removed_at` and `revoked_at`
- [ ] Remove / Revoke / Restore / update are audit-logged, session-only (a PAT gets the same refusal as for add), and Restore needs the step-up
- [ ] Settings UI: removed-keys disclosure, Revoked badge, both confirm texts and toasts; commit popover shows the `key_removed` and `key_revoked` copy
- [ ] Checked at desktop 1440 in **light and dark** on a throwaway server (scratch Postgres, unique `CZ_AUTH_COOKIE_NAME`, `admin@example.test` / `cloudzilla-seed`); evidence for both themes in the PR
- [ ] The standing mocks in `mockups/` listed under Files are updated in the same PR (classes copied from the templ, `COVERAGE.md` / `index.html` where affected) and checked under `make mockups` in both themes
- [ ] The scratch mock beside the spec (`commit.html`, `settings.html`, `mock.html`) matches the shipped page, with the new states
- [ ] `make generate-templ` re-run and the `_templ.go` files committed; `docs/signed-commits.md` and `docs/api-reference.md` updated and `npm run fmt:check` passes (not run on `.scratch/`)
- [ ] `make lint` and `go test ./...` pass; `make test-integration` passes for the new store queries and migration

## Verification

Unit and integration tests for the state matrix (active / removed / revoked × commit date before / after × GPG primary, GPG subkey, SSH).

Throwaway server: scratch Postgres on a free port (see `docs/testing.md`), a unique `CZ_AUTH_COOKIE_NAME`, sign in as `admin@example.test` / `cloudzilla-seed` (set `users.email_verified_at` first, as ticket 04 found). With native git over HTTP push signed commits with one GPG and one SSH key, then in order: Remove (old commits stay Verified, a commit with a later committer date reads `key_removed`), Restore, upload an updated GPG block with a new expiry, upload a block carrying a "superseded" and then a "compromised" revocation, Revoke the SSH key. Screenshots of the Signing keys list (with the Removed disclosure open and a Revoked row), both confirm dialogs, and the commit popover for `key_removed` and `key_revoked`, in light and dark at 1440.

## Comments

Claude, 2026-11-13: Settled in a decision session; no product code was written. Findings behind it: `parseGPGKey` refuses a revoked primary key (`gpg_key_parse.go:42`), so only subkey `revoked_at` is ever stored, and `gpgKeyState` (`signature_service.go:419`) ignores its date. go-crypto's own `revoked()` treats a compromised-reason revocation as revoked before its date, which is the Revoke semantics here.
