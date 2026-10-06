# Issue closing keywords

Created: 2026-10-06
Category: enhancement
Status: needs-triage

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

## Proposed design

Decisions still open are marked **(Q*n*)** and listed under [Open questions](#open-questions). The design below assumes the recommended answer to each.

### Parsing

A pure function, `ParseClosingRefs(text string) []ClosingRef`, in `internal/service/closing_keywords.go`:

- Keywords, case-insensitive: `close`, `closes`, `closed`, `fix`, `fixes`, `fixed`, `resolve`, `resolves`, `resolved`.
- Grammar: keyword at a word boundary, an optional `:`, whitespace, then a reference. `Fixes #12`, `fixes: #12`, `FIXES #12` match; `prefixes #12`, `Fixes#12` and `Fixes #12abc` don't.
- Reference forms: `#N`, and `owner/repo#N` **(Q2)**. `owner/repo#N` naming the text's own repo (case-insensitively) is treated as `#N`.
- One keyword per reference, as on GitHub: `Fixes #1, fixes #2` closes both; `Fixes #1, #2` closes only #1.
- Duplicates are collapsed; order of first appearance is kept.
- Plain text: code spans and fences aren't special-cased (commit messages aren't Markdown either).

### Which issues a PR closes

- **On create and on title/body edit,** parse the title and body and store the result as keyword links in `pull_issue_links`, with a new `source` column (`manual` | `keyword`). Re-parsing replaces the PR's keyword links and never touches manual ones. A manual link to an issue that is also keyword-linked stays `manual`; linking a keyword-linked issue by hand upgrades it to `manual`. Unlinking a keyword link by hand deletes it, and the next edit that still mentions it re-adds it.
- Only issues in the PR's own repo that the acting user can see are linked; unknown numbers are ignored.
- **On merge into the repo's default branch** (`pr.BaseBranch == repo.DefaultBranch`, checked at merge time), close:
  - every linked issue, manual or keyword **(Q1)**, and
  - every issue referenced by a closing keyword in the PR's commits **(Q3)**, i.e. `commitRange(base tip, headHash)` with the `headHash` from #165, read **before** the merge writes (a fast-forward leaves that range empty).
- A PR merged into any other branch closes nothing.

### Every merge path

`ff`, `merge` and `squash` all run through `UpdatePull`; auto-merge runs through `tryAutoMerge`. Both call one service method after a successful merge and `SetState(merged)`, so the strategy makes no difference:

```
IssueCloser.CloseForMergedPull(ctx, repo, pr, actor, prCommitRefs)
```

`tryAutoMerge` needs an actor **(Q4)**. Recommended: store who armed auto-merge (`pull_requests.auto_merge_by`), re-check that they can still write the repo at merge time, and use them as the actor for the merge's own timeline event, webhook, notification and activity event (all missing today) as well as for the closes. If the arming user can no longer write, auto-merge doesn't merge.

### Direct pushes to the default branch

**(Q5)** After receive-pack, both transports launch `IssueCloser.CloseForPush(ctx, repo, pusher, gitRepo, commands)` next to `OnPostReceive`:

- Only the command that updates `refs/heads/<default branch>`, and only when it's a fast-forward (`isAncestor(old, new)`). Branch creation is skipped, because the first push of an existing project would replay its whole history: a GitHub repo pushed to a fresh Cloudzilla repo would close Cloudzilla's #12 for every "Fixes #12" its history ever had. Force-pushes are skipped for the same reason, and because a rebase re-creates old messages under new SHAs.
- Deploy-key pushes are skipped: there's no user to act as or to check permissions for.
- Commits are those in `commitRange(old, new)`, oldest first, so the first commit to close an issue is the one recorded.
- The seed calls `OnPostReceive` directly and not the closer, so seeding never closes issues through this path. Mirror syncs (a parallel session) must not call it either.

### Permissions

- The actor is the merger (`UpdatePull`), the auto-merge arming user (`tryAutoMerge`) or the pusher (`CloseForPush`).
- Before each close, the actor must pass `RepoService.CanWrite` on the **issue's** repo, checked at close time. For same-repo references this already holds (merging and pushing need write), but checking per issue keeps cross-repo references **(Q2)** safe and covers a collaborator removed while auto-merge was armed.
- Issues in an archived repo are never closed.
- A PR author with only read access can create keyword links (as on GitHub), but only to issues they can see. Closing still needs a writer to merge.

### Side effects of a keyword close

Each close fires the same side effects as a manual close in `UpdateIssue`, plus a timeline entry:

- **Timeline:** a new `issue_events` table **(Q6)**, mirroring `pull_events`: `(issue_id, actor_id, actor_name, event_type, pull_id NULL, commit_sha NULL, created_at)`. The issue page interleaves events with comments by time: "alice closed this in #34", "alice closed this in `abc1234`", "alice closed this", "alice reopened this". `UpdateIssue` records manual closes and reopens too, so a keyword close followed by a manual reopen reads correctly.
- **Webhook:** `issues` with action `closed` (`WebhookService.IssuePayload`).
- **Notification:** `NotifyIssueStateChange` → `issue_closed` to the issue author and watchers, skipped when the actor is the author.
- **Activity feed:** `EventIssueClosed`.

Webhooks and notifications fire only for issues this call actually closed, after the transaction commits.

### Idempotency

Closing is a claim, in one transaction per issue, in the style of `commits_ingested`'s `attemptIngest`:

1. Lock the issue row; if it isn't open, stop (no event, webhook or notification).
2. Insert the `closed` event. Partial unique indexes on `(issue_id, pull_id)` and `(issue_id, commit_sha)` make the insert a no-op if this PR or this commit already closed this issue once; then stop.
3. Set the issue closed.

So:

- A merged PR can't merge again (`SetState` refuses merged PRs), and the `(issue_id, pull_id)` index backs that up.
- The same commit arriving again (pushed, the branch reset, the commit pushed again; or a fork's commit later pushed upstream to the same repo) doesn't re-close an issue someone reopened in between.
- A fork is a separate repo with separate issues, so a commit closing `fork#3` and later `upstream#3` closes two different issues, both correctly.
- A rebased commit (new SHA) reaching the default branch by fast-forward can re-close a reopened issue. Accepted: it's a new commit that says it fixes the issue.

### Migration

One migration (next free number at commit time; 102 today):

- `ALTER TABLE pull_issue_links ADD COLUMN source TEXT NOT NULL DEFAULT 'manual' CHECK (source IN ('manual','keyword'))`. Existing rows become `manual`.
- `CREATE TABLE issue_events (...)` with the partial unique indexes above and an `(issue_id, created_at)` index.
- `ALTER TABLE pull_requests ADD COLUMN auto_merge_by BIGINT REFERENCES users(id) ON DELETE SET NULL` **(Q4)**.

## Acceptance criteria

- [ ] `ParseClosingRefs` matches all nine keywords case-insensitively, with and without `:`, and rejects `prefixes #1`, `Fixes#1`, `Fixes #1abc` and `Fixes #1, #2`'s second reference (table-driven unit test).
- [ ] Creating a PR whose title or body says "Fixes #N" links issue N (`source = keyword`); editing the body to drop it unlinks it; a manual link survives both.
- [ ] A keyword never links an issue the acting user can't see, or an issue in another repo unless Q2 allows it.
- [ ] Merging a PR into the default branch with `ff`, `merge` and `squash` each closes its linked issues and the issues its commits reference; merging into another branch closes none.
- [ ] Auto-merge closes the same issues as a manual merge, acting as the user who armed it, and records the merge's timeline event, webhook, notification and activity event.
- [ ] A fast-forward push to the default branch over HTTP and over SSH closes the issues its new commits reference; pushes to other branches, branch creation, force-pushes and deploy-key pushes close none (integration tests with real pushes, using `internal/ssh/server_test.go`'s helpers).
- [ ] An actor without write access to the issue's repo, or an archived target repo, closes nothing.
- [ ] Each close records an `issue_events` row, fires the `issues`/`closed` webhook, notifies the author and watchers (not a self-close), and records `EventIssueClosed`; an already-closed issue gets none of these.
- [ ] The same commit or PR never closes an issue twice, even after a reopen.
- [ ] The issue page shows "closed this in #PR", "closed this in `sha`", manual close and reopen entries in time order with the comments.
- [ ] `docs/pr-merge.md`, `docs/webhooks.md` and `docs/notifications.md` describe keyword closes.

## Relevant files

- `internal/handler/pull_handler.go`: `CreatePull`, `UpdatePull` (title, body, merge branches), `tryAutoMerge`
- `internal/handler/pull_linked_issue_handler.go`, `internal/handler/issue_meta_handler.go`: manual linking
- `internal/handler/issue_handler.go`: `UpdateIssue` (manual close/reopen side effects to mirror)
- `internal/handler/commit_status_handler.go`, `internal/handler/pull_review_handler.go`: auto-merge triggers
- `internal/handler/git_http.go`, `internal/ssh/server.go`: post-receive goroutines
- `internal/service/pull_service.go`: `Create`, `SetState`, `EnableAutoMerge`
- `internal/service/issue_service.go`, `internal/store/issue_store.go`: links, `UpdateState`
- `internal/service/repo_service.go`: `OnPostReceive`, `forEachPushedCommit`, `CanWrite`
- `internal/service/code_service_merge.go`, `commit_range.go`, `merge_base.go`: merge methods, `commitRange`, `isAncestor`
- `internal/service/notification_service.go`, `internal/service/webhook_service.go`, `internal/service/event_service.go`
- `internal/service/pull_event_service.go`, `internal/model/pull_event.go`: pattern for `issue_events`
- `internal/view/pages/issue_detail.templ`, `internal/view/pages/pull_detail.templ` (timeline components)
- `internal/db/migrations/060_create_pull_issue_links.sql`, `059_create_pull_events.sql`
- `internal/seed/activity.go`: may drop its manual `LinkPull`, since the body's "Closes #N" now links
- `internal/ssh/server_test.go`: push helpers for integration tests

## Open questions

1. **Do manually linked issues close on merge?** Today a link is just a cross-reference. GitHub closes issues linked from a PR's "Development" sidebar on merge. Recommended: yes, all links close; existing open PRs' links then start closing their issues when merged, which matches what the seed already assumes.
2. **Support `owner/repo#N`?** Example: a PR in `acme/api` says "Fixes acme/docs#4". Supporting it needs a per-issue write check (designed above), but also cross-repo rows in `pull_issue_links`, whose readers assume one repo: `ListLinkedToIssue` filters both sides to one repo, and `ListLinkedToPull` filters by issue visibility but not repo readability, so a public issue in a private repo would show its title to the PR's viewers. Recommended: same-repo only for now (an `owner/repo#N` naming the PR's own repo counts); cross-repo as a follow-up ticket.
3. **Do keywords in a PR's commit messages close on merge?** Without this, "Fixes #12" in a commit closes #12 when pushed straight to `main` but not when the same commit is merged through a PR, and squash merges drop the message entirely. Recommended: yes, scanning the commits #165's `headHash` brings in, read before the merge.
4. **Who closes issues on auto-merge?** Auto-merge has no recorded actor today, and also skips the merge's own event, webhook and notification. Recommended: store `auto_merge_by`, act as that user, and fill in the missing merge side effects in the same shared post-merge path.
5. **Which pushes close issues?** Recommended: fast-forward updates of the default branch by a user. Skip branch creation, force-pushes and deploy-key pushes.
6. **Where does "closed via #PR" show?** Issues have no timeline. Recommended: an `issue_events` table rendered with the comments, recording manual closes and reopens too. Lighter alternative: `closed_by_pull_id` / `closed_by_commit_sha` columns shown as one line by the state badge, with no history.
