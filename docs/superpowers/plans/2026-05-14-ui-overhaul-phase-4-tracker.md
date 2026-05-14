# UI Overhaul · Phase 4 · Issues / PRs / Discussions / Actions — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Port `issues.templ`, `issue_detail.templ`, `issue_new.templ`, `pulls.templ`, `pull_new.templ`, `discussions.templ`. Add a placeholder `actions.templ` at `/{owner}/{repo}/actions`. Wire pinned-issue surfacing, CI status badge per PR row, reviewer suggestion, linked-PR lookup. New components: `BulkActionsBar`, `PRListRow`, `ReviewerPicker`.

**Architecture:** No migrations. Extends `PullService` with `ListWithCIStatus` and `SuggestReviewers`. `IssueService.ListPinned` and `DiscussionStore.List` (which already projects `IsAnswered`) are reused as-is — no extension needed.

**Verified-against-source assumptions (read before implementing):**

- `model.PullRequest` has `HeadBranch`, `BaseBranch`, `CreatedAt`, `AuthorName`. **There is no `HeadSHA` field** — resolve the head SHA via `CodeService.ResolveRef(owner, repoName, pr.HeadBranch)`.
- `CommitStatusService.GetCombined(ctx, owner, repoName, sha) (state, []CommitStatus, error)` is the real signature — there is no `CombinedFor(ctx, repoID, sha)`.
- `LabelStore.ListByPull(ctx, pullID)` and `LabelStore.ListByPullIDs(ctx, []int64)` already exist. `AssigneeStore.ListByPull(ctx, pullID)` already exists. `PullReviewStore.ListByPull(ctx, pullID)` already exists. **No new "ListForPull" methods are needed.**
- `IssueService.ListPinned(ctx, owner, repoName)` already exists at `internal/service/issue_service.go:168` — call it directly.
- `CodeService.GetIssueTemplates(owner, repoName, defaultBranch)` already exists at `internal/service/code_service_refs.go:85`. It reads from `.github/ISSUE_TEMPLATE/` (with fallback to `.github/ISSUE_TEMPLATE.md`), **not `.cloudzilla/ISSUE_TEMPLATE/`** — the spec earlier in this overhaul mis-stated the path.
- `CodeService.GetCodeOwners(owner, repoName, defaultBranch)` returns parsed `[]model.CodeOwnerRule`; `CodeService.MatchCodeOwners(rules, changedFiles)` returns the matched usernames. **`s.code.ReadFile(...)` does not exist** — use these two helpers.
- `DiscussionStore.List` already selects `is_answered` (see `internal/store/discussion_store.go:99`) — the store needs no further change; only handler/template work is required for the answered surface.
- `PullStore.List(ctx, repoID)` takes only `repoID` today and returns all PRs ordered by number desc. It does **not** accept a `state` filter. A new `ListByState(ctx, repoID, state, offset, limit)` (or matching variant) must be added in this phase to drive the state subnav.
- `internal/router/router.go` has **no `pageNames` slice** — there is no central page-name registry to update for a new page. Task 11 omits that step entirely.
- All page view-model types live in `internal/view/viewmodels.go` and are re-exported via `internal/handler/viewmodels.go`. New types (`ActionsData`, additional fields on `IssuesData`/`PullsData`/etc.) go in `internal/view/viewmodels.go`.

**Prerequisites:** Phase 3 merged.

**Spec:** [2026-05-14-ui-overhaul-design.md](../specs/2026-05-14-ui-overhaul-design.md)

**Branch:** `feat/ui-overhaul-phase-4-tracker`

---

### Task 1: Branch setup

- [ ] `git checkout main && git pull && git checkout -b feat/ui-overhaul-phase-4-tracker`

---

### Task 2: `PullService.ListWithCIStatus` — TDD

Replaces the existing list endpoint's result type to include the combined CI status badge per PR row.

**Files:**
- Modify: `internal/service/pull_service.go`
- Create: `internal/service/pull_service_ci_test.go`

- [ ] **Step 0: Add `PullStore.ListByState`**

`PullStore.List(ctx, repoID)` currently has no state/pagination args (see `internal/store/pull_store.go:61`). Add a new sibling method that the service can call:

```go
// internal/store/pull_store.go
func (s *PullStore) ListByState(ctx context.Context, repoID int64, state model.PRState, offset, limit int) ([]model.PullRequest, error) {
    // Mirror existing SELECT in List() but add: WHERE repo_id = $1 AND ($2 = '' OR state = $2)
    // ORDER BY number DESC LIMIT $3 OFFSET $4
}
```

Empty `state` ("") returns all states (used by "All" tab). Implement scanPullRows reuse — do not duplicate the row scan.

- [ ] **Step 1: Test**

```go
func TestPullService_ListWithCIStatus(t *testing.T) {
    stores, svcs := testStores(t)
    user := seedUserSvc(t, stores, "alice")
    repo := seedRepoSvc(t, stores, user.ID, "demo")
    // seedPull seeds a real branch head; capture the SHA returned by the helper.
    pr, headSHA := seedPullWithBranch(t, stores, repo.ID, user.ID, "first")
    seedStatus(t, stores, repo.ID, headSHA, "ci/build", "success")
    seedStatus(t, stores, repo.ID, headSHA, "ci/test", "failure")

    rows, err := svcs.Pull.ListWithCIStatus(context.Background(), repo.OwnerName, repo.Name, model.PRStateOpen, 0, 50)
    if err != nil { t.Fatalf("ListWithCIStatus: %v", err) }
    if len(rows) != 1 || rows[0].CIStatus != "failure" {
        t.Errorf("expected combined CI status 'failure', got %+v", rows)
    }
    _ = pr
}
```

- [ ] **Step 2: Implement**

```go
// in pull_service.go

type PullListRow struct {
    model.PullRequest
    HeadSHA       string // resolved at list time; not a DB column
    CIStatus      string // "success", "failure", "pending", "error", ""
    Reviewers     []model.PullReview
    LabelChips    []model.Label
    AssigneeChips []model.User
}

// Signature takes owner/repoName because CommitStatusService and CodeService
// both key off (owner, repoName), and we need the resolved repo for both
// CI lookup and HeadBranch -> SHA resolution.
func (s *PullService) ListWithCIStatus(ctx context.Context, owner, repoName string, state model.PRState, offset, limit int) ([]PullListRow, error) {
    repo, err := s.repos.GetByOwnerAndName(ctx, owner, repoName)
    if err != nil {
        return nil, err
    }
    pulls, err := s.store.ListByState(ctx, repo.ID, state, offset, limit)
    if err != nil {
        return nil, err
    }
    out := make([]PullListRow, 0, len(pulls))
    for _, p := range pulls {
        row := PullListRow{PullRequest: p}

        // Resolve head SHA from the branch ref; if the branch is gone, skip CI.
        if sha, _, err := s.code.ResolveRef(owner, repoName, p.HeadBranch); err == nil {
            row.HeadSHA = sha
            if combined, _, err := s.commitStatus.GetCombined(ctx, owner, repoName, sha); err == nil {
                row.CIStatus = string(combined)
            }
        }

        if rev, err := s.reviewStore.ListByPull(ctx, p.ID); err == nil {
            row.Reviewers = rev
        }
        if labs, err := s.labelStore.ListByPull(ctx, p.ID); err == nil {
            row.LabelChips = labs
        }
        if asg, err := s.assigneeStore.ListByPull(ctx, p.ID); err == nil {
            row.AssigneeChips = asg
        }
        out = append(out, row)
    }
    return out, nil
}
```

Notes:
- `ResolveRef` returns `(sha string, refType string, err error)` — see `code_service_refs.go`. Use the SHA only when err is nil.
- `GetCombined` returns `(state, []CommitStatus, error)`; we keep only `state` here.
- Label/assignee/reviewer stores already expose `ListByPull` — **do not** create `ListForPull` variants.
- For the per-row label fetch, prefer the batch helper `LabelStore.ListByPullIDs` if the row count is large; the simple loop above is fine for typical page sizes.

- [ ] **Step 3: Verify PASS, commit**

```bash
go test ./internal/service/ -run TestPullService_ListWithCIStatus -v
git add internal/service/pull_service.go internal/service/pull_service_ci_test.go internal/store/
git commit -m "feat(service): add PullService.ListWithCIStatus for richer PR rows"
```

---

### Task 3: `PullService.SuggestReviewers` — TDD

Suggests reviewers based on (a) CODEOWNERS file at the head ref, falling back to (b) the top 5 contributors who last-touched the files in the diff.

**Files:**
- Modify: `internal/service/pull_service.go`
- Create: `internal/service/pull_service_reviewers_test.go`

**Signature note:** Suggesting reviewers from a PR ID is awkward because the PR may not exist yet (the new-PR page asks for suggestions while the user is still composing). Take `(owner, repoName, base, head, limit)` instead — `CodeService` already exposes everything needed to compute the changed-files diff between two refs.

- [ ] **Step 1: Test**

```go
func TestSuggestReviewers_PrefersCodeOwners(t *testing.T) {
    stores, svcs := testStores(t)
    user := seedUserSvc(t, stores, "alice")
    repo := seedRepoSvc(t, stores, user.ID, "demo")
    bob := seedUserSvc(t, stores, "bob")
    // Seed a CODEOWNERS file on main that gives bob ownership over *.go
    seedFileOnBranch(t, repo, "main", "CODEOWNERS", "*.go @bob\n")
    seedFileOnBranch(t, repo, "feature", "main.go", "package main")

    out, err := svcs.Pull.SuggestReviewers(context.Background(), repo.OwnerName, repo.Name, "main", "feature", 3)
    if err != nil { t.Fatalf("SuggestReviewers: %v", err) }
    var found bool
    for _, u := range out {
        if u.Username == "bob" { found = true }
    }
    if !found {
        t.Errorf("expected bob from CODEOWNERS, got %+v", out)
    }
    _ = bob
}
```

- [ ] **Step 2: Implement**

```go
// CODEOWNERS-first, then top recent contributors as fallback.
func (s *PullService) SuggestReviewers(ctx context.Context, owner, repoName, base, head string, limit int) ([]model.User, error) {
    repo, err := s.repos.GetByOwnerAndName(ctx, owner, repoName)
    if err != nil {
        return nil, err
    }

    // 1) CODEOWNERS at default branch — parsed by CodeService.
    rules, _ := s.code.GetCodeOwners(owner, repoName, repo.DefaultBranch)
    if len(rules) > 0 {
        changed, _ := s.code.ChangedFilesBetween(owner, repoName, base, head) // implement using CompareCommits/Diff helpers already on CodeService
        owners := s.code.MatchCodeOwners(rules, changed)
        if len(owners) > 0 {
            if users, err := s.users.GetManyByUsernames(ctx, owners); err == nil && len(users) > 0 {
                if len(users) > limit {
                    users = users[:limit]
                }
                return users, nil
            }
        }
    }

    // 2) Top contributors by recent commit count (existing contributor-stats store).
    rows, err := s.contribStats.ListForRepo(ctx, repo.ID)
    if err != nil || len(rows) == 0 {
        return nil, nil
    }
    counts := map[int64]int{}
    for _, r := range rows {
        counts[r.UserID] += r.Commits
    }
    type pair struct{ id int64; n int }
    pairs := make([]pair, 0, len(counts))
    for id, n := range counts {
        pairs = append(pairs, pair{id, n})
    }
    sort.Slice(pairs, func(i, j int) bool { return pairs[i].n > pairs[j].n })
    if len(pairs) > limit {
        pairs = pairs[:limit]
    }
    ids := make([]int64, len(pairs))
    for i, p := range pairs {
        ids[i] = p.id
    }
    return s.users.GetMany(ctx, ids)
}
```

Notes:
- **Do not invent `s.code.ReadFile`** — it does not exist. Use `CodeService.GetCodeOwners` (which already locates `CODEOWNERS` or `.github/CODEOWNERS`, parses it, and returns `[]model.CodeOwnerRule`) and `CodeService.MatchCodeOwners(rules, changedFiles)` for matching.
- `ChangedFilesBetween` is the name used here for the helper that returns the file paths in the diff between two refs; reuse whatever the existing PR diff path uses (e.g. the same call site that powers the PR Files tab) rather than adding a new git tree walk.
- The contributor-stats store/service must already exist (it backs the contributors page from Phase 10.1). If the method name differs from `ListForRepo`, use the actual name verbatim.

- [ ] **Step 3: Verify PASS, commit**

```bash
go test ./internal/service/ -run TestSuggestReviewers -v
git add internal/service/pull_service.go internal/service/pull_service_reviewers_test.go internal/store/
git commit -m "feat(service): add SuggestReviewers via CODEOWNERS + top contributors"
```

---

### Task 4: New components

**Files:**
- Create: `internal/view/components/bulk_actions_bar.templ`
- Create: `internal/view/components/pr_list_row.templ`
- Create: `internal/view/components/reviewer_picker.templ`
- Test files for each.

- [ ] **Step 1: BulkActionsBar**

```go
// internal/view/components/bulk_actions_bar.templ
package components

type BulkActionsBarData struct {
    SelectedCount int
    Actions       []BulkAction
}

type BulkAction struct {
    Label string
    Href  string
    Kind  string // "primary", "destructive", or default
}

templ BulkActionsBar(d BulkActionsBarData) {
    if d.SelectedCount > 0 {
        <div role="region" aria-label="Bulk actions" class="flex items-center gap-2 px-3 py-2 bg-accent rounded-md text-sm">
            <span class="font-medium text-foreground">{ strconv.Itoa(d.SelectedCount) + " selected" }</span>
            for _, a := range d.Actions {
                <a href={ templ.SafeURL(a.Href) } class={ bulkActionClass(a.Kind) }>{ a.Label }</a>
            }
        </div>
    }
}

func bulkActionClass(kind string) string {
    switch kind {
    case "destructive":
        return "ml-auto h-7 px-2.5 text-sm text-destructive hover:bg-destructive/10 rounded"
    case "primary":
        return "h-7 px-2.5 text-sm bg-primary text-primary-foreground rounded hover:bg-primary/90"
    default:
        return "h-7 px-2.5 text-sm text-muted-foreground hover:text-foreground rounded"
    }
}
```

- [ ] **Step 2: PRListRow**

```go
// internal/view/components/pr_list_row.templ
package components

import "fmt"

type PRListRowData struct {
    OwnerName, RepoName string
    Number              int
    Title               string
    Author              string
    State               string // "open", "draft", "merged", "closed"
    CIStatus            string
    LabelChips          []LabelChip
    ReviewerAvatars     []string // initials
    OpenedAt            string   // pre-formatted relative
}

type LabelChip struct {
    Name  string
    Color string // hex
}

templ PRListRow(d PRListRowData) {
    <li class="border-b border-border last:border-0">
        <a href={ templ.SafeURL(fmt.Sprintf("/%s/%s/pulls/%d", d.OwnerName, d.RepoName, d.Number)) }
           class="flex items-center gap-3 px-4 py-3 hover:bg-accent">
            @prStateIcon(d.State)
            <div class="flex-1 min-w-0">
                <p class="font-medium text-sm truncate">{ d.Title }</p>
                <p class="text-xs text-muted-foreground mt-0.5">
                    { fmt.Sprintf("#%d opened %s by %s", d.Number, d.OpenedAt, d.Author) }
                </p>
                if len(d.LabelChips) > 0 {
                    <ul class="flex flex-wrap gap-1 mt-1">
                        for _, lab := range d.LabelChips {
                            <li class="text-[10px] px-1.5 py-0.5 rounded font-medium" style={ "background-color: " + lab.Color + "26; color: " + lab.Color }>{ lab.Name }</li>
                        }
                    </ul>
                }
            </div>
            @prCIBadge(d.CIStatus)
            <div class="flex -space-x-1.5">
                for _, r := range d.ReviewerAvatars {
                    <span class="h-5 w-5 rounded-full bg-muted border border-background grid place-items-center text-[10px] font-medium">{ r }</span>
                }
            </div>
        </a>
    </li>
}

templ prStateIcon(state string) {
    switch state {
    case "merged":
        <svg width="14" height="14" viewBox="0 0 24 24" class="text-primary flex-none" aria-label="merged" fill="currentColor"><circle cx="12" cy="12" r="3"/><path d="M14 5l5 5-5 5" fill="none" stroke="currentColor" stroke-width="2"/></svg>
    case "draft":
        <svg width="14" height="14" viewBox="0 0 24 24" class="text-muted-foreground flex-none" aria-label="draft" fill="none" stroke="currentColor" stroke-width="2"><circle cx="12" cy="12" r="3"/></svg>
    case "closed":
        <svg width="14" height="14" viewBox="0 0 24 24" class="text-destructive flex-none" aria-label="closed" fill="none" stroke="currentColor" stroke-width="2"><path d="M6 6l12 12M18 6L6 18"/></svg>
    default:
        <svg width="14" height="14" viewBox="0 0 24 24" class="text-success flex-none" aria-label="open" fill="none" stroke="currentColor" stroke-width="2"><circle cx="12" cy="12" r="3"/><path d="M12 9V3"/></svg>
    }
}

templ prCIBadge(state string) {
    switch state {
    case "success":
        <span class="text-success" title="CI passing" aria-label="CI passing">✓</span>
    case "failure", "error":
        <span class="text-destructive" title="CI failing" aria-label="CI failing">✗</span>
    case "pending":
        <span class="text-warning" title="CI pending" aria-label="CI pending">●</span>
    }
}
```

- [ ] **Step 3: ReviewerPicker**

```go
// internal/view/components/reviewer_picker.templ
package components

type ReviewerPickerData struct {
    Suggested []ReviewerOption
    All       []ReviewerOption
}

type ReviewerOption struct {
    Username string
    Reason   string // "CODEOWNERS" or "recent contributor"
    Selected bool
}

templ ReviewerPicker(d ReviewerPickerData) {
    <fieldset class="space-y-2">
        <legend class="text-sm font-medium">Reviewers</legend>
        if len(d.Suggested) > 0 {
            <p class="text-xs text-muted-foreground">Suggested</p>
            <ul class="space-y-1">
                for _, opt := range d.Suggested {
                    @reviewerOption(opt)
                }
            </ul>
        }
        <details class="mt-2">
            <summary class="text-xs text-muted-foreground cursor-pointer hover:text-foreground">All users</summary>
            <ul class="space-y-1 mt-1 max-h-48 overflow-y-auto">
                for _, opt := range d.All {
                    @reviewerOption(opt)
                }
            </ul>
        </details>
    </fieldset>
}

templ reviewerOption(opt ReviewerOption) {
    <li>
        <label class="flex items-center gap-2 px-2 py-1 hover:bg-accent rounded text-sm cursor-pointer">
            if opt.Selected {
                <input type="checkbox" name="reviewers" value={ opt.Username } checked class="h-4 w-4 rounded border-border"/>
            } else {
                <input type="checkbox" name="reviewers" value={ opt.Username } class="h-4 w-4 rounded border-border"/>
            }
            <span class="flex-1">{ opt.Username }</span>
            if opt.Reason != "" {
                <span class="text-[10px] uppercase tracking-wider text-muted-foreground/70">{ opt.Reason }</span>
            }
        </label>
    </li>
}
```

- [ ] **Step 4: Tests, regenerate, commit**

```bash
~/go/bin/templ generate
go test ./internal/view/components/ -run 'TestBulkActionsBar|TestPRListRow|TestReviewerPicker' -v
git add internal/view/components/
git commit -m "feat(ui): add BulkActionsBar, PRListRow, ReviewerPicker components"
```

---

### Task 5: Port `issues.templ`

**Files:** Modify `internal/view/pages/issues.templ`, page handler, viewmodel.

- [ ] **Step 1: Extend viewmodel with pinned + filter state**

Edit `internal/view/viewmodels.go` (canonical home of all page data types):

```go
type IssuesData struct {
    BasePage     view.BasePage
    OwnerName    string
    Repo         *model.Repository
    PinnedIssues []model.Issue
    Issues       []model.Issue

    // Filter state (read back from query params; used to render selected chip state)
    StateFilter     string // "open" (default) or "closed"
    SearchQuery     string // free-text from the search input
    LabelFilter     string // label name (single-select for now)
    MilestoneFilter string // milestone title or ""
    AssigneeFilter  string // username or ""
    Sort            string // "newest" (default), "oldest", "most-commented", "recently-updated"

    // Pre-fetched options for the filter dropdowns
    Labels     []model.Label
    Milestones []model.Milestone
    Assignees  []model.User

    // Tab counts shown in the Open/Closed subnav strip
    OpenCount   int
    ClosedCount int
}
```

- [ ] **Step 2: Populate from the handler**

```go
data.PinnedIssues, _ = h.Services.Issue.ListPinned(ctx, owner, name) // already exists at issue_service.go:168
data.Issues, _ = h.Services.Issue.ListForRepo(ctx, repo.ID, stateFilter, labelFilter, offset, limit)
data.Labels, _ = h.Services.Label.ListByRepo(ctx, repo.ID)
data.Milestones, _ = h.Services.Milestone.ListByRepo(ctx, repo.ID)
data.Assignees, _ = h.Services.Repo.ListCollaborators(ctx, repo.ID)
data.OpenCount, _ = h.Services.Issue.CountByState(ctx, repo.ID, "open")
data.ClosedCount, _ = h.Services.Issue.CountByState(ctx, repo.ID, "closed")
```

Use the existing service method names; if a count helper does not exist yet, call `len(issues)` on the relevant filtered list rather than adding a new method in this phase.

- [ ] **Step 3: Rewrite body (no bulk actions in Phase 4)**

Sections in order:

1. Page header (title + "New issue" button — already wired).
2. **Filter subnav** matching `mockups/issues.html` lines 188–200: Open/Closed tabs with mono count badges, then on the right side: a search input (`type="search"`, placeholder `Filter is:open`), a Labels dropdown button (count in mono), a Milestones dropdown button. **Add to the spec but not the mockup:** assignee dropdown and a sort selector (`newest`, `oldest`, `most-commented`, `recently-updated`). The filter row submits as a GET form so server-rendered state and bookmarkable URLs both work.
3. **Pinned section** (mockup lines 202–224): heading `Pinned <count>`, then a `grid-cols-1 sm:grid-cols-2 lg:grid-cols-3` card grid — one card per pinned issue. Render only if `len(data.PinnedIssues) > 0`.
4. **Issue list** — `<ol class="divide-y b">` with a state icon, title (with private lock indicator if applicable), `#number · opened by <user> · <relative time>`, and inline label chips.
5. Footer help line with filter-syntax hints (mockup line 284).

**Bulk actions are explicitly out of scope for Phase 4.** The `BulkActionsBar` component is still built in Task 4 (used by later phases) but is **not** placed on this page — there is no backing handler, route, or selection-state plumbing in this phase. Track as future work: "Phase 5+ — issue bulk operations (close / reopen / label / assign)."

- [ ] **Step 4: Regenerate, verify, commit**

```bash
~/go/bin/templ generate && make dev
# Visit /<owner>/<repo>/issues; pin an issue; confirm pinned section
git add internal/view/pages/issues.templ internal/view/pages/issues_templ.go internal/handler/ internal/service/issue_service.go internal/store/
git commit -m "feat(ui): port issues page with pinned section and bulk actions"
```

---

### Task 6: Port `issue_detail.templ` with linked-PRs

**Files:** Modify `internal/view/pages/issue_detail.templ`, handler, viewmodel.

- [ ] **Step 1: LinkedPRsLookup**

Add `IssueService.LinkedPRs(ctx, repoID, issueNumber)` — query `pull_requests` for any that mention `#<issue.Number>` in the title or body. The PR text column is `body` (see `internal/model/pull.go:20`), **not** `description`. Initial SQL:

```sql
SELECT id, repo_id, number, author_id, title, body, state, head_branch, base_branch,
       created_at, updated_at, merged_at, closed_at, is_draft, draft_at,
       auto_merge_enabled, auto_merge_strategy
  FROM pull_requests
 WHERE repo_id = $1
   AND (title ILIKE $2 OR body ILIKE $2)
 ORDER BY number DESC
```

Pass `'%#' || issueNumber || '%'` as `$2`. Refine later to recognise the GitHub `closes/fixes/resolves` keywords explicitly.

- [ ] **Step 2: Extend viewmodel**

```go
type IssueDetailData struct {
    // ...
    LinkedPRs []model.PullRequest
    Timeline  []TimelineItemView
}
```

- [ ] **Step 3: Port body**

Body uses `components.TimelineEntry` for comments/events. Sidebar lists labels, assignees, priority, milestone, linked PRs.

- [ ] **Step 4: Commit**

```bash
~/go/bin/templ generate && make dev
git add internal/view/pages/issue_detail.templ internal/view/pages/issue_detail_templ.go internal/handler/ internal/service/issue_service.go
git commit -m "feat(ui): port issue detail page with timeline + linked PRs"
```

---

### Task 7: Port `issue_new.templ` with template picker

**Files:** Modify `internal/view/pages/issue_new.templ`, handler.

- [ ] **Step 1: Template enumeration — reuse the existing service method**

`CodeService.GetIssueTemplates(owner, repoName, defaultBranch) ([]IssueTemplate, error)` already exists at `internal/service/code_service_refs.go:85`. It reads `.github/ISSUE_TEMPLATE/*.md` from the default branch with a fallback to `.github/ISSUE_TEMPLATE.md`. **Do not re-implement; do not switch the path to `.cloudzilla/`.** The returned `IssueTemplate` already has `Slug`, `Name`, and `Body`.

Handler call:

```go
templates, _ := h.Services.Code.GetIssueTemplates(owner, name, repo.DefaultBranch)
data.Templates = templates
```

- [ ] **Step 2: Extend viewmodel**

`IssueNewData` lives in `internal/view/viewmodels.go`. Add:

```go
type IssueNewData struct {
    BasePage  view.BasePage
    OwnerName string
    Repo      *model.Repository
    Templates []service.IssueTemplate // re-use the service-side type; do not redefine
}
```

If avoiding a `service` import in `internal/view` is desirable, define a thin view-side mirror type with the same three fields and convert in the handler.

- [ ] **Step 3: Body**

Above the title/body inputs: a row of template chips. Clicking a chip swaps the body via Alpine (sets `textarea.value`). Form submits to existing `POST /{owner}/{repo}/issues` endpoint.

- [ ] **Step 4: Commit**

```bash
~/go/bin/templ generate && make dev
git add internal/view/pages/issue_new.templ internal/view/pages/issue_new_templ.go internal/handler/ internal/service/
git commit -m "feat(ui): port new-issue page with template picker"
```

---

### Task 8: Port `pulls.templ` with PRListRow + CI status

**Files:** Modify `internal/view/pages/pulls.templ`, handler, viewmodel.

- [ ] **Step 1: Use the new ListWithCIStatus service**

```go
state := model.PRState(strings.ToLower(r.URL.Query().Get("state"))) // "" => all
data.Rows, _ = h.Services.Pull.ListWithCIStatus(ctx, owner, name, state, offset, limit)
```

The service takes `(owner, repoName)` — not `repoID` — because it needs to call `CodeService.ResolveRef` and `CommitStatusService.GetCombined`, both of which key off `(owner, repoName)`.

- [ ] **Step 2: Rewrite body using `components.PRListRow`**

State subnav at top (Open / Draft / Merged / Closed) — render each tab's count badge. Drafts are identified by `IsDraft == true && State == "open"`, so the "Open" tab should count `state=open AND is_draft=false` and the "Draft" tab `state=open AND is_draft=true`. Below the subnav: `<ul>` of `@components.PRListRow(...)` for each row.

- [ ] **Step 3: Commit**

```bash
~/go/bin/templ generate && make dev
git add internal/view/pages/pulls.templ internal/view/pages/pulls_templ.go internal/handler/
git commit -m "feat(ui): port pulls list page with CI status and reviewer chips"
```

---

### Task 9: Port `pull_new.templ` with ReviewerPicker

**Files:** Modify `internal/view/pages/pull_new.templ`, handler.

- [ ] **Step 1: Provide compare data + suggested reviewers**

`SuggestReviewers` takes branch names (not a PR ID — see Task 3). After the base/head branches are selected (either from query params on first GET or via HTMX update):

```go
base := firstNonEmpty(r.URL.Query().Get("base"), repo.DefaultBranch)
head := r.URL.Query().Get("head")

var suggested []model.User
if head != "" {
    suggested, _ = h.Services.Pull.SuggestReviewers(ctx, owner, name, base, head, 5)
}
allUsers, _ := h.Services.Repo.ListCollaborators(ctx, repo.ID)

data.Reviewer = components.ReviewerPickerData{
    Suggested: toReviewerOptions(suggested, "recent contributor"),
    All:       toReviewerOptions(allUsers, ""),
}
```

No draft PR is created up-front; suggestions are computed purely from refs + CODEOWNERS.

- [ ] **Step 2: Body**

Left column: base/head branch dropdowns + diff preview (HTMX swap on branch change). Right column: title input, description textarea, `@components.ReviewerPicker(data.Reviewer)`, labels selector. Submits to `POST /{owner}/{repo}/pulls`.

- [ ] **Step 3: Commit**

```bash
~/go/bin/templ generate && make dev
git add internal/view/pages/pull_new.templ internal/view/pages/pull_new_templ.go internal/handler/ internal/service/
git commit -m "feat(ui): port new-PR page with reviewer suggestions"
```

---

### Task 10: Port `discussions.templ`

**Files:** Modify `internal/view/pages/discussions.templ`.

- [ ] **Step 1: Surface answered state — already projected by the store**

`DiscussionStore.List` already selects `is_answered` directly (see `internal/store/discussion_store.go:99`). The `model.Discussion` struct exposes `IsAnswered` and `AnswerID`. **Do not extend the store query.** The handler/template work alone covers this.

If the page needs Open/Answered/Closed counts, add a small `DiscussionService.CountByState(ctx, repoID, state)` helper that counts:

- Open: `is_locked = false AND is_answered = false`
- Answered: `is_answered = true`
- Closed: `is_locked = true`

(adjust to whatever "closed" actually means in this codebase — verify against the discussions handler before fixing the criterion).

- [ ] **Step 2: Body — match `mockups/discussions.html`**

Layout in order:

1. **Page header** (title + "New discussion" button).
2. **Category chip row** (mockup lines 199–205): one chip per category — "All", then each category from `DiscussionCategoryStore.ListByRepo`. Each chip shows a count badge.
3. **State subnav** (mockup lines 207–211): `Open <count>` (default active), `Answered <count>`, `Closed <count>` — all three with mono count badges that come from the new `CountByState` helper.
4. **Pinned section** — if any discussions in the result set have `IsPinned` (or whatever the column is called), render them above the main list as their own group with a `Pinned` heading. Each pinned row uses the same row template but with a yellow `Pinned` badge to the right of the title (mockup lines 299–302).
5. **Main list** — each row: state icon, title, `#number · author · time · category badge · label chips`, participant avatar stack, `Answered` badge (mockup `.answered-badge` class — green check, lines 280–283) when `IsAnswered == true`, reply count.

- [ ] **Step 3: Commit**

```bash
~/go/bin/templ generate && make dev
git add internal/view/pages/discussions.templ internal/view/pages/discussions_templ.go internal/handler/ internal/service/discussion_service.go internal/store/
git commit -m "feat(ui): port discussions page with answered/pinned surfacing"
```

---

### Task 11: Create `actions.templ` placeholder

**Files:**
- Create: `internal/view/pages/actions.templ`
- Modify: `internal/router/router.go` (add the route)
- Modify: `internal/handler/page_handler.go` (add `PageActions`)
- Modify: `internal/view/viewmodels.go` (add `ActionsData`)
- Modify: `internal/handler/viewmodels.go` (add the type alias)

- [ ] **Step 1: Add the route**

```go
r.Get("/{owner}/{repo}/actions", h.PageActions)
```

- [ ] **Step 2: Handler**

```go
func (h *Handler) PageActions(w http.ResponseWriter, r *http.Request) {
    owner := chi.URLParam(r, "owner")
    name := chi.URLParam(r, "repo")
    repo, err := h.Services.Repo.GetByOwnerAndName(r.Context(), owner, name)
    if err != nil {
        http.NotFound(w, r)
        return
    }
    base := h.basePage(r)
    data := view.ActionsData{BasePage: base, Repo: repo, OwnerName: owner}
    h.render(w, r, pages.Actions(data), "Actions")
}
```

- [ ] **Step 3: Page template**

```go
// internal/view/pages/actions.templ
package pages

import (
    "github.com/mkappworks-dev/cloudzilla-app/internal/view"
    "github.com/mkappworks-dev/cloudzilla-app/internal/view/components"
    "github.com/mkappworks-dev/cloudzilla-app/internal/view/fragments"
    "github.com/mkappworks-dev/cloudzilla-app/internal/view/layout"
)

templ Actions(data view.ActionsData) {
    @layout.Base(data.BasePage, "Actions") {
        @fragments.RepoSubnav(fragments.RepoSubnavData{
            OwnerName: data.OwnerName, Repo: data.Repo, Active: "actions",
        })
        <div class="max-w-3xl mx-auto py-12">
            @components.EmptyState("Actions are coming soon.")
            <div class="mt-6 grid gap-2 opacity-40 pointer-events-none" aria-hidden="true">
                <div class="border border-border rounded-md p-4">
                    <p class="font-mono text-xs text-muted-foreground">build · main · 2m 14s</p>
                    <p class="text-sm mt-1">CI · Go test</p>
                </div>
                <div class="border border-border rounded-md p-4">
                    <p class="font-mono text-xs text-muted-foreground">lint · main · 41s</p>
                    <p class="text-sm mt-1">CI · golangci-lint</p>
                </div>
            </div>
        </div>
    }
}
```

- [ ] **Step 4: Wire ActionsData type**

In `internal/view/viewmodels.go` (the canonical home of all page view-models — see the import comment at the top of `internal/handler/viewmodels.go`):

```go
type ActionsData struct {
    BasePage  BasePage
    Repo      *model.Repository
    OwnerName string
}
```

Then add a re-export to `internal/handler/viewmodels.go`:

```go
ActionsData = view.ActionsData
```

- [ ] **Step 5: Regenerate, verify, commit**

(There is **no `pageNames` slice** in `internal/router/router.go` — routes are registered individually via `r.Get(...)`. The previous version of this plan named a phantom step; it has been removed.)

```bash
~/go/bin/templ generate && go build ./... && make dev
# Visit /<owner>/<repo>/actions, confirm friendly empty state
git add internal/view/pages/actions.templ internal/view/pages/actions_templ.go internal/handler/page_handler.go internal/router/router.go internal/view/viewmodels.go internal/handler/viewmodels.go
git commit -m "feat(ui): add Actions placeholder page and route"
```

---

### Task 12: Verify and open PR

Tests + lint + templ regen + visual sweep across the seven pages (issues / issue_detail / issue_new / pulls / pull_new / discussions / actions) in both themes. Run silent-failure-hunter. Push and open PR.

```bash
git push -u origin feat/ui-overhaul-phase-4-tracker
gh pr create --title "feat(ui): UI overhaul phase 4 — tracker pages" --body "$(cat <<'EOF'
## Summary
- Ports issues / issue_detail / issue_new / pulls / pull_new / discussions to match mockups.
- Adds Actions placeholder page at `/{owner}/{repo}/actions`.
- New components: BulkActionsBar, PRListRow, ReviewerPicker.
- `PullService.ListWithCIStatus` and `SuggestReviewers`.
- Issue template enumeration from `.cloudzilla/ISSUE_TEMPLATE/`.
- Linked-PR lookup on issue detail.

Spec: docs/superpowers/specs/2026-05-14-ui-overhaul-design.md
Plan: docs/superpowers/plans/2026-05-14-ui-overhaul-phase-4-tracker.md

## Test plan
- [x] `go test ./...` passes
- [x] Visual: seven pages in both themes
- [x] CODEOWNERS-based reviewer suggestion verified
- [x] CI badge renders per-PR row correctly

🤖 Generated with [Claude Code](https://claude.com/claude-code)
EOF
)"
```

---

## Subnav alignment (per-page `RepoSubnav` keys)

Phase 0 fixed the subnav tab keys to: `code`, `issues`, `pull_requests`, `actions`, `discussions`, `projects`, `wiki`, `releases`, `settings`. All seven pages touched in Phase 4 must call `@fragments.RepoSubnav(...)` with the correct `Active` value:

| Page                 | `Active` value     |
| -------------------- | ------------------ |
| `issues.templ`       | `"issues"`         |
| `issue_detail.templ` | `"issues"`         |
| `issue_new.templ`    | `"issues"`         |
| `pulls.templ`        | `"pull_requests"`  |
| `pull_new.templ`     | `"pull_requests"`  |
| `discussions.templ`  | `"discussions"`    |
| `actions.templ`      | `"actions"`        |

---

## Self-review checklist

- [ ] `ListWithCIStatus` resolves the head SHA via `CodeService.ResolveRef(owner, repoName, pr.HeadBranch)` — no `HeadSHA` column is referenced anywhere.
- [ ] `CommitStatusService.GetCombined(ctx, owner, repoName, sha)` (the real method) is the only commit-status entry point used — no calls to a non-existent `CombinedFor`.
- [ ] Label / assignee / reviewer fetches use the existing `ListByPull` methods; no `ListForPull` variants are introduced.
- [ ] `SuggestReviewers` uses `CodeService.GetCodeOwners` + `CodeService.MatchCodeOwners` — there is no call to a fictional `s.code.ReadFile`. Signature is `(ctx, owner, repoName, base, head, limit)`.
- [ ] `IssueService.ListPinned(ctx, owner, repoName)` is called directly (it already exists at `internal/service/issue_service.go:168`); no duplicate "add if missing" implementation.
- [ ] Issue template enumeration calls the existing `CodeService.GetIssueTemplates` which reads `.github/ISSUE_TEMPLATE/` — paths are **not** rewritten to `.cloudzilla/`.
- [ ] Linked-PRs SQL filters on `body` (the real column name), not `description`.
- [ ] Bulk-actions UI is absent from `issues.templ` (deferred to a future phase). `BulkActionsBar` is built as a reusable component but unused on Phase 4 pages.
- [ ] Issues page filter row contains: state tabs (Open/Closed), search input, label filter, milestone filter, assignee filter, sort selector.
- [ ] Discussions page contains: category chips, state subnav (Open/Answered/Closed) with mono count badges, pinned section, main list with `Answered` and `Pinned` badges where applicable.
- [ ] `PullStore.ListByState(ctx, repoID, state, offset, limit)` is added — the existing `List(ctx, repoID)` takes no state filter.
- [ ] No edits to a `pageNames` slice in `internal/router/router.go` (no such slice exists).
- [ ] `ActionsData` and any other new view-model types live in `internal/view/viewmodels.go` with a re-export alias in `internal/handler/viewmodels.go`.
- [ ] Actions page is gated by route + handler only (no fake data persistence anywhere).
- [ ] `PRListRow` correctly handles `state="draft"` (separate icon from open).
- [ ] All seven pages include `@fragments.RepoSubnav(...)` with the `Active` value listed in the table above.
