# Signature badge and popover on commit pages

Created: 2026-11-13
Category: enhancement
Status: done
Blocked by: 02
Parallel with: 03, 05

| Branch | Worktree | PR title | Effort |
| --- | --- | --- | --- |
| `feat/phase-17.2-signature-badge` | `.worktrees/feat+phase-17.2-signature-badge` | `feat(commits): signature badge and popover (17.2)` | M |

Spec: [spec.md](../spec.md). Parent: `../../2026-11-06-uiux-desktop-9.5/issues/16-signed-commits.md`.

## Problem

Commits show no trust signal. The mock draws a badge after the SHA that opens a popover with the signer and key.

## Scope

- `components.SignatureBadge(result)` and its popover: `Verified` (success), `Partially verified` (outline), `Unverified` (warning), nothing for `unsigned`. Popover content per the spec's UI section and the mock's `commit.html` states (`verified`, `verified-gpg`, `partial`, `unverified`, `unsigned`), including the "what to do" line and the footer link.
- New popover state **Verified by Cloudzilla** (signer "Cloudzilla", "made on the web and signed by this server"), not drawn yet: add it to `commit.html` / `mock.html` first, then build it. Tickets 05 and 06 produce such commits; this ticket renders any `kind == "instance"` result.
- Used on `commits.templ` (list row), `commit.templ` (header, after the SHA) and `pr_commits.templ`.
- Popover is server-rendered (no fetch), keyboard operable: the badge is a `button` with `aria-expanded`, Escape and outside click close it, focus returns to the button.

## Files (hotspots in bold)

- `internal/view/components/signature_badge.templ` (new), **`internal/view/viewmodels_repo.go`** (and `viewmodels_pull.go`)
- **`internal/view/pages/commits.templ`**, `commit.templ`, `pr_commits.templ`, `internal/handler/repo_page_handler.go`, `pull_tabs_page_handler.go`
- `mockups/commits.html`, `mockups/commit.html`, `mockups/pr_commits.html`, `.scratch/2026-11-06-security-signing-secrets/commit.html`, `mock.html`

## Acceptance criteria

- [x] All five mock states render from real data; `unsigned` shows no badge and no empty space
- [x] The popover opens with Enter/Space and a click, closes with Escape and outside click, and never clips at the right edge of the list at 1440
- [x] The instance state ("Verified by Cloudzilla") renders and is added to both mocks
- [x] A page of 30 commits makes no extra queries per row beyond ticket 02's batch (assert in a handler test)
- [x] Checked at desktop 1440 in **light and dark** on a throwaway server (scratch Postgres, unique `CZ_AUTH_COOKIE_NAME`, `admin@example.test` / `cloudzilla-seed`); evidence for both themes in the PR
- [x] The standing mocks in `mockups/` listed under Files are updated in the same PR (classes copied from the templ, `COVERAGE.md` / `index.html` where affected) and checked under `make mockups` in both themes
- [x] The scratch mock beside the spec (`commit.html`, `settings.html`, `mock.html`) matches the shipped page
- [ ] `make generate-templ` re-run and the `_templ.go` files committed; `make lint` and `go test ./...` pass

## Verification

Throwaway server: scratch Postgres on a free port (see `docs/testing.md`), a unique `CZ_AUTH_COOKIE_NAME`, sign in as `admin@example.test` / `cloudzilla-seed`. Push one commit per state (GPG, SSH, hardware-key partial, unknown key, unsigned) with native git; open the commits list, a commit and a PR's commits tab; screenshots with each popover open, light and dark at 1440.

## Comments

Claude, 2026-11-13: Left unticked: `make lint` (golangci-lint is not installed here; `gofmt` is clean). `GetCommit` no longer verifies, because post-receive stats call it; pages use `GetVerifiedCommit`. The PR commits list lost `overflow-hidden`, which clipped the popover on the last rows. The "Verified by Cloudzilla" state is covered by a component test and the mocks only: nothing signs with the instance key until ticket 05.

