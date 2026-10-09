# Issue Closing Keywords Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** "Fixes #12" (and `owner/repo#12`) in a PR's title, body or merged commits, or in a commit fast-forwarded onto the default branch, links and closes the issue, with an issue timeline entry, webhook, notification and activity event.

**Architecture:** A pure parser (`ParseClosingRefs`) feeds two consumers: `PullService` keeps `source = 'keyword'` rows in `pull_issue_links` in step with the PR text, and `IssueCloser` closes issues as an actor after a merge (`h.mergePull`, shared by `UpdatePull` and `tryAutoMerge`) or a default-branch fast-forward push (both transports). Closing is a per-issue transaction that claims an `issue_events` row, so a PR or commit closes an issue at most once.

**Tech Stack:** Go, chi v5, go-git v5, PostgreSQL, Templ.

**Spec:** `.scratch/2026-10-06-issue-closing-keywords/spec.md`

## Global Constraints

- Work in `/home/user/cloudzilla-app` on branch `feat/issue-closing-keywords`.
- Integration tests need `TEST_DATABASE_DSN='postgres://cloudzilla:test@localhost:5433/cloudzilla_test?sslmode=disable'`. Without it DB tests skip silently, so always set it.
- Edit `.templ` files, then run `make generate-templ`. Never hand-edit `*_templ.go`.
- Comments only for a *why* the code can't show, one line by default.
- Commit with `git add <specific paths>`. Never stage `.claude/`. Conventional Commits subject; the message ends with `Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>` and nothing else.
- Before each commit: `go build ./...`, `go vet ./...`, and the touched packages' tests with the DSN set.
- The migration takes the next free number at commit time; re-check `origin/main` and open PRs first.

---

## File Structure

| File | Responsibility |
| --- | --- |
| `internal/db/migrations/1NN_issue_closing_keywords.sql` (new) | `pull_issue_links.source`, `issue_events`, `pull_requests.auto_merge_by` |
| `internal/model/issue_event.go` (new) | `IssueEvent` |
| `internal/model/access_token.go` | `TargetsCover(targets, owner, repo)` shared with `middleware.TargetAllows` |
| `internal/service/closing_keywords.go` (new) | `ClosingRef`, `ParseClosingRefs` |
| `internal/service/issue_closer.go` (new) | `IssueCloser`, `CloseActor`, `CloseForPull`, `CloseForPush`, `ResolveRefs` |
| `internal/store/issue_event_store.go` (new) | `ListByIssue`, `Record`, `CloseClaim` |
| `internal/store/issue_store.go`, `pull_store.go` | link `source`, keyword-link replace, cross-repo link readers, `auto_merge_by` |
| `internal/service/pull_service.go`, `issue_service.go` | keyword sync on create/edit, auto-merge arming user, `LinkedPRs` viewer |
| `internal/handler/pull_handler.go` | `h.mergePull`; `UpdatePull` and `tryAutoMerge` call it |
| `internal/handler/issue_handler.go` | records manual close/reopen events |
| `internal/handler/git_http.go`, `internal/ssh/server.go` | launch `CloseForPush` |
| `internal/view/pages/issue_detail.templ`, `fragments/linked_issues_sidebar.templ`, `fragments/issue_meta.templ` | issue timeline, cross-repo rows |
| `docs/pr-merge.md`, `docs/webhooks.md`, `docs/notifications.md` | behaviour |

---

## Task 1: Parser

- [ ] Table test `closing_keywords_test.go`: all nine keywords in mixed case, with and without `:`; `#N`, `owner/repo#N`; rejects `prefixes #1`, `Fixes#1`, `Fixes #1abc`, `Fixes #0`, `Fixes #99999999999`, the second ref in `Fixes #1, #2`; `Fixes #1, fixes #2` gives both; duplicates collapse in first-appearance order.
- [ ] Implement with one regexp; validate owner/repo with the existing name rules.
- [ ] Commit: `feat(issues): parse closing keywords`.

## Task 2: Migration, models, stores

- [ ] Migration per the spec. `issue_events(id, issue_id, actor_id, actor_name, event_type, pull_id, commit_sha, created_at)`; `actor_id` `ON DELETE SET NULL`-safe like `pull_events`; partial unique indexes `(issue_id, pull_id) WHERE pull_id IS NOT NULL AND event_type='closed'` and the same for `commit_sha`.
- [ ] `IssueEventStore`: `Record` (manual close/reopen), `ListByIssue` (joins the PR's number and repo for rendering), and `CloseClaim(ctx, issueID, actor, pullID, sha) (closed bool, err)`: one tx, `SELECT state … FOR UPDATE`, insert event `ON CONFLICT DO NOTHING`, update the issue.
- [ ] `IssueStore.LinkToPull` upserts `source='manual'`; `ReplaceKeywordLinks(pullID, issueIDs)` in one tx; `ListLinkedToPull` adds `readableBy` and returns owner/repo names; `PullStore.ListLinkedToIssue(issueID, viewer)`; `SetAutoMerge` takes the arming user.
- [ ] Store integration tests for the claim (second claim by same PR/commit is a no-op even after reopen), link upgrade/replace, and readability filtering.
- [ ] Commit: `feat(issues): store issue events and keyword links`.

## Task 3: IssueCloser

- [ ] `ResolveRefs(ctx, refs, contextRepo, viewerID)` → issues the viewer may read and see.
- [ ] `CloseForPull(ctx, actor, pr, repo, extraRefs)` closes linked issues and resolved commit refs; `CloseForPush(ctx, repo, actor, gitRepo, commands)` filters to a default-branch fast-forward and walks `commitRange` oldest first.
- [ ] Per issue: `CanWrite` on the issue's repo, not archived, `model.TargetsCover`; then `CloseClaim`; on success fire webhook, notification, activity event.
- [ ] Integration tests: same-repo and cross-repo close; no write, archived, targets miss; idempotent after reopen; side effects only for actual closes.
- [ ] Commit: `feat(issues): close issues from closing references`.

## Task 4: Keyword links on PR create/edit

- [ ] `PullService.Create`, `UpdateTitle`, `UpdateBody` call `syncKeywordLinks(pr, actorID)` (parse title+body, resolve as actor, `ReplaceKeywordLinks`).
- [ ] Tests: create links `keyword`; edit drops it; manual survives; manual link of keyword-linked issue upgrades; cross-repo only when readable.
- [ ] Commit: `feat(pulls): link issues named by closing keywords`.

## Task 5: `h.mergePull` and auto-merge

- [ ] Move checks, commit-ref collection (`CodeService.ClosingRefsInRange(owner, repo, base, headHash)` before the merge), merge, `SetState`, PR event, webhook, notification, activity, and `CloseForPull` into `h.mergePull`.
- [ ] `EnableAutoMerge` stores `auto_merge_by`; `tryAutoMerge` disarms when it's NULL or lacks write, else `h.mergePull` as that user with `autoMergeAuthor`.
- [ ] Handler tests: `ff`/`merge`/`squash` into default close linked + commit-referenced issues; into another branch closes none; auto-merge closes and records the merge event; disarm cases.
- [ ] Commit: `feat(pulls): close linked issues on merge`.

## Task 6: Push path

- [ ] HTTP: carry PAT targets in `gitUser`; launch `CloseForPush` when the pusher is a user. SSH: launch it for user pushes only.
- [ ] Integration tests with real pushes over HTTP and SSH: fast-forward to default closes; other branch, branch creation, force-push, deploy-key push close none.
- [ ] Commit: `feat(git): close issues from commits pushed to the default branch`.

## Task 7: Issue timeline and sidebars

- [ ] `UpdateIssue` records `closed`/`reopened` events.
- [ ] Issue page interleaves `issue_events` with comments by time; PR/issue sidebars render cross-repo rows as `owner/repo#N` without picker toggles.
- [ ] `make generate-templ`; render tests.
- [ ] Commit: `feat(issues): show close and reopen events on the issue page`.

## Task 8: Docs, review, PR

- [ ] Update `docs/pr-merge.md`, `docs/webhooks.md`, `docs/notifications.md`.
- [ ] Subagent review of the diff against the spec; fix findings.
- [ ] Tick the spec's acceptance criteria, `Status: done`.
- [ ] Open the PR: `feat(issues): close issues from closing keywords`.
