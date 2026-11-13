# Sign server-made commits as the instance

Created: 2026-11-13
Category: enhancement
Status: ready-for-agent
Blocked by: 05
Parallel with: 03, 04, 07

| Branch | Worktree | PR title | Effort |
| --- | --- | --- | --- |
| `feat/phase-17.2-sign-server-commits` | `.worktrees/feat+phase-17.2-sign-server-commits` | `feat(signing): instance-signed server-made commits (17.2)` | L |

Spec: [spec.md](../spec.md). Parent: `../../2026-11-06-uiux-desktop-9.5/issues/16-signed-commits.md`.

## Problem

Merges, squashes, web edits and wiki saves are unsigned and have the user as committer (ADR 0002 changes that).

## Scope

- One helper that takes a built `object.Commit`, sets `Committer` to `Cloudzilla <noreply@<noreplyHost>>` (author unchanged), signs `EncodeWithoutSignature` with `InstanceSigner`, sets `PGPSignature` and stores the object. Without a key (`ErrNoSecretKey`) the commit is stored unsigned but still has the instance committer.
- All eight sites use it: `ThreeWayMergePullRequest`, `UpdatePullBranch`, `SquashMergePullRequest`, `ApplySuggestions` (`code_service_merge.go`); the commit helper behind `CommitFile`, `editFile`, `DeleteFile` (`code_service_files.go`); the two wiki commits (`code_service_wiki.go`); the repository initial commit (`repo_service_create.go`). Fast-forward merges create no commit and are unchanged.
- Audit what read the committer of these commits as the user: contributor stats, `ResolveCommitAuthors`, the stats backfill, notification and event authors, blame, activity, the PR commits tab. They must keep crediting the author; add tests where a reader used `Committer`.
- Mirrors and imports are upstream's commits and are not touched.

## Files (hotspots in bold)

- **`internal/service/code_service_merge.go`**, **`internal/service/code_service_files.go`**, `internal/service/code_service_wiki.go`, `internal/service/repo_service_create.go`
- `internal/service/commit_email.go` (`GitAuthor` / instance committer), `internal/service/server_commit.go` (new helper)
- `internal/service/contributor_stats_service.go`, `internal/service/stats_backfill.go`, `internal/service/event_service.go` (only if they read `Committer`)

## Acceptance criteria

- [ ] After each of the eight actions, native `git verify-commit` with an `allowed_signers` file holding the instance public key succeeds, and Cloudzilla's own verifier says `verified` / kind `instance`
- [ ] Author is the acting user and committer is `Cloudzilla <noreply@host>` on every one; contributor stats, the Contributors page and activity still credit the user (tests)
- [ ] With no `security.secret_key` the same actions succeed, commits are unsigned, and the instance committer is still set
- [ ] A commit made before this ticket still has committer = user and is still unsigned
- [ ] The merge-conflict, branch-protection and ref-update behaviour of the touched functions is unchanged (existing tests pass)
- [ ] `make lint` and `go test ./...` pass; `make test-integration` passes for the new store queries
- [ ] Verified once against a throwaway server (scratch Postgres, unique `CZ_AUTH_COOKIE_NAME`, `admin@example.test` / `cloudzilla-seed`) with real `curl`/native git, not only unit tests
- [ ] Commit pages show "Verified by Cloudzilla" for a web edit and a squash merge, in light and dark at 1440, with the standing mocks already updated by ticket 04

## Verification

Throwaway server: scratch Postgres on a free port (see `docs/testing.md`), a unique `CZ_AUTH_COOKIE_NAME`, sign in as `admin@example.test` / `cloudzilla-seed`. Edit a file on the web, squash-merge a PR, save a wiki page, then clone and run `git log --show-signature` and `git verify-commit` with the allowed signers; open the commit pages in light and dark at 1440.

## Comments

Claude, 2026-11-13: this is the only ticket that touches commit-creating code other in-flight tickets (edit-and-release workflows) also edit. Rebase onto `origin/main` before starting and keep the helper change mechanical.
