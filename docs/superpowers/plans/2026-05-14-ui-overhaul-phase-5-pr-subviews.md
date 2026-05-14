# UI Overhaul · Phase 5 · PR Sub-views — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add the three PR sub-views — Commits, Checks, Files changed — at `…/pulls/{n}/commits`, `…/pulls/{n}/checks`, `…/pulls/{n}/files`. Each reuses the PR chrome (header + sub-nav + sidebar) from `pull_detail.templ`. Add `ChecksList`, `DiffFileTree`, and shared `PullStateBadge` components. Add new `CodeService.PullCommits` helper. Wire per-file diff stats from existing `service.FileDiff` (`Added` / `Deleted`).

**Architecture:** Three new sub-routes on the existing PR route group. All three pages reuse a shared `PullChrome` fragment extracted from `pull_detail.templ`. The Checks page uses the existing `CommitStatusService.List(ctx, owner, repoName, sha)`. The Files page uses the existing `CodeService.GetPullDiff(owner, repoName, base, head) (*PRDiffResult, error)` from `internal/service/code_service_merge.go`. The Commits page uses a new `CodeService.PullCommits(owner, repoName, base, head) ([]CommitSummary, error)` placed in `internal/service/code_service_pulls.go`.

**Real types verified against the codebase (do not invent fields):**

- `model.PullRequest` has: `ID`, `RepoID`, `Number`, `AuthorID`, `AuthorName` (display name only), `Title`, `Body`, `State`, `HeadBranch`, `BaseBranch`, `CreatedAt`, `UpdatedAt`, `MergedAt`, `ClosedAt`, `IsDraft`, plus auto-merge fields. There is **no** `OpenedAt`, **no** `AuthorUsername`, **no** `BaseRef`/`HeadRef`, **no** `HeadSHA`. Use `CreatedAt` and `BaseBranch` / `HeadBranch`. Resolve a clickable username via `Services.User.GetByID(ctx, pull.AuthorID)`; the handler pre-resolves it and passes it to `PullChromeData.AuthorUsername`.
- `service.FileDiff` has: `OldPath`, `NewPath`, `IsBinary`, `IsNew`, `IsDelete`, `Added`, `Deleted`, `Hunks`. There is **no** `Path`, **no** `Additions`/`Deletions`. Use `Added` / `Deleted` and pick `NewPath` (falling back to `OldPath` for deletes) for the display path.
- `service.CommitSummary` has: `Hash` (short), `FullHash`, `Message` (first line, used as subject), `Author` (display name), `AuthorTime`.
- `CommitStatusService.List(ctx, owner, repoName, sha) ([]model.CommitStatus, error)` exists; there is **no** `ListFor`. Resolve the head SHA via `CodeService` (resolve `pull.HeadBranch` → commit hash) and pass it to `List`.
- `components.DiffHunkTable(hunks []service.DiffHunk)` is the existing per-hunk renderer (there is no `components.DiffTable`). Use it as `@components.DiffHunkTable(f.Hunks)` and pair it with `components.DiffFileHeader(f)` if a header is wanted, or render a custom section header to match the mockup.
- Service callers do **not** compute a repo path on disk — the service does that internally. Pass `owner` and `repoName` strings; `CodeService.repoPath` is private and not callable from outside.
- The existing `pullStateBadge` in `pull_detail.templ` is a file-private templ. Promote it to a shared `components.PullStateBadge(state model.PRState, isDraft bool)` component so the chrome and detail body render identical badges.

**Prerequisites:** Phase 4 merged.

**Spec:** [2026-05-14-ui-overhaul-design.md](../specs/2026-05-14-ui-overhaul-design.md)

**Branch:** `feat/ui-overhaul-phase-5-pr-subviews`

---

### Task 1: Branch setup

- [ ] `git checkout main && git pull && git checkout -b feat/ui-overhaul-phase-5-pr-subviews`

---

### Task 2: Promote `PullStateBadge` to a shared component

Both the existing PR detail page and the new chrome fragment must render the same badge. The current `pullStateBadge` in `internal/view/pages/pull_detail.templ` (lines ~401–417) is file-private. Promote it to `components.PullStateBadge(state model.PRState, isDraft bool)` so both call sites stay in sync.

**Files:**
- Create: `internal/view/components/pull_state_badge.templ`
- Create: `internal/view/components/pull_state_badge_test.go`
- Modify: `internal/view/pages/pull_detail.templ` (delete the private `pullStateBadge`, replace call site with `@components.PullStateBadge(data.Pull.State, data.Pull.IsDraft)`)

- [ ] **Step 1: Test (TDD)**

```go
// internal/view/components/pull_state_badge_test.go
func TestPullStateBadge_Variants(t *testing.T) {
    cases := []struct {
        state    model.PRState
        isDraft  bool
        wantText string
    }{
        {model.PRStateOpen, false, "Open"},
        {model.PRStateMerged, false, "Merged"},
        {model.PRStateClosed, false, "Closed"},
        {model.PRStateOpen, true, "Draft"}, // Draft wins over state
    }
    for _, tc := range cases {
        var buf bytes.Buffer
        PullStateBadge(tc.state, tc.isDraft).Render(context.Background(), &buf)
        if !strings.Contains(buf.String(), tc.wantText) {
            t.Errorf("state=%v draft=%v: missing %q in %s", tc.state, tc.isDraft, tc.wantText, buf.String())
        }
    }
}
```

- [ ] **Step 2: Implement**

```go
// internal/view/components/pull_state_badge.templ
package components

import "github.com/mkappworks-dev/cloudzilla-app/internal/model"

templ PullStateBadge(state model.PRState, isDraft bool) {
    if isDraft {
        @Badge(BadgeSecondary) { Draft }
    } else {
        switch state {
        case model.PRStateOpen:
            @Badge(BadgeSuccess) { Open }
        case model.PRStateMerged:
            @Badge(BadgeMerged) { Merged }
        default:
            @Badge(BadgeDestructive) { Closed }
        }
    }
}
```

(Verify `BadgeSecondary` exists in `internal/view/components/badge.templ`; if not, use the closest neutral variant — `BadgeOutline` or similar.)

- [ ] **Step 3: Update `pull_detail.templ`**

- Delete the file-private `templ pullStateBadge(state string)` block.
- Change the existing call site `@pullStateBadge(string(data.Pull.State))` to `@components.PullStateBadge(data.Pull.State, data.Pull.IsDraft)`.

- [ ] **Step 4: Regenerate, run test, commit**

```bash
~/go/bin/templ generate && go test ./internal/view/components/ -run TestPullStateBadge -v
git add internal/view/components/pull_state_badge.templ internal/view/components/pull_state_badge_templ.go internal/view/components/pull_state_badge_test.go internal/view/pages/pull_detail.templ internal/view/pages/pull_detail_templ.go
git commit -m "refactor(ui): extract PullStateBadge as shared component"
```

---

### Task 2b: Extract PR chrome into a shared fragment

The three sub-views share top chrome (title + state badge + sub-nav). Refactor `pull_detail.templ` so the chrome lives in a separate fragment used by all four pages.

**Files:**
- Create: `internal/view/fragments/pull_chrome.templ`
- Modify: `internal/view/pages/pull_detail.templ`

- [ ] **Step 1: Create the fragment**

```go
// internal/view/fragments/pull_chrome.templ
package fragments

import (
    "fmt"

    "github.com/mkappworks-dev/cloudzilla-app/internal/model"
    "github.com/mkappworks-dev/cloudzilla-app/internal/view/components"
)

// PullChromeData carries everything the chrome needs. The handler pre-resolves
// AuthorUsername via UserService.GetByID(pull.AuthorID) so the chrome doesn't
// have to know about services. AuthorUsername may be empty if the author was
// deleted — render fall back to data.Pull.AuthorName in that case.
type PullChromeData struct {
    OwnerName      string
    Repo           *model.Repository
    Pull           *model.PullRequest
    AuthorUsername string // resolved by handler; empty → fall back to Pull.AuthorName
    Active         string // "conversation", "commits", "checks", "files"
}

templ PullChrome(data PullChromeData, children templ.Component) {
    <div class="max-w-[1400px] mx-auto px-6 py-4 space-y-3">
        <header>
            <p class="text-sm text-muted-foreground">
                { fmt.Sprintf("%s / %s · #%d", data.OwnerName, data.Repo.Name, data.Pull.Number) }
            </p>
            <h1 class="text-2xl font-semibold tracking-tight mt-1">{ data.Pull.Title }</h1>
            <div class="mt-2 flex items-center gap-2">
                @components.PullStateBadge(data.Pull.State, data.Pull.IsDraft)
                <span class="text-sm text-muted-foreground">
                    { "opened " + data.Pull.CreatedAt.Format("Jan 2") + " by " }
                    if data.AuthorUsername != "" {
                        <a href={ templ.SafeURL("/" + data.AuthorUsername) } class="text-foreground hover:text-primary">{ data.AuthorUsername }</a>
                    } else {
                        <span class="text-foreground">{ data.Pull.AuthorName }</span>
                    }
                </span>
            </div>
        </header>
        <nav aria-label="Pull request sections" class="border-b border-border">
            <div class="flex gap-1 -mb-px">
                @pullChromeTab(data, "conversation", "Conversation", fmt.Sprintf("/%s/%s/pulls/%d", data.OwnerName, data.Repo.Name, data.Pull.Number))
                @pullChromeTab(data, "commits", "Commits", fmt.Sprintf("/%s/%s/pulls/%d/commits", data.OwnerName, data.Repo.Name, data.Pull.Number))
                @pullChromeTab(data, "checks", "Checks", fmt.Sprintf("/%s/%s/pulls/%d/checks", data.OwnerName, data.Repo.Name, data.Pull.Number))
                @pullChromeTab(data, "files", "Files changed", fmt.Sprintf("/%s/%s/pulls/%d/files", data.OwnerName, data.Repo.Name, data.Pull.Number))
            </div>
        </nav>
        @children
    </div>
}

templ pullChromeTab(data PullChromeData, key, label, href string) {
    if data.Active == key {
        <a href={ templ.SafeURL(href) } aria-current="page" class="px-3 py-2 text-sm font-medium text-foreground border-b-2 border-foreground">{ label }</a>
    } else {
        <a href={ templ.SafeURL(href) } class="px-3 py-2 text-sm text-muted-foreground hover:text-foreground border-b-2 border-transparent">{ label }</a>
    }
}
```

- [ ] **Step 2: Update `pull_detail.templ` to use the fragment**

Wrap the existing body in `@fragments.PullChrome(chromeData) { … }`. Remove the now-duplicated header/sub-nav markup. Update the handler `PagePullDetail` (or wherever the detail view-model is built) to resolve and pass `AuthorUsername` via `Services.User.GetByID(ctx, pull.AuthorID)` (treat ErrNoRows as empty string — render falls back to `AuthorName`).

- [ ] **Step 3: Regenerate, verify existing PR page still renders**

```bash
~/go/bin/templ generate && make dev
# Visit /<owner>/<repo>/pulls/<n> — should look identical to Phase 1's port
```

- [ ] **Step 4: Commit**

```bash
git add internal/view/fragments/pull_chrome.templ internal/view/fragments/pull_chrome_templ.go internal/view/pages/pull_detail.templ internal/view/pages/pull_detail_templ.go internal/handler/page_handler.go
git commit -m "refactor(ui): extract PullChrome fragment for PR sub-views"
```

---

### Task 3: `ChecksList` component — TDD

**Files:**
- Create: `internal/view/components/checks_list.templ`
- Create: `internal/view/components/checks_list_test.go`

- [ ] **Step 1: Test**

```go
func TestChecksList_RendersEachState(t *testing.T) {
    rows := []CheckRow{
        {Context: "ci/build", State: "success", Description: "passed in 2m", URL: "/runs/1"},
        {Context: "ci/test",  State: "failure", Description: "1 of 42 failed", URL: "/runs/2"},
        {Context: "ci/lint",  State: "pending", Description: "running", URL: ""},
    }
    var buf bytes.Buffer
    ChecksList(rows).Render(context.Background(), &buf)
    out := buf.String()
    for _, s := range []string{"ci/build", "ci/test", "ci/lint", "passed in 2m"} {
        if !strings.Contains(out, s) { t.Errorf("missing %q", s) }
    }
}
```

- [ ] **Step 2: Implement**

```go
// internal/view/components/checks_list.templ
package components

type CheckRow struct {
    Context     string
    State       string // "success", "failure", "pending", "error"
    Description string
    URL         string
}

templ ChecksList(rows []CheckRow) {
    <ul class="rounded-md border border-border bg-card divide-y divide-border">
        for _, r := range rows {
            <li class="flex items-center gap-3 px-4 py-3 text-sm">
                @checkStatusIcon(r.State)
                <div class="flex-1 min-w-0">
                    <p class="font-medium font-mono">{ r.Context }</p>
                    if r.Description != "" {
                        <p class="text-xs text-muted-foreground mt-0.5">{ r.Description }</p>
                    }
                </div>
                if r.URL != "" {
                    <a href={ templ.SafeURL(r.URL) } class="text-xs text-primary hover:underline">Details</a>
                }
            </li>
        }
    </ul>
}

templ checkStatusIcon(state string) {
    switch state {
    case "success":
        <span class="text-success">✓</span>
    case "failure", "error":
        <span class="text-destructive">✗</span>
    case "pending":
        <span class="text-warning">●</span>
    default:
        <span class="text-muted-foreground">—</span>
    }
}
```

- [ ] **Step 3: Regenerate, run test, commit**

```bash
~/go/bin/templ generate && go test ./internal/view/components/ -run TestChecksList -v
git add internal/view/components/checks_list.templ internal/view/components/checks_list_templ.go internal/view/components/checks_list_test.go
git commit -m "feat(ui): add ChecksList component"
```

---

### Task 4: `DiffFileTree` component — TDD

The mockup (`mockups/pr_files.html`) renders a flat list, but real PRs touch deep paths (e.g. `internal/view/pages/pulls.templ`). Render a **collapsible hierarchical tree** with directories grouping their children. Use Alpine.js for expand/collapse (Alpine is already loaded for other plugin UI per Phase 0). Items use `Added` / `Deleted` (matching `service.FileDiff`).

**Files:**
- Create: `internal/view/components/diff_file_tree.templ`
- Create: `internal/view/components/diff_file_tree_test.go`

- [ ] **Step 1: Test**

```go
func TestDiffFileTree_BuildsTreeAndRendersStats(t *testing.T) {
    items := []DiffFileTreeItem{
        {Path: "main.go", Anchor: "diff-0", Added: 12, Deleted: 3},
        {Path: "internal/util.go", Anchor: "diff-1", Added: 0, Deleted: 8},
        {Path: "internal/view/pages/pulls.templ", Anchor: "diff-2", Added: 58, Deleted: 4},
    }
    // buildDiffTree groups items by directory; assert directory nodes appear.
    root := buildDiffTree(items)
    if root.findDir("internal") == nil {
        t.Fatal("expected 'internal' directory node")
    }
    if root.findDir("internal").findDir("view") == nil {
        t.Fatal("expected nested 'internal/view' directory node")
    }

    var buf bytes.Buffer
    DiffFileTree(items).Render(context.Background(), &buf)
    out := buf.String()
    for _, s := range []string{"main.go", "util.go", "pulls.templ", "internal", "+12", "-8", "+58"} {
        if !strings.Contains(out, s) {
            t.Errorf("missing %q", s)
        }
    }
}
```

- [ ] **Step 2: Implement**

```go
// internal/view/components/diff_file_tree.templ
package components

import (
    "fmt"
    "sort"
    "strings"
)

// DiffFileTreeItem represents one changed file. Fields match service.FileDiff:
// use Added/Deleted (not Additions/Deletions). Anchor is the in-page id of
// the diff section the link should scroll to.
type DiffFileTreeItem struct {
    Path    string
    Anchor  string
    Added   int
    Deleted int
}

// diffTreeNode is a folder or file node in the hierarchy.
type diffTreeNode struct {
    Name     string
    FullPath string // empty for the synthetic root
    IsDir    bool
    Item     *DiffFileTreeItem // non-nil for leaves
    Children map[string]*diffTreeNode
}

func (n *diffTreeNode) findDir(name string) *diffTreeNode {
    if n == nil || n.Children == nil {
        return nil
    }
    c := n.Children[name]
    if c == nil || !c.IsDir {
        return nil
    }
    return c
}

// buildDiffTree groups items by directory into a nested tree.
func buildDiffTree(items []DiffFileTreeItem) *diffTreeNode {
    root := &diffTreeNode{IsDir: true, Children: map[string]*diffTreeNode{}}
    for i := range items {
        it := items[i]
        parts := strings.Split(it.Path, "/")
        cur := root
        for j, p := range parts {
            isLeaf := j == len(parts)-1
            child, ok := cur.Children[p]
            if !ok {
                child = &diffTreeNode{
                    Name:     p,
                    FullPath: strings.Join(parts[:j+1], "/"),
                    IsDir:    !isLeaf,
                    Children: map[string]*diffTreeNode{},
                }
                cur.Children[p] = child
            }
            if isLeaf {
                child.Item = &it
            }
            cur = child
        }
    }
    return root
}

// sortedChildren returns children with directories first, then files, each alpha-sorted.
func sortedChildren(n *diffTreeNode) []*diffTreeNode {
    out := make([]*diffTreeNode, 0, len(n.Children))
    for _, c := range n.Children {
        out = append(out, c)
    }
    sort.Slice(out, func(i, j int) bool {
        if out[i].IsDir != out[j].IsDir {
            return out[i].IsDir // dirs first
        }
        return out[i].Name < out[j].Name
    })
    return out
}

templ DiffFileTree(items []DiffFileTreeItem) {
    {{ root := buildDiffTree(items) }}
    <nav aria-label="Files changed" class="w-72 flex-none border-r border-border overflow-y-auto">
        <ul class="py-2 text-sm">
            @diffTreeChildren(root, 0)
        </ul>
    </nav>
}

templ diffTreeChildren(node *diffTreeNode, depth int) {
    for _, c := range sortedChildren(node) {
        if c.IsDir {
            <li x-data={ "{ open: true }" }>
                <button type="button" @click="open = !open" class="w-full flex items-center gap-1 px-2 py-1 text-muted-foreground hover:text-foreground hover:bg-accent rounded" :aria-expanded="open">
                    <svg x-show="open" width="11" height="11" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" aria-hidden="true"><polyline points="6 9 12 15 18 9"/></svg>
                    <svg x-show="!open" width="11" height="11" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" aria-hidden="true"><polyline points="9 6 15 12 9 18"/></svg>
                    <span class="font-mono truncate flex-1 text-left" style={ fmt.Sprintf("padding-left:%dpx", depth*8) }>{ c.Name }</span>
                </button>
                <ul x-show="open" class="ml-2">
                    @diffTreeChildren(c, depth+1)
                </ul>
            </li>
        } else if c.Item != nil {
            <li>
                <a href={ templ.SafeURL("#" + c.Item.Anchor) } class="flex items-center gap-2 px-2 py-1 text-muted-foreground hover:text-foreground hover:bg-accent rounded">
                    <span class="font-mono truncate flex-1" style={ fmt.Sprintf("padding-left:%dpx", depth*8) }>{ c.Name }</span>
                    if c.Item.Added > 0 {
                        <span class="text-success text-xs">{ fmt.Sprintf("+%d", c.Item.Added) }</span>
                    }
                    if c.Item.Deleted > 0 {
                        <span class="text-destructive text-xs">{ fmt.Sprintf("-%d", c.Item.Deleted) }</span>
                    }
                </a>
            </li>
        }
    }
}
```

- [ ] **Step 3: Regenerate, test, commit**

```bash
~/go/bin/templ generate && go test ./internal/view/components/ -run TestDiffFileTree -v
git add internal/view/components/diff_file_tree.templ internal/view/components/diff_file_tree_templ.go internal/view/components/diff_file_tree_test.go
git commit -m "feat(ui): add DiffFileTree component with collapsible folders"
```

---

### Task 5: `pr_commits.templ` page

**Files:**
- Create: `internal/service/code_service_pulls.go` (new `PullCommits` helper)
- Create: `internal/service/code_service_pulls_test.go`
- Create: `internal/view/pages/pr_commits.templ`
- Modify: `internal/router/router.go` (route + `pageNames`)
- Modify: `internal/handler/page_handler.go` (add `PagePullCommits`)
- Modify: `internal/handler/viewmodels.go` (or wherever page view-models live — see CLAUDE.md) for `PullCommitsData`

- [ ] **Step 1: `CodeService.PullCommits` — TDD**

Add the helper next to existing pull helpers in `code_service_merge.go`. Place new code in a separate file to keep diffs small.

```go
// internal/service/code_service_pulls.go
package service

import (
    gogit "github.com/go-git/go-git/v5"
    "github.com/go-git/go-git/v5/plumbing"
    "github.com/go-git/go-git/v5/plumbing/object"
    "github.com/go-git/go-git/v5/plumbing/storer"
)

// PullCommits returns commits reachable from head but not from base, in
// reverse-chronological order. This is the list of commits a PR "adds" on top
// of its base branch. Returns CommitSummary (already defined in code_service_commit.go).
func (s *CodeService) PullCommits(owner, repoName, base, head string) ([]CommitSummary, error) {
    repo, err := gogit.PlainOpen(s.repoPath(owner, repoName))
    if err != nil {
        return nil, err
    }
    baseCommit, _, err := resolveRef(repo, base)
    if err != nil {
        return nil, err
    }
    headCommit, _, err := resolveRef(repo, head)
    if err != nil {
        return nil, err
    }

    // Collect all ancestors of base so we can exclude them.
    excluded := make(map[plumbing.Hash]bool)
    iterBase, err := repo.Log(&gogit.LogOptions{From: baseCommit.Hash})
    if err != nil {
        return nil, err
    }
    _ = iterBase.ForEach(func(c *object.Commit) error {
        excluded[c.Hash] = true
        return nil
    })
    iterBase.Close()

    // Walk head, stop at the first excluded ancestor on each path.
    iter, err := repo.Log(&gogit.LogOptions{From: headCommit.Hash, Order: gogit.LogOrderCommitterTime})
    if err != nil {
        return nil, err
    }
    defer iter.Close()

    var out []CommitSummary
    err = iter.ForEach(func(c *object.Commit) error {
        if excluded[c.Hash] {
            return storer.ErrStop
        }
        out = append(out, summarize(c))
        return nil
    })
    return out, err
}

// summarize is a small extract of the GetCommits row builder so PullCommits
// produces identical CommitSummary shape. Move the existing inline code from
// code_service_commit.go into this helper if shared.
func summarize(c *object.Commit) CommitSummary {
    hash := c.Hash.String()
    short := hash
    if len(short) > 7 {
        short = short[:7]
    }
    msg := c.Message
    first := msg
    for i, r := range msg {
        if r == '\n' {
            first = msg[:i]
            break
        }
    }
    return CommitSummary{
        Hash:       short,
        FullHash:   hash,
        Message:    first,
        Author:     c.Author.Name,
        AuthorTime: c.Author.When,
    }
}
```

Write a test that uses an in-process repo via `go-git` (mirror the style of any existing service test that creates commits) covering: linear FF history (head adds N commits), divergent history (only head-only commits returned), no-op (head == base → empty).

- [ ] **Step 2: Register route**

In `internal/router/router.go`:

```go
r.Get("/{owner}/{repo}/pulls/{number}/commits", h.PagePullCommits)
```

Also append `"pull_commits"` to `pageNames` (per CLAUDE.md step 11).

- [ ] **Step 3: Handler**

```go
func (h *Handler) PagePullCommits(w http.ResponseWriter, r *http.Request) {
    owner := chi.URLParam(r, "owner")
    repoName := chi.URLParam(r, "repo")
    num, _ := strconv.Atoi(chi.URLParam(r, "number"))

    repo, err := h.Services.Repo.GetByOwnerAndName(r.Context(), owner, repoName)
    if err != nil { http.NotFound(w, r); return }
    pull, err := h.Services.Pull.GetByNumber(r.Context(), repo.ID, num)
    if err != nil { http.NotFound(w, r); return }

    commits, err := h.Services.Code.PullCommits(owner, repoName, pull.BaseBranch, pull.HeadBranch)
    if err != nil {
        // Log + render empty. Don't 500 on git errors — show the chrome with
        // an empty state so the sub-nav remains navigable.
        h.Logger.Warn("PullCommits", "err", err, "owner", owner, "repo", repoName, "pr", num)
        commits = nil
    }

    authorUsername := ""
    if u, uerr := h.Services.User.GetByID(r.Context(), pull.AuthorID); uerr == nil && u != nil {
        authorUsername = u.Username
    }

    data := PullCommitsData{
        BasePage:       h.basePage(r),
        OwnerName:      owner,
        Repo:           repo,
        Pull:           pull,
        AuthorUsername: authorUsername,
        Commits:        commits,
    }
    h.render(w, r, pages.PullCommits(data), "Commits · "+pull.Title)
}
```

- [ ] **Step 4: Template**

```go
// internal/view/pages/pr_commits.templ
package pages

import (
    "github.com/mkappworks-dev/cloudzilla-app/internal/handler"
    "github.com/mkappworks-dev/cloudzilla-app/internal/view/fragments"
    "github.com/mkappworks-dev/cloudzilla-app/internal/view/layout"
)

// commitDays groups []service.CommitSummary by calendar day using AuthorTime.
// Implement in internal/view/pages/helpers.go. Returns []CommitDayGroup{Date, Commits}.

templ PullCommits(data handler.PullCommitsData) {
    @layout.Base(data.BasePage, "Commits · " + data.Pull.Title) {
        @fragments.PullChrome(fragments.PullChromeData{
            OwnerName: data.OwnerName, Repo: data.Repo, Pull: data.Pull,
            AuthorUsername: data.AuthorUsername, Active: "commits",
        }) {
            <div class="mt-4 space-y-1">
                if len(data.Commits) == 0 {
                    @components.EmptyState("No commits between base and head.")
                } else {
                    for _, day := range commitDays(data.Commits) {
                        <h3 class="text-xs font-mono text-muted-foreground uppercase tracking-wider mt-4">{ day.Date.Format("Mon, Jan 2 2006") }</h3>
                        <ul class="rounded-md border border-border bg-card divide-y divide-border">
                            for _, c := range day.Commits {
                                <li class="flex items-center gap-3 px-3 py-2">
                                    <span class="font-mono text-xs text-muted-foreground">{ c.Hash }</span>
                                    <p class="flex-1 text-sm truncate">{ c.Message }</p>
                                    <span class="text-xs text-muted-foreground">{ c.Author }</span>
                                </li>
                            }
                        </ul>
                    }
                }
            </div>
        }
    }
}
```

- [ ] **Step 5: Wire view-model**

Define `PullCommitsData` next to existing handler view-models (per CLAUDE.md, view-models live in `internal/handler/viewmodels.go`, not `internal/view/view.go`):

```go
type PullCommitsData struct {
    BasePage       BasePage
    OwnerName      string
    Repo           *model.Repository
    Pull           *model.PullRequest
    AuthorUsername string
    Commits        []service.CommitSummary
}
```

- [ ] **Step 6: Regenerate, verify, commit**

```bash
~/go/bin/templ generate && make dev
# Visit /<owner>/<repo>/pulls/<n>/commits
git add internal/service/code_service_pulls.go internal/service/code_service_pulls_test.go internal/view/pages/pr_commits.templ internal/view/pages/pr_commits_templ.go internal/view/pages/helpers.go internal/handler/page_handler.go internal/handler/viewmodels.go internal/router/router.go
git commit -m "feat(ui): add PR commits sub-view"
```

---

### Task 6: `pr_checks.templ` page

**Files:**
- Create: `internal/view/pages/pr_checks.templ`
- Modify: `internal/router/router.go`, `internal/handler/page_handler.go`

- [ ] **Step 1: Register + handler**

The existing `CommitStatusService.List(ctx, owner, repoName, sha) ([]model.CommitStatus, error)` takes a SHA, not a branch. Resolve `pull.HeadBranch` → SHA via the same `resolveRef` path that `GetPullDiff` uses. The simplest way: call `CodeService.GetCommits(owner, repoName, pull.HeadBranch, 1, 1)` and take `commits[0].FullHash`. (If a more direct `ResolveRefSHA` helper is added in another phase, switch to that.) Confirm `model.CommitStatus` field names by reading `internal/model/commit_status.go` — common names are `Context`, `State`, `Description`, `TargetURL`, but verify before coding.

```go
r.Get("/{owner}/{repo}/pulls/{number}/checks", h.PagePullChecks)

func (h *Handler) PagePullChecks(w http.ResponseWriter, r *http.Request) {
    // ...load repo, pull (same as PagePullCommits)...

    // Resolve head SHA from head branch.
    headSHA := ""
    if log, lerr := h.Services.Code.GetCommits(owner, repoName, pull.HeadBranch, 1, 1); lerr == nil && len(log.Commits) > 0 {
        headSHA = log.Commits[0].FullHash
    }

    var rows []model.CommitStatus
    if headSHA != "" {
        if r2, serr := h.Services.CommitStatus.List(r.Context(), owner, repoName, headSHA); serr == nil {
            rows = r2
        }
    }

    crows := make([]components.CheckRow, 0, len(rows))
    for _, s := range rows {
        crows = append(crows, components.CheckRow{
            Context:     s.Context,
            State:       string(s.State),     // model.CommitStatusState is its own type
            Description: s.Description,
            URL:         s.TargetURL,         // verify field name when implementing
        })
    }

    authorUsername := ""
    if u, uerr := h.Services.User.GetByID(r.Context(), pull.AuthorID); uerr == nil && u != nil {
        authorUsername = u.Username
    }

    data := PullChecksData{
        BasePage: h.basePage(r), OwnerName: owner, Repo: repo, Pull: pull,
        AuthorUsername: authorUsername, Rows: crows,
    }
    h.render(w, r, pages.PullChecks(data), "Checks · "+pull.Title)
}
```

Add `PullChecksData` to `internal/handler/viewmodels.go` mirroring `PullCommitsData` but with `Rows []components.CheckRow`.

- [ ] **Step 2: Template**

```go
templ PullChecks(data handler.PullChecksData) {
    @layout.Base(data.BasePage, "Checks · " + data.Pull.Title) {
        @fragments.PullChrome(fragments.PullChromeData{
            OwnerName: data.OwnerName, Repo: data.Repo, Pull: data.Pull,
            AuthorUsername: data.AuthorUsername, Active: "checks",
        }) {
            <div class="mt-4">
                if len(data.Rows) == 0 {
                    @components.EmptyState("No status checks reported for this PR yet.")
                } else {
                    @components.ChecksList(data.Rows)
                }
            </div>
        }
    }
}
```

- [ ] **Step 3: Regenerate, verify, commit**

```bash
~/go/bin/templ generate && make dev
git add internal/view/pages/pr_checks.templ internal/view/pages/pr_checks_templ.go internal/handler/page_handler.go internal/router/router.go internal/view/view.go
git commit -m "feat(ui): add PR checks sub-view"
```

---

### Task 7: `pr_files.templ` page

**Files:**
- Create: `internal/view/pages/pr_files.templ`
- Modify: `internal/router/router.go`, `internal/handler/page_handler.go`, `internal/view/view.go`

- [ ] **Step 1: Register + handler**

Use the existing `CodeService.GetPullDiff(owner, repoName, base, head) (*PRDiffResult, error)` from `internal/service/code_service_merge.go`. `PRDiffResult.Files` is `[]service.FileDiff` with `OldPath`, `NewPath`, `IsBinary`, `IsNew`, `IsDelete`, `Added`, `Deleted`, `Hunks`. No new service work is needed for stats — they are already populated.

```go
r.Get("/{owner}/{repo}/pulls/{number}/files", h.PagePullFiles)

// displayPath returns the path to show in tree + header for a FileDiff.
func displayPath(f service.FileDiff) string {
    if f.IsDelete {
        return f.OldPath
    }
    return f.NewPath
}

func (h *Handler) PagePullFiles(w http.ResponseWriter, r *http.Request) {
    // ...load repo, pull (same as PagePullCommits)...

    diff, err := h.Services.Code.GetPullDiff(owner, repoName, pull.BaseBranch, pull.HeadBranch)
    if err != nil {
        h.Logger.Warn("GetPullDiff", "err", err, "owner", owner, "repo", repoName, "pr", num)
        diff = &service.PRDiffResult{} // render empty state inside chrome
    }

    tree := make([]components.DiffFileTreeItem, 0, len(diff.Files))
    for i, f := range diff.Files {
        tree = append(tree, components.DiffFileTreeItem{
            Path:    displayPath(f),
            Anchor:  fmt.Sprintf("diff-%d", i),
            Added:   f.Added,
            Deleted: f.Deleted,
        })
    }

    authorUsername := ""
    if u, uerr := h.Services.User.GetByID(r.Context(), pull.AuthorID); uerr == nil && u != nil {
        authorUsername = u.Username
    }

    data := PullFilesData{
        BasePage: h.basePage(r), OwnerName: owner, Repo: repo, Pull: pull,
        AuthorUsername: authorUsername, Tree: tree, Diff: diff,
    }
    h.render(w, r, pages.PullFiles(data), "Files changed · "+pull.Title)
}
```

Add `PullFilesData` to `viewmodels.go`:

```go
type PullFilesData struct {
    BasePage       BasePage
    OwnerName      string
    Repo           *model.Repository
    Pull           *model.PullRequest
    AuthorUsername string
    Tree           []components.DiffFileTreeItem
    Diff           *service.PRDiffResult
}
```

- [ ] **Step 2: Template**

The existing diff renderer is `components.DiffHunkTable(hunks []service.DiffHunk)` (there is no `components.DiffTable`). The file header can either reuse `components.DiffFileHeader(f)` or be written inline to match the mockup more closely. Inline version below keeps the +/- stats next to the path and is closer to `mockups/pr_files.html`.

```go
templ PullFiles(data handler.PullFilesData) {
    @layout.Base(data.BasePage, "Files changed · " + data.Pull.Title) {
        @fragments.PullChrome(fragments.PullChromeData{
            OwnerName: data.OwnerName, Repo: data.Repo, Pull: data.Pull,
            AuthorUsername: data.AuthorUsername, Active: "files",
        }) {
            <div class="mt-4 flex gap-4">
                if len(data.Diff.Files) == 0 {
                    @components.EmptyState("No changes.")
                } else {
                    @components.DiffFileTree(data.Tree)
                    <main class="flex-1 min-w-0 space-y-4">
                        for i, f := range data.Diff.Files {
                            {{ path := f.NewPath }}
                            if f.IsDelete {
                                {{ path = f.OldPath }}
                            }
                            <section id={ fmt.Sprintf("diff-%d", i) } class="rounded-md border border-border bg-card">
                                <header class="px-4 py-2 border-b border-border flex items-center gap-3 text-sm">
                                    <span class="font-mono truncate flex-1">{ path }</span>
                                    if !f.IsBinary {
                                        <span class="text-success">{ fmt.Sprintf("+%d", f.Added) }</span>
                                        <span class="text-destructive">{ fmt.Sprintf("-%d", f.Deleted) }</span>
                                    }
                                </header>
                                if f.IsBinary {
                                    <p class="px-4 py-3 text-sm text-muted-foreground">Binary file not shown.</p>
                                } else {
                                    @components.DiffHunkTable(f.Hunks)
                                }
                            </section>
                        }
                    </main>
                }
            </div>
        }
    }
}
```

- [ ] **Step 3: Regenerate, verify, commit**

```bash
~/go/bin/templ generate && make dev
git add internal/view/pages/pr_files.templ internal/view/pages/pr_files_templ.go internal/handler/page_handler.go internal/router/router.go internal/view/view.go internal/service/code_service.go
git commit -m "feat(ui): add PR files-changed sub-view"
```

---

### Task 8: Verify and open PR

- [ ] Tests + lint + templ regen + visual sweep across three sub-views in both themes. silent-failure-hunter.

```bash
git push -u origin feat/ui-overhaul-phase-5-pr-subviews
gh pr create --title "feat(ui): UI overhaul phase 5 — PR sub-views" --body "$(cat <<'EOF'
## Summary
- Adds three PR sub-views: Commits, Checks, Files changed.
- Extracts PR chrome into a shared `PullChrome` fragment used by all four PR pages.
- Promotes `PullStateBadge` to a shared `components.PullStateBadge(state, isDraft)` so the chrome and detail body stay in sync.
- New components: `ChecksList`, `DiffFileTree` (hierarchical, collapsible).
- Adds `CodeService.PullCommits(owner, repoName, base, head) []CommitSummary` in `code_service_pulls.go`.
- Reuses the existing `CodeService.GetPullDiff` and `CommitStatusService.List` — no changes needed to per-file stats (`FileDiff.Added`/`Deleted` already populated).

Spec: docs/superpowers/specs/2026-05-14-ui-overhaul-design.md
Plan: docs/superpowers/plans/2026-05-14-ui-overhaul-phase-5-pr-subviews.md

## Test plan
- [x] `go test ./...` passes
- [x] Visual: three sub-views in both themes
- [x] Navigate via PR sub-nav between Conversation / Commits / Checks / Files
- [x] silent-failure-hunter clean

🤖 Generated with [Claude Code](https://claude.com/claude-code)
EOF
)"
```

---

## Self-review checklist

- [ ] `PullChrome` fragment and `pull_detail.templ` both render via the shared `components.PullStateBadge(state, isDraft)` — no duplicated badge logic.
- [ ] No reference anywhere to non-existent fields: `Pull.OpenedAt`, `Pull.AuthorUsername`, `Pull.BaseRef`, `Pull.HeadRef`, `Pull.HeadSHA`, `FileDiff.Path`, `FileDiff.Additions`, `FileDiff.Deletions`, `CommitStatusService.ListFor`, `CodeService.Diff`, `components.DiffTable`, or `repoPathFor`.
- [ ] All three sub-routes accept the same `{owner}/{repo}/pulls/{number}` prefix as the existing route, and each page name is appended to `pageNames` in `router.go`.
- [ ] `DiffFileTree` anchor IDs match the `id="diff-N"` section IDs in the files page body so in-page jumping works.
- [ ] `DiffFileTree` renders as a hierarchical, collapsible tree (Alpine `x-data="{open:true}"`), folders first, then files, both alpha-sorted.
- [ ] Checks page handles an empty result with `components.EmptyState`; handler gracefully degrades when head SHA can't be resolved.
- [ ] Files page renders binary files with "Binary file not shown" and never tries to diff them.
- [ ] `PagePullCommits` / `PagePullChecks` / `PagePullFiles` all resolve `AuthorUsername` via `Services.User.GetByID(pull.AuthorID)` and pass it into `PullChromeData`; rendering falls back to `Pull.AuthorName` on miss.
- [ ] No handler passes a disk path to `CodeService` — all calls use `(owner, repoName, ...)`.
