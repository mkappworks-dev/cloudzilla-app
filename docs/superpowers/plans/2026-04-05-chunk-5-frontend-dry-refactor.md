# Milestone 1 — Chunk 5: Frontend DRY Refactor ⬜ NOT STARTED

> **Scope:** Out of scope for `tech/m1-licensing-security-cleanup`. Implement on a separate branch (e.g., `tech/chunk-5-frontend-dry-refactor`).

**Goal:** Extract duplicated Templ patterns into shared components, deduplicate sidebar pickers, and clean up Tailwind configuration.

---

## Duplicated Patterns Found

| Pattern | Occurrences | Files Affected |
|---------|-------------|----------------|
| State badges (issue/PR) | 8 | issues, pulls, pull_detail, issue_detail, search, project_detail, fragments |
| Empty state cards | 12+ | issues, pulls, search, notifications, discussions, explore, feed, gists, etc. |
| Pagination controls | 5 | feed, commits, audit_log, code_search, gists |
| Tab bar navigation | 3 | search, explore, pulls |
| Sidebar label picker | 2 | issue_detail, pull_detail |
| Sidebar assignee picker | 2 | issue_detail, pull_detail |
| Sidebar milestone picker | 3 | issue_detail, pull_detail, milestone_sidebar fragment |
| Milestone progress bar | 6 | milestones, milestone_sidebar, issue_detail, pull_detail |
| Commit status icons | 2 | commit, pull_detail |
| Diff table rendering | 2 | commit, pull_detail |
| Markdown body card | 4 | issue_detail, pull_detail (pages + fragments) |
| White card container | 30+ | virtually every page |
| Section heading + action | 8+ | multiple pages |

**Tailwind finding:** Custom `forge-*` colors defined in `tailwind.config.js` but never used in any template.

---

## File Map

| File | Action |
|------|--------|
| `internal/view/components/card.templ` | Create |
| `internal/view/components/empty_state.templ` | Create |
| `internal/view/components/badge.templ` | Create |
| `internal/view/components/pagination.templ` | Create |
| `internal/view/components/tabs.templ` | Create |
| `internal/view/components/markdown_body.templ` | Create |
| `internal/view/components/progress_bar.templ` | Create |
| `internal/view/components/status_icon.templ` | Create |
| `internal/view/components/sidebar.templ` | Create |
| `internal/view/components/diff_table.templ` | Create |
| `tailwind/tailwind.config.js` | Edit (remove unused forge colors) |
| Multiple pages/*.templ | Edit (adopt components) |
| Multiple fragments/*.templ | Edit (adopt components) |

---

## Task 1: Create Card & Empty State Components

**Create `internal/view/components/card.templ`:**

```go
templ Card() {
    <div class="bg-white border border-gray-200 rounded-lg p-6">
        { children... }
    </div>
}

templ CardNoPadding() {
    <div class="bg-white border border-gray-200 rounded-lg">
        { children... }
    </div>
}
```

**Create `internal/view/components/empty_state.templ`:**

```go
templ EmptyState(message string) {
    <div class="bg-white border border-gray-200 rounded-lg p-8 text-center">
        <p class="text-gray-500">{ message }</p>
    </div>
}

templ EmptyStateWithHint(message, hint string) {
    <div class="bg-white border border-gray-200 rounded-lg p-8 text-center">
        <p class="text-gray-500">{ message }</p>
        <p class="text-sm text-gray-400 mt-2">{ hint }</p>
    </div>
}
```

Adopt in: issues, pulls, search, notifications, discussions, explore, feed, code_search, gists, commits (~12 files).

---

## Task 2: State Badge Components

**Create `internal/view/components/badge.templ`:**

```go
// IssueStateBadge renders open (green) or closed (red) badge
templ IssueStateBadge(state string)

// PullStateBadge renders open (green), merged (purple), or closed (red) badge
templ PullStateBadge(state string)

// GenericBadge renders a colored pill badge
templ GenericBadge(text, bgClass, textClass string)
```

Adopt in: 8 files (issues, pulls, pull_detail, issue_detail, search, project_detail, fragments).

---

## Task 3: Pagination Component

**Create `internal/view/components/pagination.templ`:**

```go
templ Pagination(baseURL string, currentPage int, hasNext bool, hasPrev bool)
templ PaginationWithCount(baseURL string, currentPage, totalPages int)
```

Unifies 5 different pagination implementations. Adopt in: feed, commits, audit_log, code_search, gists.

---

## Task 4: Tab Bar Component

**Create `internal/view/components/tabs.templ`:**

```go
type Tab struct {
    Label    string
    URL      string
    IsActive bool
    Count    int  // -1 means no count badge
}

templ TabBar(tabs []Tab)
```

Adopt in: search, explore, pulls.

---

## Task 5: Markdown Body Card

**Create `internal/view/components/markdown_body.templ`:**

```go
templ MarkdownBody(html string) {
    <div class="prose prose-sm max-w-none bg-gray-50 p-4 rounded border border-gray-200 my-4">
        @templ.Raw(html)
    </div>
}
```

Adopt in: 4 locations (issue_detail, pull_detail — pages + fragments).

---

## Task 6: Progress Bar Component

**Create `internal/view/components/progress_bar.templ`:**

```go
templ ProgressBar(closedCount, totalCount int)
templ ProgressBarSmall(closedCount, totalCount int)  // h-1 variant for sidebar
```

Adopt in: 6 locations across milestones pages, milestone_sidebar, issue_detail, pull_detail.

---

## Task 7: Commit Status Icon Component

**Create `internal/view/components/status_icon.templ`:**

```go
templ StatusIcon(state string)   // success=green✓, failure=red✗, error=red⚠, pending=yellow●
templ StatusRow(name, state, targetURL, description string)
```

Adopt in: commit.templ, pull_detail.templ.

---

## Task 8: Unified Sidebar Pickers

**Create `internal/view/components/sidebar.templ`:**

```go
templ LabelSidebar(owner, repoName string, itemNumber int, kind string, labels, allLabels []model.Label, canWrite bool)
templ AssigneeSidebar(owner, repoName string, itemNumber int, kind string, assignees []model.User, canWrite bool)
```

The `kind` parameter is `"issues"` or `"pulls"` — determines API URL path.

**Critical:** HTMX `hx-target` IDs must remain stable (`#issue-labels`, `#pull-labels`, `#issue-assignees`, `#pull-assignees`).

Adopt in: issue_detail.templ, pull_detail.templ.

---

## Task 9: Deduplicate Milestones List

`MilestonesList` in `fragments/milestone_sidebar.templ` and the milestones list in `pages/milestones.templ` are nearly identical.

Refactor `pages/milestones.templ` to use `@fragments.MilestonesList(...)` for the list portion. Keep only page header and "New milestone" form unique to the page.

---

## Task 10: Extract Diff Table Component

**Create `internal/view/components/diff_table.templ`:**

```go
templ DiffFileHeader(filename string, additions, deletions int)
templ DiffHunkTable(hunks []service.DiffHunk)  // simple read-only version
```

Pull_detail keeps its own table body for line comment interactivity (unique to PRs). Adopt `DiffFileHeader` and basic hunk rendering in commit.templ.

---

## Task 11: Clean Up Tailwind Configuration

**Edit `tailwind/tailwind.config.js`:**

Remove unused `forge-*` colors (`forge-green`, `forge-red`, `forge-purple`, `forge-blue`, `forge-dark`, `forge-border`, `forge-bg`) — none are referenced in any Templ file.

---

## Task 12: Evaluate pull_detail.templ Size

After extracting components in Tasks 2, 5, 7, 8, and 10, evaluate remaining size. If still over ~150 lines of Templ source, split merge controls into a helper or sibling file.

---

## Implementation Sequence

```
[1-2] Card, EmptyState, Badge ──────────┐
                                         ├─► [8-9] Sidebar pickers, milestone dedup
[3-6] Pagination, Tabs, Markdown, Bar ──┘
                                         ├─► [10] Diff table extraction
[7] Status icons ────────────────────────┘
                                         └─► [11-12] Tailwind cleanup, final evaluation
```

After each component creation:
1. Run `templ generate`
2. Run `go build ./...`
3. Spot-check affected pages in browser

---

## Risks

- **Circular imports:** `components` imports `model` for types. Verify `pages → components → model` has no cycles (it shouldn't — nothing imports components back into model).
- **HTMX swap targets:** Sidebar components must preserve exact `hx-target` IDs to avoid breaking HTMX swaps.
- **Generated files:** Never edit `_templ.go` — only run `templ generate`.
- **No frontend tests:** Verification is `go build` + visual browser checks.
