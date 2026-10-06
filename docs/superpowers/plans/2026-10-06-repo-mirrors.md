# Repository Pull Mirrors Implementation Plan

> **For agentic workers:** Steps use checkbox (`- [ ]`) syntax for tracking. Work test-first: write the failing test, watch it fail, implement, watch it pass, commit.

**Goal:** An import can be kept in sync with its upstream as a read-only pull mirror. Archived repos are read-only on every path that writes git content.

**Architecture:**
- One predicate, `service.CheckContentWritable`, guards every git-content write.
- Credentials are sealed by `internal/secretbox` under `security.secret_key`.
- `repo_mirrors` rows are claimed with `FOR UPDATE SKIP LOCKED` plus a lease by a single `MirrorService.Run` loop.
- Each sync fetches into the live repo through the import's SSRF guard and `WrapForReceive`, then runs the machine-only post-push side effects.

**Tech stack:** Go, chi v5, go-git v5, PostgreSQL, Templ.

**Spec:** `.scratch/repo-mirrors/spec.md`, with tickets in `.scratch/repo-mirrors/issues/`.

## Global constraints

- Branch `feat/repo-mirrors`, worktree `.worktrees/feat+repo-mirrors`.
- Integration tests need `TEST_DATABASE_DSN='postgres://cloudzilla:test@localhost:5433/cloudzilla_test?sslmode=disable'`, migrated. Without it, DB tests skip silently.
- Running as root breaks four `chmod`-based storer-failure tests in `gittransport`, `handler` and `ssh`. They fail on `main` too and are unrelated.
- Edit `.templ`, then `make generate-templ`. Never hand-edit `*_templ.go`.
- Write comments only for a *why* the code can't show.
- Commit with `git add <paths>`, never `.claude/`. Use a Conventional Commits subject, and end the message with `Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>` only.
- Before each commit, run `go build ./...`, `go vet ./...`, `golangci-lint run ./...` and the touched packages' tests with the DSN set.
- Before the migration commit, recheck the next migration number against `origin/main`.
- Tickets 06–08 build the UI agreed in the [mockups](https://claude.ai/artifact/1qdQ3E9jfTCzmijzWhWRgq): banner option A, the settings section as drawn, and a fixed list of interval choices.

## Ticket 01: content-writable guard

| File | Change |
| --- | --- |
| `internal/model/repo.go`, `internal/service/repo_service.go` | `ContentReadOnly`, `ErrRepoArchived` and `CheckContentWritable`. `UpdateGeneral` refuses a default-branch change on a read-only repo. |
| `internal/handler/handler.go` | `contentWritableRepoJSON`: `writableRepoJSON` plus the guard, answering 403 `{"error":"repository is archived"}` |
| `internal/handler/ref_handler.go` | All four handlers use `contentWritableRepoJSON` |
| `internal/handler/pull_handler.go` | Merge checks the guard; `tryAutoMerge` returns early |
| `internal/handler/pull_line_comment_handler.go` | `ApplySuggestion` uses `contentWritableRepoJSON` |
| `internal/handler/release_handler.go` | `CreateRelease` uses `contentWritableRepoJSON` |
| `internal/handler/page_repo_handler.go` | Maps the guard error to 403 |
| `git_http.go`, `ssh/server.go`, `repo_files_handler.go`, `profile_readme_handler.go` | Call the guard instead of `IsArchived` |
| `internal/view/...` | Hide the write controls when the repo is read-only |
| `internal/handler/archived_writes_test.go` (new) | Table test across every path |

- [x] Write the table test: archived repo × {branch create and delete, tag create and delete, merge, apply suggestion, release, default-branch change}. Each case expects a 403 and unchanged refs or HEAD. Run it and watch it fail.
- [x] Add the predicate and helper, and wire the paths. Run the test and watch it pass.
- [x] Hide the UI controls and regenerate templ.
- [x] Run lint and tests, then commit `fix(repo): refuse git writes to archived repositories on every path`.

## Ticket 02: secretbox

- [x] `internal/secretbox` tests: round trip; wrong key, wrong purpose, tampering and unknown version give `ErrUndecryptable`; short key; empty key gives a nil box.
- [x] Implement it: HKDF-SHA256 from the standard library's `crypto/hkdf`, AES-256-GCM, version byte `0x01`, purpose as AAD.
- [x] Config `security.secret_key` with validation, and the docs row.
- [x] Commit `feat(security): add secret_key and secretbox for credentials at rest`.

## Ticket 03: schema, store, config

- [x] Migration `NNN_repo_mirrors.sql`, the model, and `MirrorStore` with an integration test: concurrent claims don't overlap, an expired lease can be reclaimed, backoff is applied.
- [x] `Repository.IsMirror` scanned in every `RepoStore` SELECT, with a test.
- [x] The `mirror.*` config with defaults and validation.
- [x] Commit `feat(mirror): add repo_mirrors schema, store and config`.

## Ticket 04: sync (done)

- [x] `MirrorService.Sync`: guarded fetch with prune, HEAD follow, ref diff and machine side effects, tested against `testutil.ServeGitHTTP`. go-git never requests thin packs, so no storer wrapper is needed (a guard test pins that).

## Ticket 05: scheduler (done)

- [x] `Run`/`Wake`/`SyncNow`, a lease of the timeout plus 1m, started from `main.go` when `mirror.enabled`.

## Ticket 06: import creates a mirror (done)

- [x] `ImportRequest.Mirror`/`MirrorInterval`, sealed token, mirror row in the import's publish step, the form's sync card, `repo.mirror.create` audit.

## Ticket 07: read-only mirrors (done)

- [x] `ContentReadOnly` covers mirrors, `ErrRepoMirror` and `PushRefusal`, PR creation refused, option A banner and badge, Sync now endpoint.

## Ticket 08: settings and API (done)

- [x] `GET`/`PATCH`/`DELETE /api/repos/{owner}/{repo}/mirror` (needs `CanManage`; `repo:admin` for tokens), the settings section from frame 2, and update/delete audit entries.

## Ticket 09

These follow the ticket files. Expand this plan before starting each one.
