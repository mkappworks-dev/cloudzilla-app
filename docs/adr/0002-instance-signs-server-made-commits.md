# 2. The instance signs server-made commits and is their committer

Status: accepted, 2026-11-13

## Context

Merges, squashes, web edits and wiki saves are written by the server with the User as both author and committer, so
they carry no signature and can never read as verified. Signing them with a key the User does not hold would claim the
User signed something they did not.

## Decision

Every server-made commit has the User as author and `Cloudzilla <noreply@<host>>` as committer, and is signed with an
instance signing key (Ed25519, SSHSIG, namespace `git`). The popover names the instance as the signer. This is GitHub's
model for web commits.

- The private key is sealed with `internal/secretbox` under `security.secret_key`; an admin can rotate it. Retired
  public keys stay trusted, so commits signed before a rotation stay Verified.
- Contributor stats, "authored by" lines and notifications read the author, so credit is unchanged. Anything that read
  the committer of these commits as the User must be checked.
- Losing or changing `security.secret_key` loses the private key: new server-made commits need a freshly generated key,
  and the retired public key keeps the old ones Verified.

## Considered options

Leaving server-made commits unsigned was cheaper but meant a squash-merged PR, the commit that lands on the default
branch, always showed no badge. Signing with the User as committer was rejected as a false claim.
