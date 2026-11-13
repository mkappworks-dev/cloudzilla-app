# Signature verifier and SignatureStatus on commit models

Created: 2026-11-13
Category: enhancement
Status: ready-for-agent
Blocked by: 01
Parallel with: 03

| Branch | Worktree | PR title | Effort |
| --- | --- | --- | --- |
| `feat/phase-17.2-signature-verifier` | `.worktrees/feat+phase-17.2-signature-verifier` | `feat(signing): verify commit signatures (17.2)` | L |

Spec: [spec.md](../spec.md). Parent: `../../2026-11-06-uiux-desktop-9.5/issues/16-signed-commits.md`.

## Problem

A commit's `gpgsig` header is never read. Nothing can say whether a commit is verified, so no page can show a badge.

## Scope

- `SignatureService.Verify` exactly as the spec's Verification section: unsigned, unsupported, no_key, bad_signature, key_expired, key_revoked, email_not_verified, email_not_on_key, multiple_signatures; `verified` / `partially_verified` / `unverified` / `unsigned`.
- Unarmor `-----BEGIN SSH SIGNATURE-----` and call `verifySSHSig(pub, "git", payload, blob, false)`; GPG through go-git's `Commit.Verify` with the stored armored key, picking the key by the signature packet's issuer ID (primary or signing subkey).
- Verified emails: a `UserService` method returning an account's verified addresses (primary if verified, verified additional emails, the noreply address). GPG also needs the key's UID emails.
- Batch key lookups: one query per kind for the whole page of commits, not one per commit.
- `SignatureResult` (status, reason, kind, key label, fingerprint / key ID, signer username, committer email) added to `CommitSummary` and `CommitDetail`; `GetCommits` and `GetCommit` fill it. The PR commits path (`code_service_pulls.go`) fills it too. No templates change in this ticket.
- A verify error for one commit never fails the page: it yields `unverified` with `unsupported`.

## Files (hotspots in bold)

- `internal/service/signature_service.go` (new), `internal/service/signature_ssh.go` (new), **`internal/service/request_signature.go`** (extract nothing it doesn't need; reuse `verifySSHSig`)
- **`internal/service/code_service_commit.go`**, `internal/service/code_service_pulls.go`, **`internal/service/services.go`**
- `internal/service/user_service.go` (verified emails), `internal/store/gpg_key_store.go`, `internal/store/ssh_signing_key_store.go` (batch lookups)
- testdata: small signed commit objects under `internal/service/testdata/signed/`

## Acceptance criteria

- [ ] Every status and reason in the spec's Testing list has a table-driven case using real signatures made by native git (gpg and `ssh-keygen -Y sign`), committed as testdata so CI needs neither tool
- [ ] A hardware-key (sk-) SSH signature and a signature by a GPG signing subkey verify
- [ ] Adding a key after the fact flips the same commit from `unverified` to `verified` with no cache to clear; deleting it flips it back
- [ ] A page of 30 commits does a constant number of key queries (asserted in a test)
- [ ] `GetCommits`, `GetCommit` and the PR commits path return a `SignatureResult` per commit
- [ ] `make lint` and `go test ./...` pass; `make test-integration` passes for the new store queries
- [ ] Verified once against a throwaway server (scratch Postgres, unique `CZ_AUTH_COOKIE_NAME`, `admin@example.test` / `cloudzilla-seed`) with real `curl`/native git, not only unit tests

## Verification

Unit and integration tests. Throwaway server: scratch Postgres on a free port (see `docs/testing.md`), a unique `CZ_AUTH_COOKIE_NAME`, sign in as `admin@example.test` / `cloudzilla-seed`. Create commits with native git signed by a GPG key and an SSH key, push over HTTP with a token, and read `GetCommits` results from a throwaway test that prints the statuses (delete it afterwards).

## Comments

