# Signing-key squatting: `other_user` and a clearer 409

Created: 2026-11-13
Category: bug
Status: ready-for-agent
Blocked by: none
Parallel with: 10

| Branch | Worktree | PR title | Effort |
| --- | --- | --- | --- |
| `fix/signing-key-squatting` | `.worktrees/fix+signing-key-squatting` | `fix(signing): stop a squatted key attributing commits to its squatter (17.2)` | M |

Spec: [spec.md](../spec.md) (decisions 5, 6 and 13). Builds on tickets 01, 02 and 04, all done.

## Problem

A signing key belongs to one account instance-wide (decision 5) and adding one needs no proof that you hold the private
key. Mallory registers Rhea's public key first. Rhea then cannot add it (`409 this key is already registered`, which
says nothing about why), and every commit Rhea signs as `rhea@corp.dev`, an address verified on her account, matches
Mallory's key, fails the email check and renders **Partially verified, signed by @mallory**.

GitHub and GitLab both enforce one account per key and, as far as we know, ask for no proof of possession either.
GitHub shows such commits as Unverified and never names the squatter. GitLab has a separate `other_user` status.

## Decision

Settled with the user on 2026-11-13 (spec decision 13).

- **`other_user`.** When a valid signature's key owner fails the email rule and the committer email is verified on a
  *different* account, the result is `unverified` with reason `other_user`, not `partially_verified`. `SignerUsername`
  and `KeyLabel` stay empty (the squatter is never named); the fingerprint or key ID stays so the real owner recognises
  their key.
- **Accepted gap.** If the committer email is verified on no account (an unverified address, or a person not on this
  instance) `other_user` cannot fire and the result stays `partially_verified` naming the key owner. It is
  indistinguishable from the legitimate case in the spec (Rhea, `rhea@contractor.dev`), and the popover already says the
  email is unverified on that account. No viewer-dependent popover.
- **Split the 409.** The same account adding its own key again: "this key is already on your account". Another account
  holds it: "this key is registered to another account on this instance; if it is yours, ask an administrator to remove
  it". The message names nobody. The status stays `409`.
- **No new admin feature.** The existing `revoke-credentials` (or account deletion) frees a squatted fingerprint; a
  runbook goes in `docs/signed-commits.md`.

Rejected, with the trigger to revisit:

| Option | Why not | Revisit when |
| --- | --- | --- |
| Proof of possession on add (`ssh-keygen -Y sign` / `gpg --clearsign` of a server challenge) | Challenge store, two verify paths, dialog UX; fails for touch-required hardware keys and keys on another machine; neither forge asks for it; with `other_user` the damage is cosmetic | Squatting is seen in the wild |
| Same key on several accounts, resolved by committer email | Reverses decision 5 and the `UNIQUE` fingerprint; with unique verified emails it mostly reproduces `other_user` at far higher cost | — |
| Admin "reassign key" feature | Needs its own audit and notification design; the existing admin actions already free the key | Admins report `revoke-credentials` is too blunt: add a "remove one signing key" action then |

## Scope

- `SignatureService.Verify`: after the email check fails with `email_not_verified`, look up which accounts own the
  failing commits' committer emails in **one batched query per call**, run only when such commits exist. If the owner is
  another account than the key's, the result is `unverified` / `other_user` as above. `email_not_on_key` is untouched
  (the email is verified on the key owner's own account). The noreply address resolves as `userByAuthorEmail` does
  (`internal/service/commit_email.go`), including one minted under an earlier base URL.
- New `ReasonOtherUser`. The badge popover for it (`signature_badge.templ`): headline "Unverified"; "This commit is
  signed with a key registered to a different account than the one that owns `<committer email>`."; what to do: "If the
  key is yours, ask an administrator to remove it from the other account, then add it in Settings → Signing keys."; the
  fingerprint, no username.
- `SigningKeyService.AddGPGKey` / `AddSSHKey`: on `ErrSigningKeyTaken`, decide whether the adding user already holds the
  key and return a distinct error; the handler (`signingKeyError`) maps the two to the messages above.
- `docs/signed-commits.md`: the `other_user` row in the reasons table; a "Key squatting" section with the accepted gap,
  the rejected alternatives and the admin runbook (find the holder with a SQL lookup on `gpg_keys` / `gpg_subkeys` /
  `ssh_signing_keys`, then `revoke-credentials` on the account, or delete the account when it is not a real user; say
  what `revoke-credentials` also wipes); update the `409` line under Signing keys. `docs/api-reference.md` if it quotes
  the 409 text.

## Files (hotspots in bold)

- **`internal/service/signature_service.go`** (shared with ticket 10), `internal/service/user_service_signing.go` or
  `commit_email.go` (the email-owner lookup), `internal/store/user_store.go` (the batched query)
- `internal/service/signing_key_service.go`, `internal/store/gpg_key_store.go`, `internal/store/ssh_signing_key_store.go`,
  `internal/handler/signing_key_handler.go`
- **`internal/view/components/signature_badge.templ`** (+ regenerated `_templ.go`), `signature_badge_test.go`
- **`docs/signed-commits.md`** (reasons table, shared with ticket 10)
- `mockups/signature-badge.js`, `mockups/commit.html` and the other `mockups/` pages that draw a badge state,
  `.scratch/2026-11-06-security-signing-secrets/commit.html`, `mock.html`

## Acceptance criteria

- [ ] Table-driven cases in `internal/service`: key owned by Mallory, committer email verified on Rhea's account →
      `unverified` / `other_user`, no signer, no key label, fingerprint kept; same for an SSH key and a GPG key; the noreply
      address of another account; committer email verified on no account → still `partially_verified` /
      `email_not_verified`; committer email verified on the key owner's own account → unchanged; `email_not_on_key` unchanged
- [ ] A page of 30 commits makes a constant number of queries: the email-owner lookup is one query per call and none when
      no commit needs it (asserted in a test)
- [ ] Adding a key that another account holds returns `409` with the "another account" message; adding your own key twice
      returns `409` with the "already on your account" message; neither names an owner (handler tests, JSON and HTMX)
- [ ] The popover renders `other_user` with no username and no key label; a component test covers it
- [ ] The standing mock `mockups/signature-badge.js` and every `mockups/` page that draws badge states gain the
      `other_user` state, classes copied from the templ, `COVERAGE.md` / `index.html` where affected, and are checked
      under `make mockups` in light **and** dark at desktop 1440
- [ ] The scratch mock beside the spec (`commit.html`, `mock.html`) gains the same state and matches the shipped page
- [ ] `docs/signed-commits.md` updated as listed under Scope
- [ ] `make generate-templ` re-run and the `_templ.go` files committed; `make lint` and `go test ./...` pass;
      `make test-integration` passes for the new store query

## Verification

Throwaway server: scratch Postgres on a free port (see `docs/testing.md`), a unique `CZ_AUTH_COOKIE_NAME`, sign in as
`admin@example.test` / `cloudzilla-seed`. Create a second account (Mallory) and register a native-git SSH signing key on
it; sign a commit with that key as the admin with a verified email, push it over HTTP with a token, and open the commit
page: expect **Unverified** with the `other_user` popover and no username. Try to add the same key as the admin and as
Mallory and read both `409` messages. Push a commit with an unverified email on a key you own and confirm it stays
**Partially verified**. Screenshots with the popover open in light and dark at desktop 1440, for the real page and the
mock under `make mockups`.

## Comments

Claude, 2026-11-13: Decided with the user in a grilling session. The 409 split costs one extra lookup on a rare failing
path. Ticket 10 (key revocation, parallel session) also edits `signature_service.go` and the reasons table in the docs:
whichever merges second rebases those two spots.
