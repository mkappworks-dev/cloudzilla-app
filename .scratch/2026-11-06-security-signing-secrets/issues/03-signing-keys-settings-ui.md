# Settings: Signing keys section

Created: 2026-11-13
Category: enhancement
Status: ready-for-agent
Blocked by: 01
Parallel with: 02, 04, 05

| Branch | Worktree | PR title | Effort |
| --- | --- | --- | --- |
| `feat/phase-17.2-signing-keys-ui` | `.worktrees/feat+phase-17.2-signing-keys-ui` | `feat(settings): signing keys section (17.2)` | M |

Spec: [spec.md](../spec.md). Parent: `../../2026-11-06-uiux-desktop-9.5/issues/16-signed-commits.md`.

## Problem

Users have no place to add the keys that sign their commits. The mock draws a **Signing keys** section in account settings with GPG and SSH lists and an add dialog.

## Scope

- Sidebar entry and section `#signing-keys` in `settings.templ`, from `settings.html` in this folder: GPG key rows (key ID, fingerprint, each UID email with a verified or "not on your account" badge, subkey and expiry, Expired badge), SSH signing rows ("also an auth key" tag when the fingerprint's key is in `ssh_keys`), the add dialog switching GPG/SSH, the empty state.
- Add / delete through HTMX against the ticket 01 endpoints, with the `ConfirmFields` step-up as the SSH key dialog; fragments in `internal/view/fragments/` as `ssh_keys.templ` does; toasts "Signing key added" / "removed".
- Delete confirmation text: removing a key turns its commits Unverified.
- Mock states `keys`, `empty`, `add-gpg`, `add-ssh` stay in sync with the page.

## Files (hotspots in bold)

- **`internal/view/pages/settings.templ`**, `internal/view/fragments/signing_keys.templ` (new), `internal/view/viewmodels_settings.go`
- `internal/handler/settings_page_handler.go`, `internal/view/pages/fragment_parity_test.go`
- `mockups/account_settings.html`, `mockups/settings.html`, `mockups/COVERAGE.md` (if affected), `.scratch/2026-11-06-security-signing-secrets/settings.html`

## Acceptance criteria

- [ ] Sections, empty state, both lists and the add dialog match the mock; GPG add shows the parse error inline; a duplicate shows the `409` message
- [ ] Adding then removing a key works with HTMX, with the step-up fields, and keeps focus order sensible (dialog focus trap, Escape closes)
- [ ] The "also an auth key" tag shows when the SSH fingerprint is also in `ssh_keys`
- [ ] Checked at desktop 1440 in **light and dark** on a throwaway server (scratch Postgres, unique `CZ_AUTH_COOKIE_NAME`, `admin@example.test` / `cloudzilla-seed`); evidence for both themes in the PR
- [ ] The standing mocks in `mockups/` listed under Files are updated in the same PR (classes copied from the templ, `COVERAGE.md` / `index.html` where affected) and checked under `make mockups` in both themes
- [ ] The scratch mock beside the spec (`commit.html`, `settings.html`, `mock.html`) matches the shipped page
- [ ] `make generate-templ` re-run and the `_templ.go` files committed; `make lint` and `go test ./...` pass

## Verification

Throwaway server: scratch Postgres on a free port (see `docs/testing.md`), a unique `CZ_AUTH_COOKIE_NAME`, sign in as `admin@example.test` / `cloudzilla-seed`. Add a GPG and an SSH signing key, one SSH key that is also an auth key, an expired GPG key (testdata), remove one. Screenshots of `keys`, `empty` and the open dialog in light and dark at 1440.

## Comments

