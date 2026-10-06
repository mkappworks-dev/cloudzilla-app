# Issue closing keywords

Created: 2026-10-06
Category: enhancement
Status: done

## Problem

A PR that says "Fixes #12" or a commit on `main` that says "Closes #12" does nothing in Cloudzilla. The issue stays open after the merge or push, and someone has to close it by hand. GitHub, GitLab and Gitea all close it, so people moving over expect it to work.

What's in the code today (checked on `origin/main` at 4d1e48d7, 2026-10-06):

- **Linking is manual only.** The PR sidebar's picker (`POST`/`DELETE /api/repos/{owner}/{repo}/pulls/{number}/linked-issues/{issueNumber}`) and the issue sidebar's picker (`POST`/`DELETE /api/repos/{owner}/{repo}/issues/{number}/linked-pulls/{pullNumber}`) write `pull_issue_links` (migration 060). Both need write access. Migration 060's comment says these links are "distinct from #N references parsed out of the PR body", but nothing parses the body: the code has no `#N` parser of any kind and no closing-keyword parser.
- **Merging closes nothing, linked or not.** `UpdatePull` (`internal/handler/pull_handler.go`), `tryAutoMerge` (same file) and `PullService.SetState` change only the PR. None of them reads `pull_issue_links` or touches an issue.
- **The seed fakes it.** `pullBody` (`internal/seed/activity.go:156`) writes "Closes #N" into a PR body and `seedPull` then links that issue by hand (`activity.go:206`). Merging the seeded PR leaves the issue open, and the seed closes issues independently at random (`activity.go:133`).
- **Issues have no timeline.** PRs have `pull_events` (migration 059), rendered as the PR timeline. The issue page shows only the body and comments. Neither manual closes nor reopens are recorded anywhere except `closed_at`.
- **Issue and PR numbers are separate sequences per repo** (`MAX(number)+1` over `issues` and over `pull_requests` respectively). Unlike GitHub, `#5` can be both issue 5 and PR 5. Closing keywords only make sense for issues, so `#N` after a keyword always means the issue.
- **Auto-merge (Phase 7.1) is a bare merge.** `tryAutoMerge` runs from `commit_status_handler.go` and `pull_review_handler.go`, merges and calls `SetState(merged)`. It records no `merged` timeline event and sends no `pull_request` webhook, notification or activity event. `EnableAutoMerge` takes the user's ID but doesn't store it, so nothing knows who armed it.
- **#165 pins the merged commit.** `UpdatePull` and `tryAutoMerge` resolve head to a commit, check that commit's statuses, and pass its hash to the merge method, which refuses with `ErrRefMoved` if head moved. Anything this feature reads from the head branch must use that same hash.
- **Squash drops commit messages.** The squash commit's message is always "Squash merge branch 'x' into 'y'" (`code_service_merge.go:485`), so a "Fixes #12" in a PR commit never reaches the default branch's history after a squash merge.
- **Server-side merges don't run push processing.** Merges (like web commits, applied suggestions and wiki edits) advance the branch through `gitref.Move`, not receive-pack, so `OnPostReceive` and the push webhook never see them.
- **Push processing is `RepoService.OnPostReceive`**, launched in a goroutine after each receive-pack over HTTP (`git_http.go:405`) and SSH (`ssh/server.go:276`), and called directly by the seed (`seed/repos.go:175`). It is given only the applied ref commands, not the pusher. It walks each branch's new commits with `forEachPushedCommit`. For a new branch that walk covers the branch's whole history.
- **`commits_ingested` (migration 073) can't serve as the "already processed" marker.** It's the contributor-stats claim table: it only holds commits whose author email maps to a user, and `RebuildRepoStats` (the stats backfill) deletes and refills it.
- **Pushers:** HTTP pushes always have a user (session or PAT). SSH deploy-key pushes have no user (`pusherName` stays empty) and already skip activity events for that reason.
- **Forks** are full copies (`copyDir`) with their own issues. There are no cross-repo PRs: a PR's head and base are branches of the same repo.
- **Opening a PR needs only read access** (`PullService.Create` checks `CanRead`). Editing a PR's title or body needs write access.
- **Issue visibility:** an issue can be `private`, visible to its author and to the repo's owners, admins and writers (`issueVisibleTo`).

- **Link readers assume one repo.** `PullStore.ListLinkedToIssue` (the issue sidebar) filters both the PR and the issue to the issue's repo and takes no viewer. `IssueStore.ListLinkedToPull` (the PR sidebar) filters by `issueVisibleTo` but not by whether the viewer can read the issue's repo. The unlink endpoints resolve the other side by number in the current repo. Manual links are also written by the new-issue form (`linked_pulls`, `page_issue_handler.go`) and the seed.
- **Token targets:** a `repo:admin` personal access token can be limited to some repos and orgs (`Claims.Targets`, migration 098), enforced on `/api/repos/...` routes by `middleware.TargetAllows`.

## Decisions

Agreed with the maintainer on 2026-10-06:

1. **Every linked issue closes on merge,** manual or keyword, as GitHub's "Development" links do. Existing manual links on open PRs start closing their issues when merged.
2. **Full cross-repo support:** `owner/repo#N` links and closes issues in other repos, subject to the read and write checks below.
3. **PR commits count:** keywords in the commit messages a merge brings in close issues too. They aren't shown as links before the merge.
4. **Auto-merge acts as the user who armed it** (new `pull_requests.auto_merge_by`) and gets the full merge side effects it lacks today.
5. **Only fast-forward pushes by a user** to the default branch close issues. Branch creation, force-pushes and deploy-key pushes don't.
6. **Issues get a timeline:** a new `issue_events` table, rendered with the comments, recording keyword closes and manual closes and reopens.

## Design

### Parsing

A pure function, `ParseClosingRefs(text string) []ClosingRef`, in `internal/service/closing_keywords.go`. `ClosingRef` is `{Owner, Repo string; Number int}`, with `Owner` and `Repo` empty for a bare `#N`.

- Keywords, case-insensitive: `close`, `closes`, `closed`, `fix`, `fixes`, `fixed`, `resolve`, `resolves`, `resolved`.
- Grammar: keyword at a word boundary, an optional `:`, whitespace, then a reference ending at a word boundary. `Fixes #12`, `fixes: #12`, `FIXES #12` match; `prefixes #12`, `Fixes#12` and `Fixes #12abc` don't.
- References: `#N` and `owner/repo#N`, where `owner` and `repo` follow the owner-name and repo-name rules (`ownerNameRe`, `validNameRe`). `N` is a positive number that fits in an `int32`; anything else is ignored.
- One keyword per reference, as on GitHub: `Fixes #1, fixes #2` closes both; `Fixes #1, #2` closes only #1.
- Callers resolve `owner/repo#N` naming the text's own repo (case-insensitively) to that repo, so it dedupes against `#N`. Duplicates are collapsed; first-appearance order is kept.
- Plain text: code spans and fences aren't special-cased (commit messages aren't Markdown).

### Resolving a reference

`resolve(ctx, ref, contextRepo, actor)` turns a `ClosingRef` into an issue:

- A bare `#N` means issue N in the context repo (the PR's repo, or the pushed repo). Issue and PR numbers are separate sequences, so it is always an issue.
- `owner/repo#N` means issue N in that repo, which must exist and not be deleted.
- The actor must be able to read the target repo (`CanRead`) and see the issue (`issueVisibleTo`). Otherwise the reference is dropped, and nothing about the target is revealed.

### Links

- `pull_issue_links` gets a `source` column (`manual` | `keyword`).
- **On PR create and on title or body edit,** parse the title and body, resolve each reference as the acting user, and replace the PR's keyword links with the result, in one transaction. Manual links are never touched. A manual link to an issue the text also names stays `manual`; linking a keyword-linked issue by hand (`LinkToPull`, used by both sidebars, the new-issue form and the seed) upgrades it to `manual`. Unlinking a same-repo keyword link by hand deletes it, and the next edit that still names it re-adds it.
- **Reading links checks repo readability for cross-repo rows:**
  - `IssueStore.ListLinkedToPull(pullID, viewer)` adds `readableBy` on the issue's repo and returns the issue's owner and repo name.
  - `PullStore.ListLinkedToIssue(issueID, viewer)` drops the same-repo filter on the PR, adds `readableBy` on the PR's repo and returns the PR's owner and repo name. `IssueService.LinkedPRs` gains a viewer parameter.
  - Both sidebars render a cross-repo row as `owner/repo#N`, linked to the other repo, without an unlink control: the unlink endpoints resolve the other side by number in the current repo. Editing the PR text is how a cross-repo link goes away.
- The sidebar pickers stay same-repo.

### Closing on merge

`ff`, `merge` and `squash` all run through `UpdatePull`; auto-merge runs through `tryAutoMerge`. Both move to one handler method, `h.mergePull(ctx, repo, pr, strategy, actor)` in `pull_handler.go`, which runs the checks, the merge and everything after it, so no path can skip a step:

1. Draft, review (`CanMerge`) and branch-protection (`CheckMerge`) checks against `headHash`, the commit #165 pins.
2. If `pr.BaseBranch == repo.DefaultBranch`, read the closing references in the commits being merged, `commitRange(base tip, headHash)`, before the merge writes (a fast-forward leaves that range empty).
3. The merge itself, with `headHash`. `ErrRefMoved` stops here as today.
4. `SetState(merged)`, the `merged` PR event, the `pull_request` webhook, `NotifyPRStateChange` and `EventPRMerged`.
5. If the base is the default branch: close every linked issue (manual and keyword, same-repo and cross-repo) and every issue the commit references resolve to, through the closer below with `pull_id` as the source.

A PR merged into any other branch closes nothing.

**Auto-merge:** `EnableAutoMerge` stores the arming user in `auto_merge_by`; `DisableAutoMerge` clears it. `tryAutoMerge` loads that user and calls `h.mergePull` as them, with `autoMergeAuthor` still as the git author. If `auto_merge_by` is NULL (armed before this change, or the user was deleted) or the user can no longer write the repo, auto-merge disarms itself instead of merging, so the badge stops claiming it's armed.

### Closing on push

After receive-pack, both transports launch `IssueCloser.CloseForPush(ctx, repo, actor, gitRepo, commands)` in a goroutine next to `OnPostReceive`:

- Only the command that updates `refs/heads/<default branch>`, and only when it's a fast-forward (`isAncestor(old, new)`). Branch creation would replay a whole history: a GitHub project pushed to a fresh Cloudzilla repo would close Cloudzilla's #12 for every "Fixes #12" its history ever had. Force-pushes are skipped for the same reason, and because a rebase re-creates old messages under new SHAs.
- Deploy-key pushes are skipped: there's no user to act as.
- Commits are `commitRange(old, new)`, oldest first, so the first commit to close an issue is the one recorded. The commit SHA is the source.
- The seed calls `OnPostReceive` directly and not the closer. Mirror syncs (a parallel session) must not call it either.

### The closer

`IssueCloser` (`internal/service/issue_closer.go`) owns closing. Its actor is `{UserID, Username, Targets}`: the merger (`Targets` from their claims), the auto-merge arming user (no targets), or the pusher (`Targets` from the PAT for HTTP, none for SSH keys).

For each issue, it closes only when:

- the actor can write the **issue's** repo (`RepoService.CanWrite`), checked at close time;
- the issue's repo isn't archived;
- the actor's token targets, if any, cover the issue's repo (the matching half of `TargetAllows`, moved to a function both packages can call).

A reference that fails any of these is skipped and logged at debug level; the merge or push still succeeds.

Accepted gap: auto-merge doesn't persist the arming token's targets, so its closes use the arming user's full permissions.

### Timeline

A new `issue_events` table, mirroring `pull_events`: `(id, issue_id, actor_id, actor_name, event_type, pull_id NULL, commit_sha NULL, created_at)`. `pull_id` references `pull_requests` with `ON DELETE SET NULL`; the event keeps rendering as "closed this" if the PR is gone.

- Types: `closed`, `reopened`.
- The issue page interleaves events with comments by time: "alice closed this in #34", "alice closed this in acme/api#34" (cross-repo), "bob closed this in `a1b2c3d`" (linked to the commit in the repo it was pushed to), "carol closed this", "carol reopened this". Rendering reuses the PR timeline's `TimelineItem` and `TimelineIcon` components.
- `UpdateIssue` records manual closes and reopens.

### Side effects

Each close the closer makes fires what a manual close in `UpdateIssue` fires, with the closer's actor:

- `issues` webhook with action `closed` (`WebhookService.IssuePayload`), on the issue's repo;
- `NotifyIssueStateChange` → `issue_closed` to the issue author and watchers, skipped when the actor is the author;
- `EventIssueClosed` in the activity feed.

They fire after the close's transaction commits, and only for issues this call actually closed.

### Idempotency

Closing is a claim, one transaction per issue, in the style of `commits_ingested`'s `attemptIngest`:

1. `SELECT ... FOR UPDATE` the issue; if it isn't open, stop (no event, webhook or notification).
2. Insert the `closed` event. Partial unique indexes on `(issue_id, pull_id)` and `(issue_id, commit_sha)` turn the insert into a no-op if this PR or this commit already closed this issue once; then stop.
3. Set the issue closed.

So:

- A merged PR can't merge again (`SetState` refuses merged PRs), and the `(issue_id, pull_id)` index backs that up.
- The same commit arriving again (pushed, the branch reset, the commit pushed again; or a fork's commit later pushed upstream) doesn't re-close an issue someone reopened in between.
- A fork is a separate repo with its own issues: a bare `#3` in a commit closes `fork#3` when pushed to the fork and `upstream#3` when pushed upstream. A cross-repo `upstream/repo#3` pushed to the fork closes `upstream#3` once, and the same commit pushed upstream later finds the claim.
- A rebased commit (new SHA) that reaches the default branch by fast-forward can re-close a reopened issue. Accepted: it's a new commit that says it fixes the issue.

### Migration

One migration, the next free number at commit time (102 today):

- `ALTER TABLE pull_issue_links ADD COLUMN source TEXT NOT NULL DEFAULT 'manual' CHECK (source IN ('manual','keyword'))`. Existing rows become `manual`.
- `CREATE TABLE issue_events (...)` with an `(issue_id, created_at)` index and the two partial unique indexes.
- `ALTER TABLE pull_requests ADD COLUMN auto_merge_by BIGINT REFERENCES users(id) ON DELETE SET NULL`.

## Acceptance criteria

- [x] `ParseClosingRefs` matches all nine keywords case-insensitively, with and without `:`, for `#N` and `owner/repo#N`, and rejects `prefixes #1`, `Fixes#1`, `Fixes #1abc`, an out-of-range number and the second reference in `Fixes #1, #2` (table-driven unit test).
- [x] Creating a PR whose title or body says "Fixes #N" links issue N with `source = keyword`; editing the text to drop it unlinks it; a manual link survives both; linking a keyword-linked issue by hand makes it `manual`.
- [x] "Fixes owner/repo#N" links an issue in another repo only when the acting user can read that repo and see the issue.
- [x] The PR sidebar shows a cross-repo linked issue, and the issue sidebar a cross-repo linked PR, only to viewers who can read the other repo, as `owner/repo#N` with no unlink control.
- [x] Merging a PR into the default branch with `ff`, `merge` and `squash` each closes its manual and keyword links and the issues its commits reference, same-repo and cross-repo; merging into another branch closes none.
- [x] Auto-merge closes the same issues as a manual merge, acting as the arming user, and records the merge's PR event, webhook, notification and activity event. With no arming user, or one who lost write access, it disarms instead of merging.
- [x] A fast-forward push to the default branch over HTTP and over SSH closes the issues its new commits reference; pushes to other branches, branch creation, force-pushes and deploy-key pushes close none (integration tests with real pushes, using `internal/ssh/server_test.go`'s helpers).
- [x] An actor who can't write the issue's repo, an archived issue repo, or a token whose targets don't cover the issue's repo closes nothing there.
- [x] Each close records an `issue_events` row, fires the `issues`/`closed` webhook on the issue's repo, notifies the author and watchers (not on a self-close), and records `EventIssueClosed`; an already-closed issue gets none of these.
- [x] The same commit or PR never closes an issue twice, even after a reopen.
- [x] The issue page shows keyword closes (same-repo PR, cross-repo PR, commit), manual closes and reopens, in time order with the comments.
- [x] `docs/pr-merge.md`, `docs/webhooks.md` and `docs/notifications.md` describe keyword closes.

## Relevant files

- `internal/handler/pull_handler.go`: `CreatePull`, `UpdatePull` (title, body, merge branches), `tryAutoMerge`
- `internal/handler/pull_linked_issue_handler.go`, `internal/handler/issue_meta_handler.go`, `internal/handler/page_issue_handler.go`: link sidebars and the new-issue form
- `internal/handler/issue_handler.go`: `UpdateIssue` (manual close/reopen side effects)
- `internal/handler/commit_status_handler.go`, `internal/handler/pull_review_handler.go`: auto-merge triggers
- `internal/handler/git_http.go`, `internal/ssh/server.go`: post-receive goroutines, the pusher's identity
- `internal/middleware/auth.go`, `internal/middleware/scope.go`: `Claims.Targets`, `TargetAllows`
- `internal/service/pull_service.go`: `Create`, `SetState`, `EnableAutoMerge`, `DisableAutoMerge`
- `internal/service/issue_service.go`, `internal/store/issue_store.go`, `internal/store/pull_store.go`: links, `UpdateState`, `ListLinkedToPull`, `ListLinkedToIssue`
- `internal/store/repo_store.go`: `readableBy`
- `internal/service/repo_service.go`: `OnPostReceive`, `forEachPushedCommit`, `CanRead`, `CanWrite`
- `internal/service/code_service_merge.go`, `commit_range.go`, `merge_base.go`: merge methods, `commitRange`, `isAncestor`
- `internal/service/notification_service.go`, `internal/service/webhook_service.go`, `internal/service/event_service.go`
- `internal/service/pull_event_service.go`, `internal/model/pull_event.go`: the pattern for `issue_events`
- `internal/view/pages/issue_detail.templ`, `internal/view/pages/pull_detail.templ`, `internal/view/fragments/linked_issues_sidebar.templ`, `internal/view/fragments/issue_meta.templ`
- `internal/db/migrations/059_create_pull_events.sql`, `060_create_pull_issue_links.sql`
- `internal/seed/activity.go`: its manual `LinkPull` after writing "Closes #N" becomes redundant
- `internal/ssh/server_test.go`: push helpers for integration tests
