# Signed commit verification (ROADMAP 17.2)

Created: 2026-11-13
Category: enhancement
Status: ready-for-agent

Mock: [README.md](./README.md) and the pages beside it. Parent ticket: `../2026-11-06-uiux-desktop-9.5/issues/16-signed-commits.md`.
Secret scanning (17.3, ticket 17) shares this folder and is specced later: see
[For ticket 17](#for-ticket-17-secret-scanning).

## Problem

A commit can carry a `gpgsig` header (GPG, or SSH as `gpg.format ssh` writes it), and Cloudzilla ignores it. A reviewer
reading `Merge pull request #12` or a contributor's commit cannot tell whether the account they see actually made it:
anyone can set `user.email` to `malith@example.com` and push. ROADMAP 17.2 plans a "Verified" badge and a GPG key list,
with no UI and no place for SSH signing keys.

Example: Rhea signs with her YubiKey (registered on her account) and commits as `rhea@contractor.dev`, an address she
never verified. Today the commit page shows nothing. After this spec it shows a **Partially verified** badge whose
popover says "verify rhea@contractor.dev to make this Verified".

## What exists today (read, not assumed)

- **go-git exposes the signature.** `object.Commit.PGPSignature` holds the `gpgsig` header whatever its format (go-git
  v5.19.2, `plumbing/object/commit.go`); `EncodeWithoutSignature` returns the signed bytes; `Commit.Verify(armoredKeyring)`
  verifies PGP only, via `ProtonMail/go-crypto` (an indirect dependency today). SSH signatures are not verified by go-git.
- **An SSHSIG verifier exists.** `verifySSHSig(pub, namespace, message, blob, touch)` in
  `internal/service/request_signature.go` checks an SSHSIG blob (sha256/sha512, hardware keys, `no-touch-required`)
  for signed API requests. Commit signatures use the same format with namespace `git`; the commit side only has to
  unarmor `-----BEGIN SSH SIGNATURE-----` first.
- **SSH keys** are rows of `ssh_keys` (migration 007): `public_key`, MD5 `fingerprint` (unique, also checked against
  `deploy_keys`), looked up on every SSH handshake. Added through `POST /api/user/keys` with a password + TOTP step-up.
- **Verified emails.** `User.EmailVerified()` for the primary address; `user_emails` (migration 125) for additional ones,
  `UserEmail.Verified()`; `userByAuthorEmail` also resolves the noreply address
  `<id>+<username>@users.noreply.<host>`.
- **Where commits render.** `CommitSummary`/`CommitLog` (`GetCommits`) and `CommitDetail` (`GetCommit`) in
  `internal/service/code_service_commit.go`; pages `commits.templ`, `commit.templ`, `pr_commits.templ`, `tree.templ`,
  `blame.templ`.
- **Server-made commits** set `Committer` to the author's signature at eight sites: `code_service_merge.go`
  (`ThreeWayMergePullRequest`, `UpdatePullBranch`, `SquashMergePullRequest`, `ApplySuggestions`),
  `code_service_files.go` (the commit helper under `CommitFile`, `editFile`, `DeleteFile`), `code_service_wiki.go` (two)
  and the repository's initial commit in `repo_service_create.go`.
- **Sealing secrets.** `internal/secretbox` encrypts under `security.secret_key` (mirror tokens use it).
- **Account surfaces that list keys:** the data export (`export_service.go`, `ssh_keys.json`), the admin
  `revoke-credentials` action, and `docs/access-control.md`'s rule that a PAT cannot add SSH keys.

## Decisions

Settled with the user on 2026-11-13, except those marked *derived*.

1. **Four statuses:** `verified`, `partially_verified`, `unverified`, `unsigned` (the mock's fourth status). A valid
   signature from an account's key whose email check fails is `partially_verified`.
2. **Signing keys are separate from SSH auth keys.** New tables; `ssh_keys`, its fingerprint lookup and the deploy-key
   rule are untouched. The same public key may be added as both, each use separately.
3. **Instance signing is in scope** ([ADR 0002](../../docs/adr/0002-instance-signs-server-made-commits.md)): every
   server-made commit has the User as author and `Cloudzilla <noreply@<host>>` as committer and is signed by an
   instance key. Credit (author) is unchanged.
4. **Verify on read, no cache.** No `repositories.verify_commits` column and no `commit_signatures` table. A key added,
   removed, expired or revoked changes every affected commit at the next render. Key lookups are batched per page.
5. **Signing key rules.** Adding one needs the same step-up as `POST /api/user/keys` (password + TOTP), writes an audit
   entry, and is session-only (a PAT cannot add one). A key (GPG fingerprint or SSH SHA256 fingerprint) belongs to one
   account instance-wide.
6. **Email rule.** The committer email must be among the owner's verified emails (primary if verified, verified
   additional emails, the noreply address). For a GPG key it must also be a UID email of the key; a key UID email that
   is not verified on the account is listed as "not on your account" and does not count.
7. **Expiry and removal.** A key expired *at the commit's committer date* is `unverified` (`key_expired`); a commit
   signed before expiry stays Verified. Git lets the committer set that date, so "signed before expiry" is forgeable:
   a knowingly accepted cost. A deleted or revoked key makes its commits `unverified`. GPG subkeys verify when they
   carry the signing flag.
8. **Surfaces:** commits list, commit page, PR commits tab, then the tree's last-commit row and blame. No new commit
   JSON endpoint; only key CRUD under `/api/user`.
9. *Derived:* the repository initial commit is a server-made commit and is signed too. Old server-made commits (committer
   = user) are not rewritten or backfilled.
10. *Derived:* with no `security.secret_key` the instance key cannot be sealed, so server-made commits still get the
    instance committer but stay unsigned (a startup warning, and the admin page says why). The key is generated on first
    use once the secret key is set.
11. *Derived:* account deletion cascades the user's signing keys, so their commits fall to `unverified`.
    `revoke-credentials` also deletes signing keys; the data export includes them (public parts).
12. *Derived:* a commit with more than one signature block, an unparseable signature, or an algorithm we do not support
    is `unverified` with a reason, never an error.
13. **Key squatting** (2026-11-13). Registration needs no proof of possession, so a squatter can hold a victim's key. A
    valid signature whose committer email is verified on a *different* account than the key owner is `unverified` with
    reason `other_user`, naming no signer; if the email is verified nowhere it stays `partially_verified` (accepted
    gap). The 409 on add tells the same account from another account. Proof of possession, several accounts per key and
    an admin reassign feature are declined; the admin runbook is `revoke-credentials`. See
    [ticket 09](./issues/09-signing-key-squatting.md).

## Design

### Schema

Re-check the next free number on `origin/main` when a ticket starts (129 is the highest as of 3332dc7e). The first
ticket takes the next number and the instance-key ticket the one after; whichever merges second renumbers.

```sql
-- 130_create_signing_keys.sql
CREATE TABLE gpg_keys (
    id          BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    user_id     BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    key_id      TEXT NOT NULL,               -- 16 hex, the primary key's long ID
    fingerprint TEXT NOT NULL UNIQUE,        -- 40 hex, primary key
    public_key  TEXT NOT NULL,               -- armored block, as pasted (primary + subkeys + UIDs)
    expires_at  TIMESTAMPTZ,
    revoked_at  TIMESTAMPTZ,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX idx_gpg_keys_user ON gpg_keys(user_id);
CREATE TABLE gpg_subkeys (
    id          BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    gpg_key_id  BIGINT NOT NULL REFERENCES gpg_keys(id) ON DELETE CASCADE,
    key_id      TEXT NOT NULL UNIQUE,
    fingerprint TEXT NOT NULL UNIQUE,
    can_sign    BOOLEAN NOT NULL,
    expires_at  TIMESTAMPTZ,
    revoked_at  TIMESTAMPTZ
);
CREATE TABLE gpg_key_emails (               -- UID emails, lower-cased; "verified" is computed on read
    gpg_key_id BIGINT NOT NULL REFERENCES gpg_keys(id) ON DELETE CASCADE,
    email      TEXT NOT NULL,
    PRIMARY KEY (gpg_key_id, email)
);
CREATE TABLE ssh_signing_keys (
    id          BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    user_id     BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    title       TEXT NOT NULL,
    public_key  TEXT NOT NULL,
    fingerprint TEXT NOT NULL UNIQUE,        -- SHA256:..., what ssh-keygen -l prints
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX idx_ssh_signing_keys_user ON ssh_signing_keys(user_id);

-- 131_create_instance_signing_keys.sql
CREATE TABLE instance_signing_keys (
    id                 BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    public_key         TEXT NOT NULL,
    fingerprint        TEXT NOT NULL UNIQUE,
    private_key_sealed BYTEA NOT NULL,       -- secretbox, purpose "instance-signing"
    created_at         TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    retired_at         TIMESTAMPTZ
);
CREATE UNIQUE INDEX idx_instance_signing_keys_active ON instance_signing_keys ((TRUE)) WHERE retired_at IS NULL;
```

The SSH signing fingerprint is SHA256, not `ssh_keys`' MD5: they are separate tables with separate lookups, and SHA256 is
what `ssh-keygen -l` and users compare against.

### Verification (`SignatureService`)

`Verify(ctx, commits []*object.Commit) (map[plumbing.Hash]SignatureResult, error)`; one batched key lookup per call.

1. No `PGPSignature` → `unsigned` (no badge).
2. Armor starts `-----BEGIN PGP SIGNATURE-----` → read the issuer key ID from the signature packet; starts
   `-----BEGIN SSH SIGNATURE-----` → unarmor and read the public key from the blob; else `unverified` (`unsupported`).
3. Look the key up: instance keys (active and retired) first, then `gpg_keys`/`gpg_subkeys` by key ID, or
   `ssh_signing_keys` by fingerprint. Not found → `unverified` (`no_key`).
4. Verify the signature over `EncodeWithoutSignature`: GPG through `Commit.Verify` with the stored armored key, SSH
   through `verifySSHSig(pub, "git", payload, blob, touch=false)` (a signing hardware key's touch flag is not required;
   `ssh-keygen -Y sign` of a commit may be made with `no-touch-required`). Failure → `unverified` (`bad_signature`).
5. Key state at the committer date: expired → `unverified` (`key_expired`); revoked → `unverified` (`key_revoked`).
6. Instance key → `verified`, signer "Cloudzilla".
7. User key: committer email rule from decision 6. Pass → `verified`; fail → `partially_verified` with reason
   `email_not_verified` (or `email_not_on_key` for GPG when the email is verified on the account but absent from the key).

`SignatureResult` carries status, reason, key kind (`gpg`, `ssh`, `instance`), key label, fingerprint/key ID, signer
username, and the committer email. Templates never touch go-git types.

### Instance signing

`InstanceSigner` owns the active key: generates Ed25519 on first need, seals the private half with `secretbox` (purpose
`instance-signing`), exposes `Sign(commit) (armoredSSHSig string, err)` and `Rotate(ctx)`. Rotation retires the active
row and generates a new one; retired public keys stay trusted for verification. All eight commit sites go through one
helper that sets the instance committer and attaches the signature; `go-git`'s `CommitOptions.SignKey` is PGP-only, so
the helper builds the commit object, signs `EncodeWithoutSignature`, sets `PGPSignature` and stores it itself. Native
`git verify-commit` with an `allowed_signers` file proves the format in tests.

Admin surface: a "Commit signing" section on `/admin/settings` shows the active key's fingerprint and public key
(copyable, so operators can add it to an `allowed_signers` file) and a **Rotate key** button behind the existing
confirmation step-up.

### UI

- **Badge + popover** (`components.SignatureBadge`): `Verified` (success), `Partially verified` (outline), `Unverified`
  (warning), nothing when unsigned. The popover is server-rendered into the page (no fetch on click), closes on
  Escape/outside click, and has the signer, key type, key label, fingerprint and the "what to do" line from the mock.
  A new popover state, **Verified by Cloudzilla** ("This commit was made on the web and signed by this server"), is not
  in the mock and is added to it in the first UI ticket.
- **Settings → Signing keys** (`/settings#signing-keys`): GPG list (key ID, fingerprint, emails with verified / "not on
  your account", subkeys, expiry, an Expired badge), SSH signing list ("also an auth key" tag when the fingerprint is
  in `ssh_keys`), the add dialog switching GPG/SSH, the empty state.
- **Routes:** `GET/POST/DELETE /api/user/gpg_keys`, `GET/POST/DELETE /api/user/ssh_signing_keys` (session-only,
  step-up on POST), HTMX variants as for SSH keys.

## Slicing

| Ticket | Slice | Blocked by | Parallel with |
| ------ | ----- | ---------- | ------------- |
| [01](./issues/01-signing-keys.md) | Tables, services and API for GPG and SSH signing keys | — | — |
| [02](./issues/02-signature-verifier.md) | `SignatureService.Verify`, `SignatureStatus` on commit models | 01 | 03 |
| [03](./issues/03-signing-keys-settings-ui.md) | Account settings "Signing keys" section + mock | 01 | 02, 04, 05 |
| [04](./issues/04-signature-badge-commit-pages.md) | Badge and popover on commits, commit and PR commits pages | 02 | 03, 05 |
| [05](./issues/05-instance-signing-key.md) | Instance signing key, admin section, verifier trust | 02 | 03, 04 |
| [06](./issues/06-sign-server-made-commits.md) | Instance committer and signature on the eight server-made commit sites | 05 | 03, 04, 07 |
| [07](./issues/07-signature-badge-tree-blame.md) | Badge on the tree's last-commit row and blame | 04 | 05, 06 |
| [08](./issues/08-signing-docs-and-e2e.md) | Docs, ROADMAP rewrite, native-git end-to-end on a throwaway server | 01–07 | — |
| [09](./issues/09-signing-key-squatting.md) | `other_user` reason, clearer 409 for a taken key | — | 10 |

Shared-file conflicts: 02 and 05 both edit `signature_service.go`, which is why 05 waits for 02; 04 and 07 both edit view
models (07 waits). Migrations 130/131 collide with anything else landing first: re-check at build.

## Testing

Unit and integration tests per ticket. Fixtures are real signatures made by native `git` (`gpg --armor` keys generated
in the test with a temp `GNUPGHOME`, and `ssh-keygen -Y sign` / `git -c gpg.format=ssh`), committed as small testdata
objects so CI needs neither gpg nor ssh-keygen. Table-driven verifier cases cover every status and reason: unsigned,
unparseable, two signature blocks, unknown key, wrong key (key of another account, same ID collision refused at add),
tampered message, expired-then-signed and signed-then-expired, revoked, GPG UID-email mismatch, SSH hardware key,
email unverified, email verified, noreply email, deleted key, instance key active and retired. See
[testing](../../docs/testing.md) for what CI runs.

## Verification

Every UI ticket is checked on a throwaway server: scratch Postgres on a free port, a unique `CZ_AUTH_COOKIE_NAME` per
`127.0.0.1` server (the browser cookie jar is shared across ports), signed in as `admin@example.test` /
`cloudzilla-seed`, at desktop 1440 in **light and dark** (set `cz-theme` in localStorage; emulate dark before the
first check). The standing mocks in `mockups/` change in the same PR as the page and are checked under `make mockups`
in both themes. Signed commits for the checks come from native git against the throwaway server (HTTP with a token).

## Out of scope

- Requiring signed commits (branch protection, `verify_commits` as an enforcement flag).
- A commit JSON API exposing `verification`; webhook payload fields.
- Backfilling or re-signing old server-made commits; signing tags.
- Key expiry notifications; S/MIME and x509 signatures.
- Instance key in a KMS or HSM; automatic rotation.

## For ticket 17 (secret scanning)

Ticket 17 gets its own spec and tickets in this folder. What it inherits and must not break:

- **Mock folder.** `alert.html`, `security.html`, `mock.html`'s Security subnav and `mock-input.css` belong to 17. The
  signing tickets update `commit.html`, `settings.html` and `mock.html`'s commit states and leave 17's pages alone. The
  README's "Open questions" 3–6 are 17's to settle; 1–2 are settled here.
- **Push path.** Signing never reads or writes in the push path: verification is on read. Secret scanning's post-receive
  scan and pre-receive push protection live in `internal/service/repo_service_push.go` and the receive-pack handlers
  (`git_http.go`, `internal/ssh/server.go`); nothing in 01–08 edits them.
- **Server-made commits.** The instance committer and signing in ticket 06 touch the same commit-creating code that
  secret scanning must also cover if it scans merges and web edits; 17 should scan the stored commit, not a pre-signing
  copy.
- **Migrations.** 130 and 131 are taken by this spec; 17's `secret_scan_alerts` migration takes the next free number at
  build time. ROADMAP's `117_create_secret_scan_alerts.sql` is stale.
- **Shared docs.** `docs/ROADMAP.md` phase 17.2/17.3 rows and the Security entries in CLAUDE.md's subsystem list are
  edited by 08 (signing) and by 17's docs ticket: rebase onto each other.
- **Nav.** The repo `Security` tab is 17's; signing adds no repo-level tab.

## Comments

Claude, 2026-11-13: Questions 1 and 2 of the mock README (fourth status; instance-signed web commits) are answered by
decisions 1 and 3 above.

Claude, 2026-11-13: Decision 13 and ticket 09 answer signing-key squatting; options weighed and declined are in the ticket.
