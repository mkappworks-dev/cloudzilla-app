# Pull Request Merge Strategies

Cloudzilla supports three merge strategies selectable from the PR detail page.

**Diff view:** The PR's Files tab (`PagePullFiles`) diffs head's tip against its merge base with base, like
`git diff base...head`, so commits that land on base after head branched off don't show up as reverted by the PR.
Branches with no common history fall back to a diff between the two tips. The Files badge, CODEOWNERS reviewer
auto-request and suggested reviewers use the same file list.

**Merged PRs:** once head is merged into base, both the commit range and the diff between the branches are empty, so a
merge records the base tip it saw and the head tip it merged (`pull_requests.merged_base_sha` and `merged_head_sha`,
migration 120, set by `PullService.RecordMergedRange` after `SetState`). The Commits tab, Files tab and their badges
read from those two SHAs instead of the branches (`pullDiffRefs`). A PR merged earlier is rebuilt at startup by
`PullService.BackfillMergedRanges` (`CodeService.ReconstructMergedRange`): a merge commit whose second parent is the
head tip gives the base tip as its first parent; a fast-forward counts the recorded number of commits down from the
head. A squash merge, a deleted or moved head branch, or a missing commit count can't be rebuilt, so the PR keeps empty
SHAs and its Commits and Files tabs say "unavailable after merge" instead of "No changes."

## Available Strategies

| Strategy        | Button                 | When shown                           | What it does                                            |
| --------------- | ---------------------- | ------------------------------------ | ------------------------------------------------------- |
| Fast-forward    | "Merge (fast-forward)" | Head is a direct descendant of base  | Advances base branch ref — no new commit                |
| Three-way merge | "Create merge commit"  | FF or clean three-way merge possible | Creates a commit with two parents (base + head)         |
| Squash merge    | "Squash and merge"     | FF or clean three-way merge possible | Collapses head commits into a single new commit on base |

**Conflict detection:** `mergeFiles` performs a pure tree-level three-way merge in memory — if the same path (a file,
symlink or submodule pointer) was changed on both sides relative to the merge base, in content or in mode, all merge
buttons are hidden and a conflict warning is shown. That includes a chmod on one side and an edit on the other, which
`git merge` would combine. The developer must rebase locally and push. Only the merge methods write the merged tree,
through `mergeTreesNoConflict`; viewing a PR writes nothing to the repo.

A file, symlink or submodule on one side where the other side has a directory at the same path is a conflict too, as in
`git merge`. When base adds the file `lib` and head adds `lib/x`, no path changed on both sides, but a tree can't hold
two entries named `lib`.

**History walks:** Viewing and merging a PR read only the history since base and head diverged, not all of either
branch. `findMergeBase` and `isAncestor` (`merge_base.go`) are git's merge-base walk, exact under any clock skew. The
Ahead/Behind counts and the Commits tab come from `commitRange`, which lists what `git rev-list a..b` does, in its
order: under clock skew deeper than its slop, that can include commits base reaches. History made within one second, as
scripts make it, can still be read to the root.

## Merge Flow

1. `PagePullDetail` (the Conversation tab) calls `CodeService.Mergeability(base, head)` →
   `Mergeability{Ahead, Behind, HasConflicts}` and derives the merge box's `CanFastForward`, `CanThreeWayMerge` and
   `CanSquash` flags from it
2. The merge box offers only the strategies those flags allow
3. On button click → HTMX `PATCH /api/repos/{owner}/{repo}/pulls/{number}` with `state=merged` and
   `merge_strategy=ff|merge|squash`
4. `UpdatePull` calls `h.mergePull`, which runs the checks and dispatches to `MergePullRequest`,
   `ThreeWayMergePullRequest`, or `SquashMergePullRequest`
5. On success → `PullService.SetState(merged)`, the `merged` timeline event, the `pull_request` webhook, the
   notification and activity event, then [closing issues](#closing-issues) → HTMX callers get `204 No Content`
6. On failure (conflict, missing branch, etc.) → 422 → the layout shows an error toast. If a push moved the base branch
   mid-merge, or the head branch since its statuses were checked → 409 and the PR stays open (see below)

**HTMX responses:** `UpdatePull` (merge, close, reopen, draft toggle, auto-merge) and `SubmitReview` answer an HTMX
request with `204 No Content`. The page's `hx-on::after:request` stashes a toast in `sessionStorage` and reloads, so
every box, badge and the timeline are rendered fresh instead of by a partial that can go stale.

**Concurrent pushes:** every server-side commit (merges, applied suggestions, web file commits, wiki edits) advances its
branch through `gitref.Move`, a compare-and-swap against the tip it read. If a push moved the branch in between, the
update is refused with `ErrRefMoved`, handlers answer 409 ("branch was updated while saving; reload and try again"), and
the pushed commits stay. An unconditional write would be a force push that skips the `block_force_push` check, which
only runs in receive-pack.

The head branch gets the same guard: `UpdatePull` and `tryAutoMerge` resolve head to a commit, check that commit's
required statuses with `BranchProtection.CheckMerge`, and pass its hash to the merge method. If a push moved head since,
the merge fails with `ErrRefMoved` (409 from `UpdatePull`; auto-merge leaves the PR open) instead of merging commits the
required checks never saw.

**One merge path:** `UpdatePull` and `tryAutoMerge` both go through `h.mergePull` (`pull_handler.go`), so every merge,
manual or automatic, gets the same checks and side effects. Auto-merge acts as the user who armed it
(`pull_requests.auto_merge_by`, set by `EnableAutoMerge`), with `auto-merge <auto-merge@localhost>` as the git author of
merge and squash commits. If no arming user is recorded (armed before migration 102, or the user was deleted) or they
can no longer write the repo, `tryAutoMerge` disarms auto-merge instead of merging.

**Tree order:** those commits write their trees through `writeTree`, which sorts entries the way git does: a directory
compares as its name plus `/`, so `docs.md` comes before `docs/`. go-git refuses to encode a tree in any other order
("entries in tree are not sorted").

## CodeService Methods

- `GetPullDiff(owner, repo, base, head)` → `*PRDiffResult`
- `PullDiffStats(owner, repo, base, head)` → `(DiffStats, error)` (`GetPullDiff`'s file and line totals without the
  hunks; feeds the Files badge on the other PR tabs)
- `Mergeability(ctx, owner, repo, base, head)` → `(Mergeability, error)`
- `PullCommits(owner, repo, base, head)` → `([]CommitSummary, error)` (the commits head has and base doesn't, newest
  first)
- `MergePullRequest(owner, repo, base, head, headHash)` → `error` (fast-forward only)
- `ThreeWayMergePullRequest(owner, repo, base, head, headHash, author GitAuthor)` → `error`
- `SquashMergePullRequest(owner, repo, base, head, headHash, author GitAuthor)` → `error`
- `checkFastForward(repo, baseCommit, headCommit)` → `bool` (private; `isAncestor`, as `git merge-base --is-ancestor`)
- `findMergeBase(repo, a, b)` → `(*object.Commit, error)` (private; best common ancestor, as `git merge-base`)
- `mergeTreesNoConflict(repo, mergeBase, base, head)` → `(plumbing.Hash, bool, error)` (private)
- `mergeFiles(mergeBase, base, head)` → `(map[string]mergeFile, bool, error)` (private; `mergeTreesNoConflict` without
  the tree writes, used by `Mergeability`)
- `flattenTree(tree)` → `(map[string]mergeFile, error)` (private; every entry but directories, so symlinks and
  submodules survive a rebuild)
- `changedPaths(from, to)` → `map[string]bool` (private; paths added, deleted, or changed in content or mode)
- `buildTree(repo, files)` → `(plumbing.Hash, error)` (private; recursively encodes tree objects)
- `hasPathUnderFile(files)` → `bool` (private; whether a path lies below another, such as `lib/x` below `lib`)

`UpdatePull` gets the `GitAuthor` from `UserService.CommitAuthor`, which honours the merger's keep-email-private setting
(see [api-reference](./api-reference.md#commit-email-privacy)).

## The PR page's merge box

`MergeabilityBox` (`components/mergeability_box.templ`) picks one state, first match wins:

| State             | When                                                   | Footer                          |
| ----------------- | ------------------------------------------------------ | ------------------------------- |
| Draft             | the PR is a draft                                      | Ready for review                |
| Conflicts         | head and base changed the same path                    | Close only                      |
| Up to date        | head has nothing base lacks                            | Close only                      |
| Changes requested | a reviewer's standing verdict is `changes_requested`   | Close only                      |
| Checks failing    | a required check failed or errored with none passing   | disabled Merge, "View checks →" |
| Review required   | approvals are below the branch protection rule's count | Enable auto-merge               |
| Waiting on checks | a required check has not reported success yet          | Enable auto-merge               |
| Ready             | otherwise                                              | Merge split button              |
| Waiting           | Ready, for a viewer without write access               | none (lock line)                |

Auto-merge is offered only in "Review required" and "Waiting on checks": failing required checks need a push or a re-run
first. Once armed, any non-draft state shows who armed it and the strategy, with Disable. With no branch protection rule
there are no required reviews or checks, so the box goes from Ready straight to Merge.

**Viewers without write access** see "Ready, waiting for a maintainer" in a neutral header where a writer sees "Ready to
merge", and a lock line in place of the buttons. Their other states read the same, since those say what blocks the
merge.

**Conflicts:** the box lists the commands for merging base into head locally (`git fetch`, `git switch`, `git merge`,
`git push`) and a Check again button. There is no in-browser conflict editor.

**Update branch:** a writer sees it beside Merge whenever head is behind base without conflicts
(`MergeabilityBoxData.CanUpdateBranch`). `POST …/pulls/{n}/update-branch` calls `CodeService.UpdatePullBranch`, which
merges base into head as a two-parent commit by the caller (`Merge branch 'main' into 'feature'`, parents head then
base) through `gitref.Move`, so a push that lands meanwhile gets `409`. A conflict gets `422` and leaves head alone;
head already containing base gets `409` (`ErrBranchUpToDate`). Like applied suggestions, the commit runs the push side
effects (`PushService.AfterWebCommit`) and needs `repo:write` on a token.

**Dismissing a review:** "Changes requested" lists each blocking reviewer, and a repo admin or owner (`CanManage`) gets
Dismiss review beside each. A writer cannot, since writers can merge and would clear the check on themselves. The
dialog's reason is required (at most 500 characters). `PullReviewService.Dismiss` turns a `changes_requested` review
into `dismissed` (migration 121, with `dismissed_by_name`, `dismissed_reason` and `dismissed_at`), which neither
`HasChangesRequested` nor `CountApprovals` counts, so the merge unblocks and auto-merge gets a turn. The timeline keeps
the reviewer's words and adds "Dismissed by …: reason", and the action is audited as `pull.review.dismiss` (target the
pull request, metadata `review_id`, `reviewer`, `reason`). Only a standing request for changes can be dismissed;
approvals can't. When the reviewer submits again, the new verdict replaces the dismissal.

**Confirm step:** the Merge button opens a dialog showing the strategy and, for `merge` and `squash`, an editable commit
title and message. They default to `Merge branch 'head' into 'base'` and `Squash merge branch 'head' into 'base'`
(`service.DefaultMergeMessage`, `DefaultSquashMessage`), and a fast-forward shows no fields because it creates no
commit. The dialog patches `merge_title` and `merge_message`; closing keywords in them close issues like those in commit
messages. Enabling auto-merge patches at once and keeps the default message.

**Merged header:** the merged event's `detail` holds the commits that merged, counted before the merge, since a
fast-forward or merge commit leaves no range between base and head afterwards. The header names the event's actor, not
the author. A PR merged before the count was recorded falls back to its author and the commit count of its recorded
range; with neither, the header reads "merged into main" with no count.

**Reviewing:** the "Review changes" dialog posts to `/reviews`. Write access is required, and the PR author may only
comment. A `commented` review never replaces the reviewer's standing Approve or Request changes, so their comment is
posted to the timeline instead.

## Suggestions

A `suggestion` fence in a line comment renders as a diff of the line it replaces. Writers see Apply suggestion on a
right-side suggestion of an open PR. Applying commits to the head branch through `CodeService.ApplySuggestions` and
records `applied_at`, `applied_sha` and `applied_by` on the comment (migration 119), so a second apply gets `409`.

"Add to batch" is client-side state. "Commit suggestions" posts the ids to `POST …/pulls/{n}/suggestions/apply`, which
applies them in one commit, bottom-up per file so a replacement that changes the line count doesn't shift the next one.
Two suggestions on one line fail the whole batch with `422`, and nothing changes.

## Closing Issues

GitHub-style closing keywords close issues when a PR merges into the default branch or when commits reach it by a push.

**Keywords:** `close`, `closes`, `closed`, `fix`, `fixes`, `fixed`, `resolve`, `resolves`, `resolved`, case-insensitive,
with an optional `:`, then whitespace and `#N` (an issue in the same repo) or `owner/repo#N`. One keyword per reference:
`Fixes #1, #2` closes only #1. `ParseClosingRefs` (`closing_keywords.go`) is the parser. `#N` always means an issue:
issues and PRs are numbered separately.

**Links:** creating a PR or editing its title or body re-reads the text, and the issues it names that the editor can
see, within their token's targets if it has any, become the PR's `keyword` links (`pull_issue_links.source`). Links made
by hand are `manual`, and linking a keyword-linked issue by hand makes it manual, so editing the text never removes it.
Cross-repo links show in both sidebars as `owner/repo#N`, to viewers who can read the other repo, with no picker toggle:
editing the PR text is how one goes away.

**On merge** into the default branch, `IssueCloser.CloseForPull` closes every linked issue, manual or keyword, and every
issue named in the merged commits' messages. Those are read with `ClosingRefsInMerge` before the merge writes, since a
fast-forward leaves nothing between base and head afterwards; squash commits don't carry them. A merge into any other
branch closes nothing.

**On push**, `IssueCloser.CloseForPush` runs after receive-pack over HTTP and SSH. Only a fast-forward of the default
branch by a user counts: creating the branch, force-pushing it and deploy-key pushes close nothing. Commits are read
oldest first. Server-side merges and web commits don't go through receive-pack and close nothing this way.

**Who may close:** the merger, the auto-merge arming user or the pusher, who must be able to write the issue's repo,
whose repo isn't archived, and whose token, if it is limited to some repos and orgs, covers that repo. Otherwise the
reference is skipped; the merge or push still succeeds. Auto-merge doesn't keep the arming token's targets, so it closes
with the user's full permissions.

**Once each:** each close is a transaction that locks the issue, records an `issue_events` row and closes it. Unique
indexes on `(issue_id, pull_id)` and `(issue_id, commit_sha)` for `closed` events mean a PR or a commit closes an issue
at most once, even after someone reopens it. A close fires the `issues` webhook (action `closed`), `issue_closed`
notifications and the `issue_closed` activity event, as a manual close does.

**Timeline:** the issue page interleaves `issue_events` with comments: "closed this in #34", "closed this in
acme/api#34", "closed this in `a1b2c3d`", and manual closes and reopens, which `UpdateIssue` records. The PR or commit
is left out for viewers who can't read the repo it came from.

## PRDiffResult Type

```go
type PRDiffResult struct {
    Files        []FileDiff
    TotalAdded   int
    TotalDeleted int
}
```

---

## Draft Pull Requests

A draft PR signals that the work is still in progress and is not yet ready for merge or review.

### State

A draft PR has `state = "open"` and `is_draft = true`. It is separate from closed/merged PRs. Converting to ready
changes `is_draft` to `false` without touching `state`.

`draft_at` records when the PR was first marked as a draft. It is never cleared when converting to ready.

### PR List Filtering

The pulls list page (`/{owner}/{repo}/pulls`) filters by `?state=` query param:

| `?state=` | Shows                                      |
| --------- | ------------------------------------------ |
| `open`    | Open PRs with `is_draft = false` (default) |
| `draft`   | Open PRs with `is_draft = true`            |
| `closed`  | Closed PRs                                 |
| `merged`  | Merged PRs                                 |

### PR Detail Page

When `is_draft = true`:

- A yellow "Draft" badge appears next to the PR title
- A yellow banner is shown: "This pull request is still a work in progress."
- All merge strategy buttons are hidden
- The review prompt and "Review changes" dialog are hidden
- The muted "Draft" merge box offers "Ready for review" (calls `PATCH` with `{"is_draft":"false"}`); the banner only
  explains
- The "Close pull request" button stays in the merge box, the one place that closes a PR

An open, non-draft PR gets "Convert to draft" at the bottom of the sidebar, for writers.

### API

**Mark PR as draft / convert to ready:**

```
PATCH /api/repos/{owner}/{repo}/pulls/{number}
Content-Type: application/json

{"is_draft": true}   # mark as draft
{"is_draft": false}  # convert to ready for review
```

The `is_draft` and `state` fields are mutually exclusive in one request — handle them in separate calls.

**Merge block:**

Attempting `PATCH` with `state=merged` on a draft PR returns:

```
HTTP 422 Unprocessable Entity
cannot merge a draft pull request
```

Convert to ready first, then merge.

### Implementation

- Schema: `is_draft BOOLEAN NOT NULL DEFAULT FALSE`, `draft_at TIMESTAMPTZ` (migration 029)
- Store: `PullStore.SetDraft(ctx, id, isDraft)` — SQL uses `CASE WHEN $1 = TRUE THEN NOW() ELSE draft_at END` to
  preserve `draft_at` on convert-to-ready
- Service: `PullService.SetDraft(ctx, owner, repo, number, isDraft)` — guards against closed/merged PRs
- Handler: `UpdatePull` checks `is_draft` field before `state`; draft merge block runs before `CanMerge`
