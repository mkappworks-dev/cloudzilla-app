# Mockgen harness, CI and three proof pages

Created: 2026-11-12
Category: enhancement
Status: done

| Branch         | Worktree                 | PR title                                              | Effort |
| -------------- | ------------------------ | ----------------------------------------------------- | ------ |
| `tech/mockgen` | `.worktrees/tech+mockgen` | `tech(mockups): generate mocks from templ (harness)` | L      |

## Problem

See [spec.md](../spec.md). Nothing renders mocks from the real components yet, and nothing stops them drifting.

## Scope

- `cmd/mockgen`: fixtures package (frozen clock, fixed names/avatars, shared builders), page registry (route, component,
  fixture, viewer, variants), renderer that calls the real `layout.Base` and strips htmx/Alpine scripts, resolves
  `assets.URL` to stable paths, and writes `mockups/generated/`.
- `mockups/mock-ui.js`: shared dropdown, dialog, tab and theme-toggle behaviour for generated pages.
- `mockups/server.js` serves `generated/` (extensionless routes); `Makefile` gets `mockups-generate`; `make mockups`
  still only serves.
- Generated `mockups/index.html`, and the generated region of `COVERAGE.md` between `mockgen` markers.
- Proof pages: `home`, `login` and `not_found` (new, ends the "no mock" row), with `anon`/`viewer`/`writer`/`admin`,
  admin-mode and `--empty`/`--error` variants where the templ branches. Hand mocks `home.html` and `login.html` are
  deleted.
- `mockups` job in `ci.yml`: Go from `go.mod`, the same action versions as the other jobs, regenerate, `git diff --exit-code`.
- `tests/mockgen-diff/`: local fidelity check against a throwaway server (see spec); documented in `docs/testing.md`.
- Docs: `mockups/README.md` (when to generate vs hand-write, how to add a page or variant), `COVERAGE.md`,
  `CLAUDE.md`'s `mockups/` line, `npm run fmt`.
- Any page that cannot render from a plain view-model is skipped and listed in `COVERAGE.md`; shipped code is not
  changed.

## Files

- `cmd/mockgen/**` (new), `mockups/generated/**`, `mockups/mock-ui.js`, `mockups/server.js`, `mockups/index.html`,
  `mockups/README.md`, `mockups/COVERAGE.md`, `Makefile`, `.github/workflows/ci.yml`, `tests/mockgen-diff/**`,
  `docs/testing.md`, `CLAUDE.md`

## Acceptance criteria

- [x] `go run ./cmd/mockgen` is deterministic: two runs produce no diff
- [x] `cmd/mockgen` is not linked into `cmd/server` (check `go list -deps ./cmd/server`)
- [x] Proof pages generated with their variants; `make mockups` serves them with no Go installed
- [x] CI job fails when generated output is stale (verified once with a deliberate change)
- [x] `tests/mockgen-diff` passes for the proof pages against a throwaway server
- [x] Proof pages checked at 1440 in light and dark under `make mockups`
- [x] README explains generate vs hand-write; `npm run fmt:check` passes
- [x] The PR summary mentions commits `80680841` (triage 32–40) and `cbe641b2` (run order and scores), kept in this PR

## Verification

Throwaway server (standalone postgres, `cz-admin migrate` and `seed`, unique port and `auth.cookie_name`), sign in as
`admin@example.test` / `cloudzilla-seed`, run `tests/mockgen-diff`, and screenshot each proof page in both themes.

## Comments
