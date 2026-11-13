# Signed commits and secret scanning mock (ROADMAP 17.2–17.3)

Two features, one mock. Commits get a signature badge that opens a popover with the signer and key. Account settings
gain a **Signing keys** section for GPG and SSH keys. Repositories get a **Security** tab that lists secret scanning
alerts, an alert page that can be closed with a reason, and push protection that rejects pushes adding a secret. Classes
come from `badge.templ` (success, outline, warning, destructive, secondary), the commits and commit pages, the account
settings key rows and dialog, `dropdown_menu.templ`, `alert.templ`, `timeline.templ`, and the blob line table.

Open with `python3 -m http.server` from the repo root at `/.scratch/2026-11-06-security-signing-secrets/mock.html`.
Rebuild CSS with
`bin/tailwindcss -i .scratch/2026-11-06-security-signing-secrets/mock-input.css -o .scratch/2026-11-06-security-signing-secrets/mock.css`.
`shell.js` has no Security tab, so repo pages swap in a copy of its subnav with one added before Settings. Screenshots are
in `shots/` (gitignored, not committed).

## Pages and states

- `mock.html`: commit history with **Verified** (SSH, GPG), **Partially verified**, **Unverified** and unsigned (no
  badge) rows. States open each popover.
- `commit.html`: commit header with the badge after the SHA and its popover open. States: `verified`, `verified-gpg`,
  `partial`, `unverified`, `unsigned`.
- `settings.html`: account settings with "Signing keys" in the sidebar. GPG keys show the key ID, fingerprint, each
  email with a verified or "not on your account" badge, subkey and expiry, plus an expired key. SSH signing keys show
  whether the key is also an auth key. The add dialog switches between GPG and SSH. States: `keys`, `empty`, `add-gpg`,
  `add-ssh`.
- `security.html`: open and closed alerts (secret type, masked hint, file:line, commit, author, detected, close reason),
  a type filter, the empty state, and `push`: the `git push` terminal output when push protection rejects a push.
- `alert.html`: masked code snippet at the commit, "How to fix", activity timeline, and an aside with pattern, all
  locations and whether the secret is still on the default branch. A "Close as" menu offers Revoked, False positive and
  Used in tests; a closed alert offers Reopen. The `blocked` state lets a writer allow a pushed secret once.

## ROADMAP vs mock

| ROADMAP proposes                                                                | Mock assumes                                                                                                                                         |
| ------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------- |
| `SignatureStatus`: `verified`, `unverified`, `unsigned`                         | Adds `partially_verified`: valid signature from the account's key, but the committer email isn't one of the account's verified emails               |
| Verify with keys already in `ssh_keys`                                          | A separate signing use: an SSH key is added as auth, signing or both (`ssh_keys.usage`, or `ssh_signing_keys`)                                      |
| `gpg_keys`, `GET/POST/DELETE /api/user/gpg_keys`                                | Same, plus `gpg_key_emails` and subkeys; web route `/settings#signing-keys`                                                                         |
| `repositories.verify_commits BOOLEAN`, described as a cache                     | Dropped by [spec.md](./spec.md): verify on read, no cache. A cache wanted `commit_signatures (repo_id, sha, status, key_id, signer_id)`; `verify_commits` reads like "require signed commits"         |
| `secret_scan_alerts.state` in `open`, `resolved`, `false_positive`               | `state` `open`/`closed` plus `resolution` `revoked`/`false_positive`/`used_in_tests`, `resolved_by`, `resolved_at`, and an `alert_locations` table    |
| Scan after push; `GET /{owner}/{repo}/security/secrets`                          | Also push protection: pre-receive rejection, plus closing the alert (`POST …/security/secrets/{n}/allow`) is the allowance; no timer, see [spec-secret-scanning.md](./spec-secret-scanning.md)               |
| Owners notified per finding                                                     | Repo admins notified; Cloudzilla tokens found in a push are revoked automatically ("Auto-revoked" badge)                                            |

## Open questions

- ~~Is "Partially verified" worth a fourth status?~~ Yes; see [spec.md](./spec.md) decision 1.
- ~~Should web edits and merges be signed by an instance key?~~ Yes, with the instance as committer; see [spec.md](./spec.md) decision 3 and [ADR 0002](../../docs/adr/0002-instance-signs-server-made-commits.md). The mock gains a "Verified by Cloudzilla" popover state and an admin "Commit signing" section in tickets 04 and 05.
- ~~The alert page masks the secret in the snippet it reads from the commit. Should writers be able to reveal it?~~ No; see [spec-secret-scanning.md](./spec-secret-scanning.md) decision 11.
- ~~Push protection isn't in 17.3. Is it in scope, and should the allowance need write access or the pusher's own
  confirmation?~~ In scope; any writer allows by closing the alert, with no timer (decisions 2, 3).
- ~~Should the Security tab show to readers with a count only, or stay hidden from them, as the mock assumes?~~ Hidden (decision 11).
- ~~Auto-revoking Cloudzilla tokens is a new behaviour. Should it notify the token owner?~~ Yes, in-app and mail (decisions 5, 12).
