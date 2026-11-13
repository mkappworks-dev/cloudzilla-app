# Signed commits

Cloudzilla reads the signature on a commit and badges it. It never enforces anything: an unsigned or unverified commit
pushes and merges like any other. There are two sources of signed commits: a person signing with a GPG or SSH key
registered on their account, and the server signing the commits it writes itself with an instance key
([ADR 0002](./adr/0002-instance-signs-server-made-commits.md)).

## Statuses

Verification is done on read by `SignatureService.Verify` (`internal/service/signature_service.go`). Nothing is cached
and there is no per-repository switch: adding, removing, expiring or revoking a key changes every affected commit at its
next render. A page verifies all of its commits in a constant number of queries: one per key kind, plus one for the key
owners.

| Status               | Badge              | Meaning                                                                                                  |
| -------------------- | ------------------ | -------------------------------------------------------------------------------------------------------- |
| `verified`           | Verified           | A registered key made a valid signature and the committer email passes the [email rule](#the-email-rule) |
| `partially_verified` | Partially verified | The signature is valid and the key is on an account, but the email rule fails                            |
| `unverified`         | Unverified         | The commit is signed but cannot be trusted, for the `reason` below                                       |
| `unsigned`           | none               | No `gpgsig` header                                                                                       |

An `unverified` or `partially_verified` result carries one reason. The popover turns it into a sentence and a "what to
do" line.

| Reason                | Status               | When                                                                                      |
| --------------------- | -------------------- | ----------------------------------------------------------------------------------------- |
| `email_not_verified`  | `partially_verified` | The committer email is not one of the key owner's verified emails                         |
| `email_not_on_key`    | `partially_verified` | GPG only: the email is verified on the account but is not a UID email of the key          |
| `no_key`              | `unverified`         | No account or instance key matches the signature (also: the key's owner no longer exists) |
| `bad_signature`       | `unverified`         | A key matched but the signature does not verify, for example a tampered message           |
| `key_expired`         | `unverified`         | The key had expired at the commit's committer date                                        |
| `key_revoked`         | `unverified`         | The key, or the signing subkey, is revoked                                                |
| `multiple_signatures` | `unverified`         | The header holds more than one signature block                                            |
| `unsupported`         | `unverified`         | Not a PGP or SSH signature, unparseable, or an issuer or algorithm we cannot read         |
| `unavailable`         | `unverified`         | The key lookup failed (logged); reloading usually clears it                               |

A commit that cannot be judged is `unverified` with a reason, never an error. A failed key lookup is logged and shows as
`unavailable`.

A commit signed by an instance key is `verified` with the signer "Cloudzilla" and key kind `instance`. The email rule
does not apply: its committer is the instance, not an account.

### The email rule

A valid signature from an account's key is `verified` only when the committer email is one of that account's verified
emails: the primary address if verified, the verified additional emails (`user_emails`), and the noreply address
`<id>+<username>@users.noreply.<host>` (a noreply address minted under an earlier base URL also counts). For a GPG key
the email must also be a UID email of the key. A UID email that is not verified on the account is listed under Settings
as "not on your account" and does not count. An SSH key carries no emails, so only the account check applies.

The committer email is read, not the author email, because that is the one git matches against the signer.

### Keys, expiry and removal

- A GPG signature is matched by its issuer key ID (the issuer subpacket, else the tail of the issuer fingerprint) to a
  stored primary key or to a subkey with the signing flag. The signature itself is checked with the stored armored key.
- An SSH signature is matched by the SHA256 fingerprint of the public key inside the SSHSIG blob (namespace `git`) and
  verified with the stored key. The hardware-key touch flag is not required, because `ssh-keygen -Y sign` can be told to
  skip it, so a `no-touch-required` signature verifies.
- Expiry is judged at the commit's committer date, not at the time of viewing: a commit signed before its key expired
  stays Verified. A deleted key makes its commits `unverified` (`no_key`); a revoked one `key_revoked`. A subkey is also
  bounded by its own expiry and revocation. Revocation is read when the key is added: a key revoked afterwards keeps
  verifying until its owner removes it or adds the updated key.
- **Accepted cost.** The committer date is set by the committer, so "signed before expiry" can be forged: whoever holds
  an expired key can back-date a commit and it will read Verified. Closing this needs a trusted timestamp, which a
  signed commit does not have. We accept it.

## Signing keys

Signing keys are separate from SSH authentication keys. `ssh_keys`, its MD5 fingerprint lookup on every handshake and
the deploy-key uniqueness rule are untouched, and the same SSH public key may be registered as both: a signing key is
never a way to push, and an auth key never makes a commit Verified. See
[git-transport](./git-transport.md#ssh-key-management).

Migration `130_create_signing_keys.sql`:

| Table              | Holds                                                                                                                            |
| ------------------ | -------------------------------------------------------------------------------------------------------------------------------- |
| `gpg_keys`         | One row per primary key: `key_id` (16 hex), `fingerprint` (40 hex, unique), the armored `public_key`, `expires_at`, `revoked_at` |
| `gpg_subkeys`      | Subkeys of a GPG key: `key_id`, `fingerprint`, `can_sign`, `expires_at`, `revoked_at`                                            |
| `gpg_key_emails`   | The key's UID emails, lower-cased; whether each is verified is computed on read                                                  |
| `ssh_signing_keys` | `title`, `public_key`, SHA256 `fingerprint` (unique), the form `ssh-keygen -l` prints                                            |

Rules, enforced by `SigningKeyService` and the handlers in `internal/handler/signing_key_handler.go`:

- A key belongs to one account instance-wide: a fingerprint (or a subkey ID) already registered anywhere is a `409`.
- Adding needs the same step-up as `POST /api/user/keys` (password, plus a TOTP or email code where the account has
  them) and is audit-logged (`user.gpg_key.add`, `user.ssh_signing_key.add` and their `.delete` twins). Deleting needs
  no step-up.
- The endpoints are session-only: a personal access token cannot add or delete a signing key. See
  [access-control](./access-control.md#token-scopes).
- A GPG key that is private, garbage, already expired, already revoked or has no signing-capable key is refused.
- Account deletion cascades the account's signing keys, so their commits fall to `unverified`. The admin action
  `revoke-credentials` deletes them too, and the [data export](./api-reference.md) includes their public parts
  (`ssh_signing_keys.json`).

Routes are listed in [api-reference](./api-reference.md#signing-keys): `GET/POST/DELETE /api/user/gpg_keys` and
`/api/user/ssh_signing_keys`, with HTMX variants for the Settings page. The UI is **Settings → Signing keys**
(`/settings#signing-keys`): the GPG list with each UID email marked "Verified email" or "not on your account", the SSH
list (tagged when the same key is also an auth key), and an add dialog that switches between the two.

## Where the badge shows

The badge and its server-rendered popover (`components.SignatureBadge`) appear on the commits list, the commit page, the
pull request commits tab, the tree's last-commit row and blame. There is no commit JSON endpoint exposing the result and
no webhook field. The popover shows the signer, key kind, key label, fingerprint or key ID, and what to do next; its
"About signing keys" link goes to Settings → Signing keys.

## Instance signing

Merges, squashes, branch updates, suggestion commits, web file edits and deletes, wiki saves and a repository's initial
commit are written by the server. Each of them (`serverCommitter.store` in `internal/service/server_commit.go`) keeps
the acting user as **author** and sets the **committer** to `Cloudzilla <noreply@<host>>`, where host comes from
`server.base_url`, then signs the commit with the instance key. Contributor stats, "authored by" lines and notifications
read the author, so credit is unchanged. Older server-made commits, whose committer is the user, are not rewritten or
backfilled and stay unsigned.

The key is Ed25519, signed as SSHSIG with namespace `git` (`InstanceSigner`, `internal/service/instance_signer.go`). Its
private half is sealed with `internal/secretbox` under `security.secret_key` (purpose `instance-signing`). The key is
generated the first time something needs to sign, not at startup, and a concurrent first use converges on one row. The
`instance_signing_keys` table (migration `131_create_instance_signing_keys.sql`) allows one active row at a time.

### Rotation

**Admin → Settings → Commit signing** (`/admin/settings#commit-signing`) shows the active key's fingerprint and public
key and a **Rotate key** button (`POST /api/admin/signing-key/rotate`, superadmin, behind the identity confirmation
step-up, audit action `admin.signing_key.rotate`). Rotation retires the active row and creates a new one. Retired public
keys stay trusted for verification, so commits signed before a rotation stay Verified. Rotation is manual; there is no
automatic schedule.

### Verifying with git: `allowed_signers`

An operator can check a server-made commit with native git. Copy the public key from the admin page and put it in an
`allowed_signers` file, prefixed by the committer address and a space:

```
noreply@git.example.com ssh-ed25519 AAAAC3NzaC1lZDI1NTE5...
```

```sh
git config gpg.ssh.allowedSignersFile ~/.config/git/allowed_signers
git verify-commit main
# Good "git" signature for noreply@git.example.com with ED25519 key SHA256:...
```

After a rotation add the new key as another line; keep the old lines for old commits.

### With no `security.secret_key`

Without the key there is nowhere safe to keep the private half. The server logs a startup warning, server-made commits
still get the instance committer but are stored **unsigned**, and the admin page says why. Nothing blocks a merge or an
edit. Setting the key and restarting is enough: the signing key is then generated on first use.

Losing or changing `security.secret_key` makes the stored private key unreadable (the admin page shows "sealed under a
different key"). New server-made commits stay unsigned, with a log warning, until an admin rotates the key. The old
public key stays trusted, so the commits it signed stay Verified.

## What the end-to-end run saw

Checked with native git against a throwaway server (`security.secret_key` set), a repository created with a README and a
branch of commits pushed over HTTP, then squash-merged.

| Commit                                                                | Status                  | Reason               |
| --------------------------------------------------------------------- | ----------------------- | -------------------- |
| GPG, committer email is a UID of the key and verified                 | `verified`              | --                   |
| SSH, verified email                                                   | `verified`              | --                   |
| SSH, committer `rhea@contractor.dev` (not on the account)             | `partially_verified`    | `email_not_verified` |
| GPG, committer is the account's noreply address, not a UID of the key | `partially_verified`    | `email_not_on_key`   |
| SSH, key registered nowhere                                           | `unverified`            | `no_key`             |
| GPG, key expiring in a day, committer date three days ahead           | `unverified`            | `key_expired`        |
| Plain commit                                                          | `unsigned`              | --                   |
| Repository's initial commit                                           | `verified` (Cloudzilla) | --                   |
| Web squash merge                                                      | `verified` (Cloudzilla) | --                   |

`git verify-commit` with the instance key in `allowed_signers` accepts the squash-merge commit. A hardware-key SSH
signature made with `no-touch-required` could not be produced on the test machine (no security key); the unit tests in
`internal/service` cover it with a fixture.

## Related

- [access-control](./access-control.md): signing key endpoints are session-only with a step-up; `revoke-credentials`
- [git-transport](./git-transport.md): signing keys are not authentication keys
- [api-reference](./api-reference.md#signing-keys): the endpoints
- [configuration](./configuration.md#secret-key): `security.secret_key`
