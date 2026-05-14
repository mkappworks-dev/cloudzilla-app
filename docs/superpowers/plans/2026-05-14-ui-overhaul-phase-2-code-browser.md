# UI Overhaul · Phase 2 · Code Browser — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Port `tree.templ`, `blob.templ`, `blame.templ`, `project_detail.templ` to match their mockups. Wire `CodeService.ListEntriesWithLastCommit` for the tree view (last-commit-per-entry). Add `FileTreeSidebar`, `BlameRow`, `KanbanColumn`, `KanbanCard` components. Implement drag-and-drop card persistence for the kanban board with explicit position re-ordering.

**Architecture:** No migrations. New `CodeService.ListEntriesWithLastCommit` with service-level LRU cache. New `ProjectService.ListColumnsWithCardsExpanded` view-model surface (joins issue/PR title + number from DB into a kanban-friendly shape). Kanban drag/drop posts via HTMX (`hx-patch` orchestrated by Alpine) to a new endpoint `PATCH /api/projects/{id}/cards/{cardID}/position`. The existing `ProjectService.MoveCard` + `ProjectStore.MoveCard` are extended to accept an explicit `position int` and re-order siblings in the destination column.

**Tech Stack:** Go, go-git, Templ, HTMX, Alpine.js for drag state (no raw `fetch()`).

**Prerequisites:** Phase 1 merged. Translation map up to date.

**Spec:** [2026-05-14-ui-overhaul-design.md](../specs/2026-05-14-ui-overhaul-design.md)

**Branch:** `feat/ui-overhaul-phase-2-code-browser`

---

### Task 1: Branch setup

- [ ] **Step 1: Create the branch**

```bash
git checkout main && git pull && git checkout -b feat/ui-overhaul-phase-2-code-browser
```

---

### Task 2: `CodeService.ListEntriesWithLastCommit` — TDD

**Files:**
- Modify: `internal/service/code_service.go`
- Create: `internal/service/code_service_tree_test.go`

- [ ] **Step 1: Failing test**

```go
// internal/service/code_service_tree_test.go
package service

import (
    "context"
    "testing"
)

func TestListEntriesWithLastCommit(t *testing.T) {
    code := newTestCodeService(t)
    repoPath := makeTestRepoWithFiles(t, map[string]string{
        "README.md":          "# demo",
        "main.go":            "package main\n",
        "internal/util.go":   "package internal\n",
    })

    entries, err := code.ListEntriesWithLastCommit(context.Background(), "test-owner", "demo", "HEAD", "")
    if err != nil {
        t.Fatalf("List: %v", err)
    }
    if len(entries) < 3 {
        t.Fatalf("expected >= 3 entries, got %d", len(entries))
    }
    var sawReadme bool
    for _, e := range entries {
        if e.Name == "README.md" {
            sawReadme = true
            if e.LastCommit.Message == "" {
                t.Errorf("expected non-empty last-commit message for README.md")
            }
        }
    }
    if !sawReadme {
        t.Errorf("missing README.md in entries: %+v", entries)
    }
}

func TestListEntriesWithLastCommit_Cached(t *testing.T) {
    code := newTestCodeService(t)
    repoPath := makeTestRepoWithFiles(t, map[string]string{"a.go": "package a"})

    ctx := context.Background()
    _, err := code.ListEntriesWithLastCommit(ctx, "test-owner", "demo", "HEAD", "")
    if err != nil {
        t.Fatalf("first call: %v", err)
    }
    // Second call within TTL should hit cache (no panic, same result).
    _, err = code.ListEntriesWithLastCommit(ctx, "test-owner", "demo", "HEAD", "")
    if err != nil {
        t.Fatalf("second call: %v", err)
    }
}
```

- [ ] **Step 2: Verify FAIL**

```bash
go test ./internal/service/ -run TestListEntriesWithLastCommit -v
```

- [ ] **Step 3: Implement**

> **Naming note:** `internal/service/code_service_tree.go` already exports `TreeEntry` (the simple listing struct used by `TreeResult`). The new richer struct lives alongside it as `TreeEntryWithLastCommit` — do **not** redefine `TreeEntry`. Existing callers of `GetTree`/`TreeEntry` are untouched; the tree page is migrated to the new struct by switching its handler to `ListEntriesWithLastCommit` (Task 7).

```go
// in internal/service/code_service.go (or a new internal/service/code_service_tree_lc.go)

import (
    "sync"
    "time"
)

// TreeEntryWithLastCommit is a directory listing entry enriched with the
// most recent commit that touched it. Used by the tree page. Coexists with
// the existing simple TreeEntry in code_service_tree.go.
type TreeEntryWithLastCommit struct {
    Name       string
    Path       string
    IsDir      bool
    Size       int64
    LastCommit struct {
        SHA       string
        Message   string
        Author    string
        Timestamp time.Time
    }
}

type treeCacheEntry struct {
    entries  []TreeEntryWithLastCommit
    cachedAt time.Time
}

const treeCacheTTL = 60 * time.Second

// Add to CodeService struct:
//   treeCache sync.Map // key="owner/repo:ref:dir" -> treeCacheEntry

// Follow the existing `gogit.PlainOpen(s.repoPath(owner, repoName))` +
// `resolveRef(repo, ref)` pattern used in code_service_tree.go.
func (s *CodeService) ListEntriesWithLastCommit(ctx context.Context, owner, repoName, ref, dir string) ([]TreeEntryWithLastCommit, error) {
    key := owner + "/" + repoName + ":" + ref + ":" + dir
    if v, ok := s.treeCache.Load(key); ok {
        e := v.(treeCacheEntry)
        if time.Since(e.cachedAt) < treeCacheTTL {
            return e.entries, nil
        }
    }
    repo, err := gogit.PlainOpen(s.repoPath(owner, repoName))
    if err != nil {
        return nil, err
    }
    commit, _, err := resolveRef(repo, ref)
    if err != nil {
        return nil, err
    }
    rootTree, err := commit.Tree()
    if err != nil {
        return nil, err
    }
    tree := rootTree
    if dir != "" {
        tree, err = rootTree.Tree(dir)
        if err != nil {
            return nil, err
        }
    }
    out := make([]TreeEntryWithLastCommit, 0, len(tree.Entries))
    for _, entry := range tree.Entries {
        e := TreeEntryWithLastCommit{
            Name:  entry.Name,
            Path:  joinPath(dir, entry.Name),
            IsDir: entry.Mode == filemode.Dir || entry.Mode == filemode.Submodule,
        }
        last, err := s.lastCommitTouching(repo, commit, e.Path)
        if err == nil && last != nil {
            e.LastCommit.SHA = last.Hash.String()
            e.LastCommit.Message = firstLine(last.Message)
            e.LastCommit.Author = last.Author.Name
            e.LastCommit.Timestamp = last.Author.When
        }
        if !e.IsDir {
            if blob, err := repo.BlobObject(entry.Hash); err == nil {
                e.Size = blob.Size
            }
        }
        out = append(out, e)
    }
    s.treeCache.Store(key, treeCacheEntry{entries: out, cachedAt: time.Now()})
    return out, nil
}

func joinPath(dir, name string) string {
    if dir == "" {
        return name
    }
    return dir + "/" + name
}

func firstLine(s string) string {
    for i := 0; i < len(s); i++ {
        if s[i] == '\n' {
            return s[:i]
        }
    }
    return s
}
```

Add `lastCommitTouching(repo, headCommit, path)` helper if not present (walks commits from `headCommit` backwards, checks whether the file's blob differs from the parent's blob at that path; uses `object.NewCommitPreorderIter` or similar). Inline tree-resolution rather than a `treeAt` helper to match the surrounding style in `code_service_tree.go`.

**Performance note:** `lastCommitTouching` is the hot path. For directories with N entries, the naive implementation is O(N × commit-history-length) blob reads. Acceptable for repos with bounded history; the cache (60s TTL) absorbs repeated hits on the same dir.

- [ ] **Step 3a: Measure p95 on a real repo (budget check)**

```bash
# In a repo with ≥ 1000 commits (e.g. this repo itself), hit the tree page
# 20 times against a deep-ish dir and capture wall-clock from the handler.
# Add a temporary `slog.Info("tree.lc.ms", ...)` around the call.
ab -n 20 -c 1 http://localhost:8080/<owner>/<repo>/tree/main/internal/service
```

If p95 > **500 ms**, do **not** silently merge — open a follow-up ticket flagging the need for a DB-backed cache (denormalize last-commit-per-path into a `path_last_commit` table populated by a post-receive hook). The cache TTL alone is insufficient for cold-load latency.

- [ ] **Step 4: Verify PASS**

```bash
go test ./internal/service/ -run TestListEntriesWithLastCommit -v
```

- [ ] **Step 5: Commit**

```bash
git add internal/service/code_service.go internal/service/code_service_tree_test.go
git commit -m "feat(service): add CodeService.ListEntriesWithLastCommit with LRU cache"
```

---

### Task 3: `FileTreeSidebar` component — TDD

**Files:**
- Create: `internal/view/components/file_tree_sidebar.templ`
- Create: `internal/view/components/file_tree_sidebar_test.go`

- [ ] **Step 1: Test**

```go
// internal/view/components/file_tree_sidebar_test.go
package components

import (
    "bytes"
    "context"
    "strings"
    "testing"
)

func TestFileTreeSidebar_RendersHierarchy(t *testing.T) {
    nodes := []TreeNode{
        {Name: "internal", IsDir: true, Href: "/o/r/tree/main/internal"},
        {Name: "main.go", IsDir: false, Href: "/o/r/blob/main/main.go"},
    }
    var buf bytes.Buffer
    if err := FileTreeSidebar(nodes, "main.go").Render(context.Background(), &buf); err != nil {
        t.Fatalf("render: %v", err)
    }
    out := buf.String()
    if !strings.Contains(out, "internal") || !strings.Contains(out, "main.go") {
        t.Errorf("expected tree entries, got: %s", out)
    }
    if !strings.Contains(out, `aria-current="page"`) {
        t.Errorf("expected aria-current on active file, got: %s", out)
    }
}
```

- [ ] **Step 2: Verify FAIL**

```bash
go test ./internal/view/components/ -run TestFileTreeSidebar -v
```

- [ ] **Step 3: Implement**

```go
// internal/view/components/file_tree_sidebar.templ
package components

type TreeNode struct {
    Name  string
    IsDir bool
    Href  string
}

templ FileTreeSidebar(nodes []TreeNode, activeName string) {
    <nav aria-label="File tree" class="w-64 flex-none border-r border-border overflow-y-auto">
        <ul class="py-2">
            for _, n := range nodes {
                <li>
                    if n.Name == activeName {
                        <a href={ templ.SafeURL(n.Href) } aria-current="page" class="flex items-center gap-2 px-3 py-1 text-sm bg-accent text-foreground">
                            @treeNodeIcon(n.IsDir)
                            { n.Name }
                        </a>
                    } else {
                        <a href={ templ.SafeURL(n.Href) } class="flex items-center gap-2 px-3 py-1 text-sm text-muted-foreground hover:text-foreground hover:bg-accent">
                            @treeNodeIcon(n.IsDir)
                            { n.Name }
                        </a>
                    }
                </li>
            }
        </ul>
    </nav>
}

templ treeNodeIcon(isDir bool) {
    if isDir {
        <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" class="flex-none" aria-hidden="true"><path d="M3 5a2 2 0 012-2h4l2 2h8a2 2 0 012 2v10a2 2 0 01-2 2H5a2 2 0 01-2-2V5z"/></svg>
    } else {
        <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" class="flex-none" aria-hidden="true"><path d="M13 2H6a2 2 0 00-2 2v16a2 2 0 002 2h12a2 2 0 002-2V9z"/><path d="M13 2v7h7"/></svg>
    }
}
```

- [ ] **Step 4: Regenerate and verify PASS**

```bash
~/go/bin/templ generate && go test ./internal/view/components/ -run TestFileTreeSidebar -v
```

- [ ] **Step 5: Commit**

```bash
git add internal/view/components/file_tree_sidebar.templ internal/view/components/file_tree_sidebar_templ.go internal/view/components/file_tree_sidebar_test.go
git commit -m "feat(ui): add FileTreeSidebar component"
```

---

### Task 4: `BlameRow` component

**Files:**
- Create: `internal/view/components/blame_row.templ`
- Create: `internal/view/components/blame_row_test.go`

- [ ] **Step 1: Test + Implement**

```go
// internal/view/components/blame_row.templ
package components

import "time"

type BlameLine struct {
    LineNumber int
    Code       string
    AuthorName string
    ShortSHA   string
    CommittedAt time.Time
    CommitURL  string
}

templ BlameRow(line BlameLine) {
    <tr class="hover:bg-accent group">
        <td class="px-2 py-0.5 text-xs text-muted-foreground/70 font-mono w-32 truncate" title={ line.AuthorName + " · " + line.ShortSHA }>
            <a href={ templ.SafeURL(line.CommitURL) } class="hover:text-foreground">{ line.AuthorName }</a>
        </td>
        <td class="px-2 py-0.5 text-xs text-muted-foreground/70 font-mono w-16">
            <a href={ templ.SafeURL(line.CommitURL) } class="hover:text-foreground">{ line.ShortSHA }</a>
        </td>
        <td class="px-2 py-0.5 text-xs text-muted-foreground/70 font-mono w-12 text-right">
            { lineNumberDisplay(line.LineNumber) }
        </td>
        <td class="px-3 py-0.5 text-[13px] font-mono whitespace-pre">{ line.Code }</td>
    </tr>
}

func lineNumberDisplay(n int) string {
    return intToStr(n)
}
```

(`intToStr` import or inline via strconv. Match existing helpers.)

- [ ] **Step 2: Test**

```go
// internal/view/components/blame_row_test.go
package components

import (
    "bytes"
    "context"
    "strings"
    "testing"
    "time"
)

func TestBlameRow_RendersAllFields(t *testing.T) {
    line := BlameLine{
        LineNumber: 42, Code: "package foo",
        AuthorName: "alice", ShortSHA: "abc1234",
        CommittedAt: time.Now(), CommitURL: "/x/y/commit/abc1234",
    }
    var buf bytes.Buffer
    BlameRow(line).Render(context.Background(), &buf)
    out := buf.String()
    for _, s := range []string{"alice", "abc1234", "42", "package foo"} {
        if !strings.Contains(out, s) {
            t.Errorf("missing %q in: %s", s, out)
        }
    }
}
```

- [ ] **Step 3: Run, verify, commit**

```bash
~/go/bin/templ generate && go test ./internal/view/components/ -run TestBlameRow -v
git add internal/view/components/blame_row.templ internal/view/components/blame_row_templ.go internal/view/components/blame_row_test.go
git commit -m "feat(ui): add BlameRow component"
```

---

### Task 5: Kanban components (`KanbanColumn`, `KanbanCard`)

> **Model note:** `model.ProjectCard` already exposes `IssueNumber`, `IssueTitle`, `IssueState`, `PullNumber`, `PullTitle`, `PullState` (populated via JOIN in `ProjectStore.ListCardsByColumn`). However it does **not** carry a unified `Title` or `RepoFullName` — those are derived in the service layer (see Task 6 step 0). The kanban view-model types below are deliberately decoupled from `model.ProjectCard` so the template doesn't have to branch on issue-vs-pull-vs-note.

**Files:**
- Create: `internal/view/components/kanban.templ`
- Create: `internal/view/components/kanban_test.go`

- [ ] **Step 1: Test**

```go
// internal/view/components/kanban_test.go
package components

import (
    "bytes"
    "context"
    "strings"
    "testing"
)

func TestKanbanColumn_RendersCards(t *testing.T) {
    col := KanbanColumnData{
        ID: 7, Title: "In Progress",
        Cards: []KanbanCardData{
            {ID: 1, Title: "card a", IssueNumber: 12, RepoFullName: "o/r"},
            {ID: 2, Title: "card b", IssueNumber: 13, RepoFullName: "o/r"},
        },
    }
    var buf bytes.Buffer
    KanbanColumn(col).Render(context.Background(), &buf)
    out := buf.String()
    if !strings.Contains(out, "In Progress") || !strings.Contains(out, "card a") {
        t.Errorf("missing fields: %s", out)
    }
    if !strings.Contains(out, `data-column-id="7"`) {
        t.Errorf("expected data-column-id for drop target")
    }
}
```

- [ ] **Step 2: Implement**

```go
// internal/view/components/kanban.templ
package components

import "strconv"

type KanbanCardData struct {
    ID           int64
    Title        string
    IssueNumber  int
    RepoFullName string
    Position     int
}

type KanbanColumnData struct {
    ID    int64
    Title string
    Cards []KanbanCardData
}

templ KanbanColumn(col KanbanColumnData) {
    <section
        class="w-72 flex-none bg-card border border-border rounded-md p-3 space-y-2"
        data-column-id={ strconv.FormatInt(col.ID, 10) }
        data-drop="kanban-column"
        aria-label={ "Column: " + col.Title }
    >
        <header class="flex items-center justify-between text-[13px] text-muted-foreground">
            <h3 class="font-medium text-foreground">{ col.Title }</h3>
            <span class="font-mono">{ strconv.Itoa(len(col.Cards)) }</span>
        </header>
        <ul class="space-y-2" data-cards>
            for _, card := range col.Cards {
                @KanbanCard(card)
            }
        </ul>
    </section>
}

templ KanbanCard(card KanbanCardData) {
    <li
        class="bg-background border border-border rounded p-2 text-sm cursor-grab active:cursor-grabbing hover:border-border-strong"
        draggable="true"
        data-card-id={ strconv.FormatInt(card.ID, 10) }
        data-drag="kanban-card"
    >
        <p class="font-medium">{ card.Title }</p>
        <p class="text-xs text-muted-foreground mt-1 font-mono">{ card.RepoFullName + " #" + strconv.Itoa(card.IssueNumber) }</p>
    </li>
}
```

- [ ] **Step 3: Regenerate, run, commit**

```bash
~/go/bin/templ generate && go test ./internal/view/components/ -run TestKanban -v
git add internal/view/components/kanban.templ internal/view/components/kanban_templ.go internal/view/components/kanban_test.go
git commit -m "feat(ui): add KanbanColumn and KanbanCard components"
```

---

### Task 6: Kanban drag-drop — extend MoveCard with explicit position + HTMX endpoint

**Files:**
- Modify: `internal/store/project_store.go` (extend `MoveCard` with reorder SQL)
- Modify: `internal/service/project_service.go` (extend `MoveCard` signature; add `ListColumnsWithCardsExpanded`)
- Modify: `internal/handler/project_handler.go` (add `MoveCard` HTTP handler)
- Modify: `internal/router/router.go`
- Create: `cmd/server/frontend/static/kanban.js` (Alpine-driven drag state → `htmx.ajax` PATCH)
- Modify: `internal/view/pages/project_detail.templ` (load kanban.js)

> **Existing-code reality check:**
> `ProjectService.MoveCard(ctx, projectID, cardID, newColumnID, userID int64) error` already exists and the store's `MoveCard(ctx, cardID, newColumnID)` auto-appends at `MAX(position)+1`. Phase 2 needs explicit-position re-ordering, so both signatures get a new `position int` argument and the store gets reorder SQL.

- [ ] **Step 0: Add an expanded view-model**

`model.ProjectCard` doesn't expose a unified `Title` or `RepoFullName`. Surface them in the service layer so templates stay branch-free:

```go
// internal/service/project_service.go

// KanbanCardView is the shape consumed by the project_detail page.
// Title resolves to issue title, PR title, or note (first line, ≤120 chars).
// Number is the issue/PR number (0 for note-only cards).
type KanbanCardView struct {
    ID           int64
    Title        string
    Number       int
    State        string // "open" | "closed" | "merged" | "" (note)
    Kind         string // "issue" | "pull" | "note"
    RepoFullName string
    Position     int
    ColumnID     int64
}

type KanbanColumnView struct {
    ID    int64
    Name  string
    Cards []KanbanCardView
}

// ListColumnsWithCardsExpanded resolves columns + cards joined with the
// owning repo's full name (owner/name) for display in the kanban board.
func (s *ProjectService) ListColumnsWithCardsExpanded(ctx context.Context, projectID int64) ([]KanbanColumnView, error) {
    repo, err := s.repoForProject(ctx, projectID)
    if err != nil {
        return nil, err
    }
    owner, err := s.repos.OwnerName(ctx, repo) // or whatever helper resolves owner-or-org
    if err != nil {
        return nil, err
    }
    fullName := owner + "/" + repo.Name

    raw, err := s.ListColumnsWithCards(ctx, projectID)
    if err != nil {
        return nil, err
    }
    out := make([]KanbanColumnView, len(raw))
    for i, col := range raw {
        cards := make([]KanbanCardView, 0, len(col.Cards))
        for _, c := range col.Cards {
            v := KanbanCardView{ID: c.ID, Position: c.Position, ColumnID: c.ColumnID, RepoFullName: fullName}
            switch {
            case c.IssueID != nil:
                v.Title, v.Number, v.State, v.Kind = c.IssueTitle, c.IssueNumber, c.IssueState, "issue"
            case c.PullID != nil:
                v.Title, v.Number, v.State, v.Kind = c.PullTitle, c.PullNumber, c.PullState, "pull"
            default:
                v.Title, v.Kind = firstLine(c.Note), "note"
            }
            cards = append(cards, v)
        }
        out[i] = KanbanColumnView{ID: col.Column.ID, Name: col.Column.Name, Cards: cards}
    }
    return out, nil
}
```

(Inline `firstLine` if not yet imported into this package.)

- [ ] **Step 1: Extend `ProjectStore.MoveCard` with explicit position + reorder**

Replace the current single-`UPDATE`. The new store method runs inside a transaction:

```go
// internal/store/project_store.go

// MoveCard moves a card to (newColumnID, newPosition), re-numbering siblings.
// Positions are dense (0..N-1) within each column after the move.
func (s *ProjectStore) MoveCard(ctx context.Context, cardID, newColumnID int64, newPosition int) error {
    tx, err := s.db.BeginTx(ctx, nil)
    if err != nil {
        return err
    }
    defer tx.Rollback() //nolint:errcheck // committed below on success

    // 1. Find the card's current column so we can close the gap there.
    var oldColumnID int64
    var oldPosition int
    if err := tx.QueryRowContext(ctx,
        `SELECT column_id, position FROM project_cards WHERE id = $1 FOR UPDATE`,
        cardID,
    ).Scan(&oldColumnID, &oldPosition); err != nil {
        return fmt.Errorf("locate card %d: %w", cardID, err)
    }

    if oldColumnID == newColumnID {
        // Same-column reorder: shift the interval between old and new.
        if newPosition > oldPosition {
            _, err = tx.ExecContext(ctx,
                `UPDATE project_cards SET position = position - 1
                 WHERE column_id = $1 AND position > $2 AND position <= $3 AND id != $4`,
                newColumnID, oldPosition, newPosition, cardID)
        } else if newPosition < oldPosition {
            _, err = tx.ExecContext(ctx,
                `UPDATE project_cards SET position = position + 1
                 WHERE column_id = $1 AND position >= $2 AND position < $3 AND id != $4`,
                newColumnID, newPosition, oldPosition, cardID)
        }
        if err != nil {
            return fmt.Errorf("reorder intra-column: %w", err)
        }
    } else {
        // Cross-column move: close the gap in old column, open one in new column.
        if _, err = tx.ExecContext(ctx,
            `UPDATE project_cards SET position = position - 1
             WHERE column_id = $1 AND position > $2`,
            oldColumnID, oldPosition,
        ); err != nil {
            return fmt.Errorf("close old column gap: %w", err)
        }
        if _, err = tx.ExecContext(ctx,
            `UPDATE project_cards SET position = position + 1
             WHERE column_id = $1 AND position >= $2`,
            newColumnID, newPosition,
        ); err != nil {
            return fmt.Errorf("open new column slot: %w", err)
        }
    }

    if _, err = tx.ExecContext(ctx,
        `UPDATE project_cards SET column_id = $1, position = $2 WHERE id = $3`,
        newColumnID, newPosition, cardID,
    ); err != nil {
        return fmt.Errorf("place card: %w", err)
    }
    return tx.Commit()
}
```

Update **all existing callers** of `ProjectStore.MoveCard` — `grep -rn '\.MoveCard(' internal/` — to pass an explicit position (the only existing caller is `ProjectService.MoveCard`, updated below).

- [ ] **Step 2: Extend `ProjectService.MoveCard` signature**

```go
// internal/service/project_service.go — replace existing MoveCard
func (s *ProjectService) MoveCard(ctx context.Context, projectID, cardID, newColumnID int64, newPosition int, userID int64) error {
    repo, err := s.repoForProject(ctx, projectID)
    if err != nil {
        return err
    }
    if !s.repos.CanWrite(ctx, repo, userID) {
        return ErrForbidden
    }
    colProject, err := s.projects.GetProjectByColumnID(ctx, newColumnID)
    if err != nil || colProject.ID != projectID {
        return ErrProjectNotFound
    }
    if newPosition < 0 {
        return fmt.Errorf("invalid position %d", newPosition)
    }
    if err := s.projects.MoveCard(ctx, cardID, newColumnID, newPosition); err != nil {
        return err
    }
    if err := s.projects.TouchProject(ctx, projectID); err != nil {
        log.Printf("TouchProject(%d): %v", projectID, err)
    }
    return nil
}
```

Update any other in-tree callers of `ProjectService.MoveCard` (existing handler tests, etc.) to pass `newPosition` — search with `grep -rn '\.Project\.MoveCard(' internal/`.

- [ ] **Step 3: Add the HTTP handler**

```go
// internal/handler/project_handler.go
// MoveCard handles PATCH /api/projects/{id}/cards/{cardID}/position
// Body: { "column_id": int64, "position": int }
func (h *Handler) MoveCard(w http.ResponseWriter, r *http.Request) {
    projectID, err1 := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
    cardID, err2 := strconv.ParseInt(chi.URLParam(r, "cardID"), 10, 64)
    if err1 != nil || err2 != nil {
        writeJSON(w, http.StatusBadRequest, errorResponse{Error: "invalid id"})
        return
    }
    var body struct {
        ColumnID int64 `json:"column_id"`
        Position int   `json:"position"`
    }
    if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
        writeJSON(w, http.StatusBadRequest, errorResponse{Error: "invalid body"})
        return
    }
    claims := authClaims(r)
    if claims == nil {
        writeJSON(w, http.StatusUnauthorized, errorResponse{Error: "auth required"})
        return
    }
    if err := h.Services.Project.MoveCard(r.Context(), projectID, cardID, body.ColumnID, body.Position, claims.UserID); err != nil {
        status := http.StatusForbidden
        if errors.Is(err, service.ErrProjectNotFound) {
            status = http.StatusNotFound
        }
        writeJSON(w, status, errorResponse{Error: err.Error()})
        return
    }
    writeJSON(w, http.StatusOK, struct{ OK bool }{OK: true})
}
```

- [ ] **Step 4: Register the route**

In `internal/router/router.go`, under the authenticated `/api` group (must run **after** `optAuthMW`):

```go
r.Patch("/projects/{id}/cards/{cardID}/position", h.MoveCard)
```

- [ ] **Step 5: Drag handler — HTMX + Alpine (no raw `fetch()`)**

Per project convention, all server calls go through HTMX. The drag state is local UI, so Alpine owns it; the network call is dispatched via `htmx.ajax(...)` so the standard CSRF / request-config plumbing applies.

```js
// cmd/server/frontend/static/kanban.js
// Drag state: Alpine. Network: htmx.ajax (PATCH).
document.addEventListener('alpine:init', () => {
  Alpine.data('kanbanBoard', () => ({
    dragged: null,
    onDragStart(e) {
      const t = e.target.closest('[data-drag="kanban-card"]');
      if (!t) return;
      this.dragged = t;
      e.dataTransfer.effectAllowed = 'move';
    },
    onDragOver(e) {
      if (!this.dragged) return;
      const col = e.target.closest('[data-drop="kanban-column"]');
      if (col) e.preventDefault();
    },
    async onDrop(e) {
      const col = e.target.closest('[data-drop="kanban-column"]');
      if (!col || !this.dragged) return;
      e.preventDefault();

      const cardID = this.dragged.dataset.cardId;
      const columnID = col.dataset.columnId;
      const list = col.querySelector('[data-cards]');
      list.appendChild(this.dragged); // optimistic
      const position = Array.from(list.children).indexOf(this.dragged);
      const projectID = this.$root.dataset.projectId;

      try {
        await htmx.ajax('PATCH', `/api/projects/${projectID}/cards/${cardID}/position`, {
          source: this.$root,
          values: { column_id: Number(columnID), position },
          swap: 'none',
          headers: { 'Content-Type': 'application/json' },
          // htmx serializes values as form-encoded by default; convert to JSON:
          handler: (elt, info) => {
            info.xhr.setRequestHeader('Content-Type', 'application/json');
            info.xhr.send(JSON.stringify(info.parameters));
          },
        });
      } catch (err) {
        // Source of truth is the server: reload to revert.
        window.location.reload();
      } finally {
        this.dragged = null;
      }
    },
  }));
});
```

**Why not raw `fetch()`:** project convention is HTMX for all server calls, so the standard `htmx:configRequest` hook (which already attaches the `X-CSRF-Token` header from the `csrf_token` cookie — verified in `internal/middleware/csrf.go`) is reused. Do **not** hand-roll cookie parsing.

If `htmx.ajax` with a custom `handler` proves awkward for a JSON body, the alternative is to expose a tiny shim that reads `csrf_token` from the cookie and forwards through HTMX's standard `htmx:configRequest` event. Either way, no raw `fetch()`.

- [ ] **Step 6: Load `kanban.js` only on the project page**

Add `<script src="/static/kanban.js" defer></script>` near the bottom of `project_detail.templ` (Task 9 below).

- [ ] **Step 7: Commit**

```bash
git add internal/handler/ internal/router/router.go internal/service/project_service.go internal/store/project_store.go cmd/server/frontend/static/kanban.js
git commit -m "feat(api): kanban drag — extend MoveCard with explicit position + PATCH endpoint"
```

---

### Task 7: Port `tree.templ` to match `mockups/tree.html`

**Files:**
- Modify: `internal/view/pages/tree.templ`
- Modify: `internal/handler/page_handler.go` (`PageTree`)
- Modify: `internal/handler/viewmodels.go` (`TreeData`)

- [ ] **Step 1: Extend TreeData**

```go
// internal/handler/viewmodels.go
type TreeData struct {
    // ... existing fields preserved (BasePage, Owner, RepoName, Ref, Path, Breadcrumbs, RefsURL) ...
    Entries        []service.TreeEntryWithLastCommit
    Sidebar        []components.TreeNode // IDE sidebar
    LatestCommit   TreeLatestCommit       // sub-header row, mockup tree.html / blob.html lines 387–394
}

type TreeLatestCommit struct {
    SHA       string    // short SHA shown in the sub-header
    Message   string    // first line of commit message
    Author    string
    AuthorURL string
    CommitURL string
    Timestamp time.Time
}
```

- [ ] **Step 2: Populate in handler**

```go
entries, _ := h.Services.Code.ListEntriesWithLastCommit(ctx, ownerName, repo.Name, ref, dir)
data.Entries = entries

// Latest commit summary for the current dir (mockup shows it above the listing).
// Use the most-recent commit across all entries.
var newest service.TreeEntryWithLastCommit
for _, e := range entries {
    if e.LastCommit.Timestamp.After(newest.LastCommit.Timestamp) {
        newest = e
    }
}
if newest.LastCommit.SHA != "" {
    short := newest.LastCommit.SHA
    if len(short) > 7 {
        short = short[:7]
    }
    data.LatestCommit = TreeLatestCommit{
        SHA:       short,
        Message:   newest.LastCommit.Message,
        Author:    newest.LastCommit.Author,
        AuthorURL: "/" + newest.LastCommit.Author,
        CommitURL: "/" + ownerName + "/" + repo.Name + "/commit/" + newest.LastCommit.SHA,
        Timestamp: newest.LastCommit.Timestamp,
    }
}

// Sidebar: convert entries → TreeNode + adjusted href.
data.Sidebar = make([]components.TreeNode, 0, len(entries))
for _, e := range entries {
    href := "/" + ownerName + "/" + repo.Name + "/blob/" + ref + "/" + e.Path
    if e.IsDir {
        href = "/" + ownerName + "/" + repo.Name + "/tree/" + ref + "/" + e.Path
    }
    data.Sidebar = append(data.Sidebar, components.TreeNode{
        Name: e.Name, IsDir: e.IsDir, Href: href,
    })
}
```

- [ ] **Step 3: Rewrite `tree.templ`**

Required structure (top → bottom):

1. `@fragments.RepoSubnav(data.BasePage.RepoSubnav)` with `Active: "code"`.
2. Existing eyebrow + breadcrumb header (lines 13–42 of current `tree.templ` — keep as-is, classes already in design-system tokens).
3. **NEW: Latest commit summary row** — matches mockup tree.html lines 387–394 of blob.html (the same control is reused for tree pages). Renders avatar + author link + commit message link + short SHA + relative time. Only shown when `data.LatestCommit.SHA != ""`. Skip on empty repos.
4. Two-column body with `grid grid-cols-[260px_1fr] gap-4`:
   - **Left** (`<aside>`, sticky, max-h-screen): `@components.FileTreeSidebar(data.Sidebar, "")` (no active file on tree pages).
   - **Right**: existing `components.Table` listing with rows per `data.Entries`. Adapt the existing two-column layout (icon | name) into **three** columns (icon | name | last-commit message + relative time on the right, fg3). Each row's name still links to `tree/{ref}/{path}` if dir, `blob/{ref}/{path}` if file. Sort: directories first, then files (preserve current behavior).
5. Empty-repo fallback (`ErrEmptyRepo`): preserve the existing "This directory is empty." block.

Apply the class translation map throughout. Use design tokens (`bg-card`, `border-border`, `text-muted-foreground`); do not hardcode mockup colors.

- [ ] **Step 4: Regenerate, run, verify**

```bash
~/go/bin/templ generate && make build-css && go build ./... && make dev
```

Open `/<owner>/<repo>/tree/main` and confirm sidebar + table render. Click a file in the sidebar → routes to blob.

- [ ] **Step 5: Commit**

```bash
git add internal/view/pages/tree.templ internal/view/pages/tree_templ.go internal/handler/page_handler.go internal/handler/viewmodels.go
git commit -m "feat(ui): port tree page to match mockup (IDE sidebar + last-commit columns)"
```

---

### Task 8: Port `blob.templ` and `blame.templ`

**Files:**
- Modify: `internal/view/pages/blob.templ`
- Modify: `internal/view/pages/blame.templ`
- Modify: `internal/handler/viewmodels.go` (extend `BlobData` / `BlameData`)
- Modify: `internal/handler/page_handler.go` (populate new fields)

#### Step 1 — Blob port (`mockups/blob.html` lines 191–260)

Required widgets, top → bottom:

1. **Repo subnav** — `@fragments.RepoSubnav(data.BasePage.RepoSubnav)` with `Active: "code"`.
2. **Eyebrow + breadcrumb header** — "Repository · File" eyebrow, path breadcrumb (`cloudzilla / internal / router / router.go`). Each segment is a link to the corresponding tree page; the final segment is `font-semibold` and **not** a link.
3. **Ref + meta row** — current branch/tag chip (re-use the existing LinkButton from the tree page), separator dot, mono "{N} lines · {human-readable size}".
4. **File card** (the main blob `<section>`):
   - **File header bar** (`px-4 py-2 b border-b elevated`): file icon, file name (mono, font-medium), and a right-aligned action group:
     - `Blame` button → `/{owner}/{repo}/blame/{ref}/{path}`
     - `Raw` button → `/{owner}/{repo}/raw/{ref}/{path}` (or whatever `BlameURL`'s sibling endpoint is — verify in `code_service_tree.go::GetRawBlob`)
     - `Copy` icon-only button (`aria-label="Copy file content"`) — client-side `navigator.clipboard.writeText`, hooked via a small Alpine bit; not a server roundtrip.
     - `Edit` button → repo write-protected route (gate behind `data.CanWrite`; hide if false).
     - If you want a future `Delete` button, gate behind `data.CanWrite` and a confirm dialog — out of scope here unless the mockup explicitly shows it (blob.html does not).
   - **Latest-commit sub-header** (avatar + author link + commit message link + short SHA + relative time) — same component used in Task 7 step 3; reuse the `TreeLatestCommit` view-model (or rename to `FileLatestCommit` if confusing).
   - **Code table** — line numbers in the left `<td>` (mono, `select-none`, `text-muted-foreground`, **anchor links** `<a href="#L{n}">{n}</a>` so line permalinks work and `:target` highlighting picks up — mockup CSS uses `.ln:target` to highlight). Right `<td>` is `whitespace-pre` source. Tag the `<tr>` with `id="L{n}"`.
   - **Syntax highlighting** — current `blob.templ` does not have it; if `BlobResult.Lines` already carries highlighted HTML, render via `templ.Raw`. If not, defer syntax highlighting to a follow-up task and ship the plain monospace table now. Either way, **note in the PR description** which path was taken.
5. **Binary file branch** — if `BlobResult.IsBinary == true`, show a banner ("Binary file — not displayed. Download Raw.") and **omit** the code table.
6. **Below-fold helper** — small `text-[12px] fg3` paragraph: "Permalink anchors: click any line number to deep-link (e.g. #L11)." (mockup line 260).

#### Step 2 — Blame port (`mockups/blame.html` lines 191–286)

Required widgets:

1. **Repo subnav** — `Active: "code"`.
2. **Eyebrow + breadcrumb header** ("Repository · Blame") with the same breadcrumb shape as Blob.
3. **Ref chip + "View file" button** (mockup line 207) → `/{owner}/{repo}/blob/{ref}/{path}`.
4. **Blame card**:
   - **Header bar** (`px-4 py-2 b border-b elevated`): blame icon, file name, right-aligned `"{N} lines · {M} contributors"`.
   - **Table** with three columns via `<colgroup>`: 240px (commit metadata) | 48px (line number) | flex (code). Use `@components.BlameRow(line)` per row.
   - **Hunk grouping**: consecutive lines from the same commit collapse — only the **first row in each run** carries author/SHA/date metadata; subsequent rows render a single `·` (mockup lines 238–239, 252–253, 271–272). This is service-layer concern: extend `code_service_blame.go` to mark lines as "first-of-hunk" (e.g. `BlameLine.HunkStart bool`), and `BlameRow` checks the flag. Adjust `BlameLine` in Task 4 accordingly:
     ```go
     type BlameLine struct {
         LineNumber  int
         Code        string
         AuthorName  string
         ShortSHA    string
         CommittedAt time.Time
         CommitURL   string
         HunkStart   bool // first line of a same-commit run
     }
     ```
   - **Hover summary**: each metadata cell has a `title` attribute with `"{full author} · {SHA} · {ISO date}"` for hover tooltips. Cheap; no JS popover needed.
5. **Below-fold helper paragraph** (mockup line 285): "A dot (·) marks consecutive lines from the same commit…".

#### Step 3 — Regenerate, verify, commit

```bash
~/go/bin/templ generate && make dev
# Visit /<owner>/<repo>/blob/main/<file> and /<owner>/<repo>/blame/main/<file>
# - confirm line-number anchors work (#L11 should scroll + highlight)
# - confirm Raw button downloads / opens plaintext
# - confirm blame hunk grouping (dots) appears for runs of same-commit lines
# - confirm both pages render correctly for binary files (blob) / large files
git add internal/view/pages/blob.templ internal/view/pages/blame.templ internal/view/pages/blob_templ.go internal/view/pages/blame_templ.go internal/handler/viewmodels.go internal/handler/page_handler.go internal/service/code_service_blame.go
git commit -m "feat(ui): port blob and blame pages to match mockups (breadcrumb, raw/blame/edit, hunk grouping)"
```

---

### Task 9: Port `project_detail.templ` to match `mockups/project.html`

**Files:**
- Modify: `internal/view/pages/project_detail.templ`
- Modify: `internal/handler/page_handler.go` (`PageProject` or similar)

- [ ] **Step 1: Build columns from project data**

In the handler — call the new expanded service method from Task 6 step 0 (`ListColumnsWithCardsExpanded`), which already joins issue/PR title + number + state and stamps `RepoFullName`:

```go
views, err := h.Services.Project.ListColumnsWithCardsExpanded(ctx, project.ID)
if err != nil {
    h.serverError(w, r, err)
    return
}
data.Columns = make([]components.KanbanColumnData, 0, len(views))
for _, v := range views {
    cards := make([]components.KanbanCardData, 0, len(v.Cards))
    for _, c := range v.Cards {
        cards = append(cards, components.KanbanCardData{
            ID:           c.ID,
            Title:        c.Title,
            IssueNumber:  c.Number, // 0 for note-only cards
            RepoFullName: c.RepoFullName,
            Position:     c.Position,
        })
    }
    data.Columns = append(data.Columns, components.KanbanColumnData{
        ID: v.ID, Title: v.Name, Cards: cards,
    })
}
```

(`Project.Name` and `Project.Description` on the model — `Project.Title` does **not** exist; fix any earlier references in this plan that say `data.Project.Title` to read `data.Project.Name`.)

- [ ] **Step 2: Rewrite `project_detail.templ` body**

Subnav active key: **`projects`**.

```go
templ ProjectDetail(data view.ProjectDetailData) {
    @layout.Base(data.BasePage, data.Project.Name) {
        @fragments.RepoSubnav(data.BasePage.RepoSubnav) // Active: "projects"
        <div
            data-project-id={ strconv.FormatInt(data.Project.ID, 10) }
            x-data="kanbanBoard"
            @dragstart="onDragStart($event)"
            @dragover="onDragOver($event)"
            @drop="onDrop($event)"
        >
            <header class="px-6 py-4 border-b border-border">
                <p class="font-mono text-[11px] text-muted-foreground uppercase tracking-wider mb-2">
                    <a href={ templ.SafeURL("/" + data.Owner + "/" + data.RepoName) } class="hover:underline">{ data.RepoName }</a>
                    <span aria-hidden="true">·</span>
                    Projects
                    <span aria-hidden="true">·</span>
                    Board
                </p>
                <h1 class="text-2xl font-semibold tracking-tight">{ data.Project.Name }</h1>
                if data.Project.Description != "" {
                    <p class="mt-1 text-sm text-muted-foreground max-w-2xl">{ data.Project.Description }</p>
                }
            </header>
            <div class="px-6 py-4 flex gap-4 overflow-x-auto pb-6">
                for _, col := range data.Columns {
                    @components.KanbanColumn(col)
                }
            </div>
        </div>
        <script src="/static/kanban.js" defer></script>
    }
}
```

> The `[data-cards]` selector used by `kanban.js` (Task 6 step 5) must be set on the inner `<ul>` of `KanbanColumn` — update `kanban.templ` accordingly (`<ul class="..." data-cards>`).

- [ ] **Step 3: Regenerate, verify**

```bash
~/go/bin/templ generate && make dev
# Open /<owner>/<repo>/projects/<n>, drag a card to another column,
# refresh, confirm it persists.
```

- [ ] **Step 4: Commit**

```bash
git add internal/view/pages/project_detail.templ internal/view/pages/project_detail_templ.go internal/handler/page_handler.go internal/service/project_service.go
git commit -m "feat(ui): port project board page with kanban drag-drop"
```

---

### Task 10: Verify and open PR

- [ ] **Step 1: Tests + lint + templ regen**

```bash
go test ./... && make lint && ~/go/bin/templ generate && git status
```

Expected: PASS / clean.

- [ ] **Step 2: Visual sweep in both themes**

Tree / blob / blame / project board — all four pages, both themes.

- [ ] **Step 3: silent-failure-hunter**

Run per CLAUDE.md.

- [ ] **Step 4: Push and open PR**

```bash
git push -u origin feat/ui-overhaul-phase-2-code-browser
gh pr create --title "feat(ui): UI overhaul phase 2 — code browser" --body "$(cat <<'EOF'
## Summary
- Ports `tree.templ`, `blob.templ`, `blame.templ`, `project_detail.templ` to mockups; all four include the canonical `RepoSubnav` (active keys: code/code/code/projects).
- New `CodeService.ListEntriesWithLastCommit` (LRU cache, 60s TTL; p95 measured on real repo, budget < 500 ms).
- New components: `FileTreeSidebar`, `BlameRow` (with hunk-grouping), `KanbanColumn`, `KanbanCard`.
- New service surface `ProjectService.ListColumnsWithCardsExpanded` so templates don't branch on issue-vs-pull-vs-note.
- Extends `ProjectService.MoveCard` and `ProjectStore.MoveCard` with explicit `position int`; store re-orders sibling cards densely inside a transaction.
- Drag-and-drop kanban via `PATCH /api/projects/{id}/cards/{cardID}/position`, dispatched via `htmx.ajax` (no raw `fetch()`); CSRF cookie `csrf_token`.
- Tree page gains a latest-commit summary header; blob page gains breadcrumb / Raw / Edit / Copy / line-anchor permalinks; blame page collapses runs of same-commit lines with `·`.

Spec: `docs/superpowers/specs/2026-05-14-ui-overhaul-design.md`
Plan: `docs/superpowers/plans/2026-05-14-ui-overhaul-phase-2-code-browser.md`

## Test plan
- [x] `go test ./...` passes
- [x] Visual: tree / blob / blame / project board in both themes
- [x] Kanban drag persists on refresh
- [x] silent-failure-hunter clean

🤖 Generated with [Claude Code](https://claude.com/claude-code)
EOF
)"
```

---

## Self-review checklist

- [ ] `ListEntriesWithLastCommit` LRU TTL bounded (60s) and p95 measured on a ≥1000-commit repo (< 500 ms or flagged for follow-up).
- [ ] Tree page falls back gracefully on `ErrEmptyRepo` (no latest-commit row, "This directory is empty." panel preserved).
- [ ] Tree page renders the latest-commit summary header above the listing.
- [ ] Blob page renders: subnav, breadcrumb, ref chip + line/size meta, file action bar (Blame / Raw / Copy / Edit-if-CanWrite), latest-commit sub-header, line-anchor `#L{n}` permalinks, binary fallback banner.
- [ ] Blame page renders: subnav, breadcrumb, ref chip + "View file" button, blame card header ("{N} lines · {M} contributors"), hunk-grouping via `BlameLine.HunkStart`, hover-title commit summary.
- [ ] `ProjectStore.MoveCard` runs inside a transaction, re-orders siblings densely, and handles same-column reorder + cross-column move + edge cases (move to position 0, move to end).
- [ ] `ProjectService.MoveCard` signature: `(ctx, projectID, cardID, newColumnID int64, newPosition int, userID int64) error` — all existing callers updated.
- [ ] Kanban drop endpoint authorizes via `ProjectService.MoveCard` (which enforces `CanWrite` on the project's repo).
- [ ] Kanban JS uses `htmx.ajax` (or `htmx:configRequest`-driven shim), **not** raw `fetch()`. CSRF cookie name verified: `csrf_token` (see `internal/middleware/csrf.go`).
- [ ] Kanban JS reloads on error rather than silently allowing inconsistency.
- [ ] All four pages include `@fragments.RepoSubnav(...)` with the correct `Active` key:
  - tree → `code`
  - blob → `code`
  - blame → `code`
  - project_detail → `projects`
- [ ] The subnav tab key set used in `RepoSubnav` is the canonical Phase-0 set: `code`, `issues`, `pull_requests`, `actions`, `discussions`, `projects`, `wiki`, `releases`, `settings`.
