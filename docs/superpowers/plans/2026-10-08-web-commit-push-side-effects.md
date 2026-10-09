# Web Commit Push Side Effects Implementation Plan

**Goal:** A commit made in the browser runs everything a push does: push webhook, activity event, closing keywords, contributor stats and PR head SHA, search re-index, dependency parse.

**Architecture:** A new `PushService` owns the post-receive block that `git_http.go` and `ssh/server.go` duplicated: `AfterPush(repo, gitRepo, actor, commands)`. `AfterWebCommit(repo, actor, RefUpdate)` builds the one push command for a web commit and calls it. `CodeService`'s single-commit writes return the `RefUpdate` they made; handlers pass it on after a successful commit.

**Spec:** [`.scratch/2026-10-06-web-commit-push-side-effects/spec.md`](../../../.scratch/2026-10-06-web-commit-push-side-effects/spec.md)

## Constraints

- Branch `fix/web-commit-push-side-effects`. Integration tests need `TEST_DATABASE_DSN`.
- A zero `CloseActor` means no human pusher (deploy key): no activity event, no issue closing.
- Mirror sync and the seeder keep their own variants (no actor, no events); not migrated here.

## Tasks

1. **PushService** (`internal/service/push_service.go`, ticket 01): move the block verbatim, wire into `Services`, switch HTTP and SSH to it. Test: webhook delivery recorded and activity event written for an actor; none of the activity for a zero actor.
2. **RefUpdate** (ticket 02): `commitOnto` returns it; `CommitFile`, `EditFile`, `DeleteFile` pass it up. Handlers (`SubmitNewFile`, `SubmitEditFile`, `DeleteFile`) call `AfterWebCommit` on success. Tests: service tests assert old/new tips (create for an empty repo); handler test asserts a push webhook delivery and activity event after each of new, edit, delete.
3. **README, suggestion, docs** (ticket 03): `commitSingleFile` and `ApplySuggestion` return it; handlers call it. Update `docs/code-browser.md`, `docs/webhooks.md`, `docs/git-transport.md`.

## Verification

`make lint`, `make test`, `make test-integration`.
