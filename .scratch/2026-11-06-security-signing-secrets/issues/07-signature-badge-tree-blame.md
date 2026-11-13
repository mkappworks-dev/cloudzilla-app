# Signature badge on the tree and blame

Created: 2026-11-13
Category: enhancement
Status: ready-for-agent
Blocked by: 04
Parallel with: 05, 06

| Branch | Worktree | PR title | Effort |
| --- | --- | --- | --- |
| `feat/phase-17.2-signature-badge-tree-blame` | `.worktrees/feat+phase-17.2-signature-badge-tree-blame` | `feat(code): signature badge on tree and blame (17.2)` | M |

Spec: [spec.md](../spec.md). Parent: `../../2026-11-06-uiux-desktop-9.5/issues/16-signed-commits.md`.

## Problem

ROADMAP promises the badge on tree and blame. The tree's last-commit row and blame's per-hunk commit cell show a commit but not its signature.

## Scope

- Fill `SignatureResult` for the tree's latest-commit summary and for each distinct commit in a blame view (one batched `Verify` call per page).
- Render `components.SignatureBadge` in the tree header's last-commit row and in the blame gutter; in blame, show the badge once per hunk, only icon-sized, with the same popover.
- Keep blame performance: verify distinct commits only, not each line.

## Files (hotspots in bold)

- **`internal/service/code_service_commit.go`**, `internal/service/code_service_tree.go` / `code_service_blame.go` (whichever builds those views)
- `internal/view/pages/tree.templ`, `blame.templ`, `internal/view/viewmodels_repo.go`, `internal/handler/repo_page_handler.go`
- `mockups/tree.html`, `mockups/blame.html`

## Acceptance criteria

- [ ] The tree's last-commit row and blame hunks show the badge and popover for each status; unsigned shows nothing
- [ ] Blame on a 1000-line file with 40 distinct commits does one batched key lookup (test)
- [ ] Popovers inside blame do not clip or scroll the table
- [ ] Checked at desktop 1440 in **light and dark** on a throwaway server (scratch Postgres, unique `CZ_AUTH_COOKIE_NAME`, `admin@example.test` / `cloudzilla-seed`); evidence for both themes in the PR
- [ ] The standing mocks in `mockups/` listed under Files are updated in the same PR (classes copied from the templ, `COVERAGE.md` / `index.html` where affected) and checked under `make mockups` in both themes
- [ ] The scratch mock beside the spec (`commit.html`, `settings.html`, `mock.html`) matches the shipped page
- [ ] `make generate-templ` re-run and the `_templ.go` files committed; `make lint` and `go test ./...` pass

## Verification

Throwaway server: scratch Postgres on a free port (see `docs/testing.md`), a unique `CZ_AUTH_COOKIE_NAME`, sign in as `admin@example.test` / `cloudzilla-seed`. Open a tree and a blame of a file touched by one commit per status; both themes at 1440.

## Comments

