# Pull Request Merge Strategies

Cloudzilla supports three merge strategies selectable from the PR detail page.

**Diff view:** The PR's Files tab (`PagePullFiles`) diffs head's tip against its merge base with base, like `git diff base...head`, so commits that land on base after head branched off don't show up as reverted by the PR. Branches with no common history fall back to a diff between the two tips. The Files badge, CODEOWNERS reviewer auto-request and suggested reviewers use the same file list.

## Available Strategies

| Strategy        | Button                 | When shown                           | What it does                                            |
| --------------- | ---------------------- | ------------------------------------ | ------------------------------------------------------- |
| Fast-forward    | "Merge (fast-forward)" | Head is a direct descendant of base  | Advances base branch ref — no new commit                |
| Three-way merge | "Create merge commit"  | FF or clean three-way merge possible | Creates a commit with two parents (base + head)         |
| Squash merge    | "Squash and merge"     | FF or clean three-way merge possible | Collapses head commits into a single new commit on base |

**Conflict detection:** `mergeFiles` performs a pure tree-level three-way merge in memory — if the same path (a file, symlink or submodule pointer) was changed on both sides relative to the merge base, in content or in mode, all merge buttons are hidden and a conflict warning is shown. That includes a chmod on one side and an edit on the other, which `git merge` would combine. The developer must rebase locally and push. Only the merge methods write the merged tree, through `mergeTreesNoConflict`; viewing a PR writes nothing to the repo.

A file, symlink or submodule on one side where the other side has a directory at the same path is a conflict too, as in `git merge`. When base adds the file `lib` and head adds `lib/x`, no path changed on both sides, but a tree can't hold two entries named `lib`.

**History walks:** Viewing and merging a PR read only the history since base and head diverged, not all of either branch. `findMergeBase` and `isAncestor` (`merge_base.go`) are git's merge-base walk, exact under any clock skew. The Ahead/Behind counts and the Commits tab come from `commitRange`, which lists what `git rev-list a..b` does, in its order: under clock skew deeper than its slop, that can include commits base reaches. History made within one second, as scripts make it, can still be read to the root.

## Merge Flow

1. `PagePullDetail` (the Conversation tab) calls `CodeService.Mergeability(base, head)` → `Mergeability{Ahead, Behind, HasConflicts}` and derives the merge box's `CanFastForward`, `CanThreeWayMerge` and `CanSquash` flags from it
2. The merge box offers only the strategies those flags allow
3. On button click → HTMX `PATCH /api/repos/{owner}/{repo}/pulls/{number}` with `state=merged` and `merge_strategy=ff|merge|squash`
4. `UpdatePull` calls `h.mergePull`, which runs the checks and dispatches to `MergePullRequest`, `ThreeWayMergePullRequest`, or `SquashMergePullRequest`
5. On success → `PullService.SetState(merged)`, the `merged` timeline event, the `pull_request` webhook, the notification and activity event, then [closing issues](#closing-issues) → fragment returned
6. On failure (conflict, missing branch, etc.) → 422 → `hx-on::response-error` fires alert. If a push moved the base branch mid-merge, or the head branch since its statuses were checked → 409 and the PR stays open (see below)

**Concurrent pushes:** every server-side commit (merges, applied suggestions, web file commits, wiki edits) advances its branch through `gitref.Move`, a compare-and-swap against the tip it read. If a push moved the branch in between, the update is refused with `ErrRefMoved`, handlers answer 409 ("branch was updated while saving; reload and try again"), and the pushed commits stay. An unconditional write would be a force push that skips the `block_force_push` check, which only runs in receive-pack.

The head branch gets the same guard: `UpdatePull` and `tryAutoMerge` resolve head to a commit, check that commit's required statuses with `BranchProtection.CheckMerge`, and pass its hash to the merge method. If a push moved head since, the merge fails with `ErrRefMoved` (409 from `UpdatePull`; auto-merge leaves the PR open) instead of merging commits the required checks never saw.

**One merge path:** `UpdatePull` and `tryAutoMerge` both go through `h.mergePull` (`pull_handler.go`), so every merge, manual or automatic, gets the same checks and side effects. Auto-merge acts as the user who armed it (`pull_requests.auto_merge_by`, set by `EnableAutoMerge`), with `auto-merge <auto-merge@localhost>` as the git author of merge and squash commits. If no arming user is recorded (armed before migration 102, or the user was deleted) or they can no longer write the repo, `tryAutoMerge` disarms auto-merge instead of merging.

**Tree order:** those commits write their trees through `writeTree`, which sorts entries the way git does: a directory compares as its name plus `/`, so `docs.md` comes before `docs/`. go-git refuses to encode a tree in any other order ("entries in tree are not sorted").

## CodeService Methods

- `GetPullDiff(owner, repo, base, head)` → `*PRDiffResult`
- `PullDiffStats(owner, repo, base, head)` → `(DiffStats, error)` (`GetPullDiff`'s file and line totals without the hunks; feeds the Files badge on the other PR tabs)
- `Mergeability(ctx, owner, repo, base, head)` → `(Mergeability, error)`
- `PullCommits(owner, repo, base, head)` → `([]CommitSummary, error)` (the commits head has and base doesn't, newest first)
- `MergePullRequest(owner, repo, base, head, headHash)` → `error` (fast-forward only)
- `ThreeWayMergePullRequest(owner, repo, base, head, headHash, author GitAuthor)` → `error`
- `SquashMergePullRequest(owner, repo, base, head, headHash, author GitAuthor)` → `error`
- `checkFastForward(repo, baseCommit, headCommit)` → `bool` (private; `isAncestor`, as `git merge-base --is-ancestor`)
- `findMergeBase(repo, a, b)` → `(*object.Commit, error)` (private; best common ancestor, as `git merge-base`)
- `mergeTreesNoConflict(repo, mergeBase, base, head)` → `(plumbing.Hash, bool, error)` (private)
- `mergeFiles(mergeBase, base, head)` → `(map[string]mergeFile, bool, error)` (private; `mergeTreesNoConflict` without the tree writes, used by `Mergeability`)
- `flattenTree(tree)` → `(map[string]mergeFile, error)` (private; every entry but directories, so symlinks and submodules survive a rebuild)
- `changedPaths(from, to)` → `map[string]bool` (private; paths added, deleted, or changed in content or mode)
- `buildTree(repo, files)` → `(plumbing.Hash, error)` (private; recursively encodes tree objects)
- `hasPathUnderFile(files)` → `bool` (private; whether a path lies below another, such as `lib/x` below `lib`)

`UpdatePull` gets the `GitAuthor` from `UserService.CommitAuthor`, which honours the merger's keep-email-private setting (see [api-reference](./api-reference.md#commit-email-privacy)).

## Closing Issues

GitHub-style closing keywords close issues when a PR merges into the default branch or when commits reach it by a push.

**Keywords:** `close`, `closes`, `closed`, `fix`, `fixes`, `fixed`, `resolve`, `resolves`, `resolved`, case-insensitive, with an optional `:`, then whitespace and `#N` (an issue in the same repo) or `owner/repo#N`. One keyword per reference: `Fixes #1, #2` closes only #1. `ParseClosingRefs` (`closing_keywords.go`) is the parser. `#N` always means an issue: issues and PRs are numbered separately.

**Links:** creating a PR or editing its title or body re-reads the text, and the issues it names that the editor can see, within their token's targets if it has any, become the PR's `keyword` links (`pull_issue_links.source`). Links made by hand are `manual`, and linking a keyword-linked issue by hand makes it manual, so editing the text never removes it. Cross-repo links show in both sidebars as `owner/repo#N`, to viewers who can read the other repo, with no picker toggle: editing the PR text is how one goes away.

**On merge** into the default branch, `IssueCloser.CloseForPull` closes every linked issue, manual or keyword, and every issue named in the merged commits' messages. Those are read with `ClosingRefsInMerge` before the merge writes, since a fast-forward leaves nothing between base and head afterwards; squash commits don't carry them. A merge into any other branch closes nothing.

**On push**, `IssueCloser.CloseForPush` runs after receive-pack over HTTP and SSH. Only a fast-forward of the default branch by a user counts: creating the branch, force-pushing it and deploy-key pushes close nothing. Commits are read oldest first. Server-side merges and web commits don't go through receive-pack and close nothing this way.

**Who may close:** the merger, the auto-merge arming user or the pusher, who must be able to write the issue's repo, whose repo isn't archived, and whose token, if it is limited to some repos and orgs, covers that repo. Otherwise the reference is skipped; the merge or push still succeeds. Auto-merge doesn't keep the arming token's targets, so it closes with the user's full permissions.

**Once each:** each close is a transaction that locks the issue, records an `issue_events` row and closes it. Unique indexes on `(issue_id, pull_id)` and `(issue_id, commit_sha)` for `closed` events mean a PR or a commit closes an issue at most once, even after someone reopens it. A close fires the `issues` webhook (action `closed`), `issue_closed` notifications and the `issue_closed` activity event, as a manual close does.

**Timeline:** the issue page interleaves `issue_events` with comments: "closed this in #34", "closed this in acme/api#34", "closed this in `a1b2c3d`", and manual closes and reopens, which `UpdateIssue` records. The PR or commit is left out for viewers who can't read the repo it came from.

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

A draft PR has `state = "open"` and `is_draft = true`. It is separate from closed/merged PRs. Converting to ready changes `is_draft` to `false` without touching `state`.

`draft_at` records when the PR was first marked as a draft. It is never cleared when converting to ready.

### PR List Filtering

The pulls list page (`/{owner}/{repo}/pulls`) filters by `?state=` query param:

| `?state=` | Shows                                           |
| --------- | ----------------------------------------------- |
| `open`    | Open PRs with `is_draft = false` (default)      |
| `draft`   | Open PRs with `is_draft = true`                 |
| `closed`  | Closed PRs                                      |
| `merged`  | Merged PRs                                      |

### PR Detail Page

When `is_draft = true`:
- A yellow "Draft" badge appears next to the PR title
- A yellow banner is shown: "This pull request is still a work in progress."
- All merge strategy buttons are hidden
- The review submission form is hidden
- A "Ready for review" button is shown (calls `PATCH` with `{"is_draft":"false"}`)
- The "Close PR" button remains available

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
- Store: `PullStore.SetDraft(ctx, id, isDraft)` — SQL uses `CASE WHEN $1 = TRUE THEN NOW() ELSE draft_at END` to preserve `draft_at` on convert-to-ready
- Service: `PullService.SetDraft(ctx, owner, repo, number, isDraft)` — guards against closed/merged PRs
- Handler: `UpdatePull` checks `is_draft` field before `state`; draft merge block runs before `CanMerge`
