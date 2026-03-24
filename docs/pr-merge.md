# Pull Request Merge Strategies

Cloudzilla supports three merge strategies selectable from the PR detail page.

**Diff view:** The PR detail page shows a full file diff between base and head tips for open PRs. Diff is omitted for closed/merged PRs.

## Available Strategies

| Strategy        | Button                 | When shown                           | What it does                                            |
| --------------- | ---------------------- | ------------------------------------ | ------------------------------------------------------- |
| Fast-forward    | "Merge (fast-forward)" | Head is a direct descendant of base  | Advances base branch ref — no new commit                |
| Three-way merge | "Create merge commit"  | FF or clean three-way merge possible | Creates a commit with two parents (base + head)         |
| Squash merge    | "Squash and merge"     | FF or clean three-way merge possible | Collapses head commits into a single new commit on base |

**Conflict detection:** `mergeTreesNoConflict` performs a pure tree-level three-way merge — if the same file path was modified on both sides relative to the merge base, all merge buttons are hidden and a conflict warning is shown. The developer must rebase locally and push.

## Merge Flow

1. `PagePullDetail` calls `CodeService.GetPullDiff(base, head)` → `PRDiffResult{Files, CanFastForward, CanThreeWayMerge}`
2. Template renders diff + conditionally shows strategy buttons based on capability flags
3. On button click → HTMX `PATCH /api/repos/{owner}/{repo}/pulls/{number}` with `state=merged` and `merge_strategy=ff|merge|squash`
4. `UpdatePull` dispatches to `MergePullRequest`, `ThreeWayMergePullRequest`, or `SquashMergePullRequest`
5. On success → `PullService.SetState(merged)` → fragment returned
6. On failure (conflict, missing branch, etc.) → 422 → `hx-on::response-error` fires alert

## CodeService Methods

- `GetPullDiff(owner, repo, base, head)` → `*PRDiffResult`
- `MergePullRequest(owner, repo, base, head)` → `error` (fast-forward only)
- `ThreeWayMergePullRequest(owner, repo, base, head, authorName, authorEmail)` → `error`
- `SquashMergePullRequest(owner, repo, base, head, authorName, authorEmail)` → `error`
- `checkFastForward(repo, baseCommit, headCommit)` → `bool` (private)
- `findMergeBase(repo, a, b)` → `(*object.Commit, error)` (private; LCA via ancestor walk)
- `mergeTreesNoConflict(repo, mergeBase, base, head)` → `(plumbing.Hash, bool, error)` (private)
- `flattenTree(tree)` → `(map[string]mergeFile, error)` (private)
- `buildTree(repo, files)` → `(plumbing.Hash, error)` (private; recursively encodes tree objects)

## PRDiffResult Type

```go
type PRDiffResult struct {
    Files            []FileDiff
    TotalAdded       int
    TotalDeleted     int
    CanFastForward   bool  // head is a descendant of base
    CanThreeWayMerge bool  // branches diverged but no conflicting file edits
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
