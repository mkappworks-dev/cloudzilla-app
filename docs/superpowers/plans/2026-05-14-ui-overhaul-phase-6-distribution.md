# UI Overhaul · Phase 6 · Distribution & Docs — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Port `releases.templ`, `wiki_page.templ`, `topic.templ`, `repo_settings.templ`. Wire wiki TOC extraction from rendered markdown headings. Add `WikiTOC`, `SettingsSidebar`, and `WikiPageMeta` (sidebar list with titles). Replace inline markdown wrappers with the existing `@components.MarkdownBody`. The releases mockup does NOT show file assets — drop any `ReleaseAsset` work and render the Notes / Diff / Delete action bar instead.

**Architecture:** No migrations. Wiki TOC is computed server-side from the rendered HTML body using a small in-process extractor. `SettingsSidebar` is shared with Phase 10's account-settings page. The wiki page list is upgraded to return `[]WikiPageMeta{Slug, Title, UpdatedAt}` by reading each markdown's first heading.

**Prerequisites:** Phase 5 merged. Phase 0 subnav tab keys are: `code`, `issues`, `pull_requests`, `actions`, `discussions`, `projects`, `wiki`, `releases`, `settings`.

**Spec:** [2026-05-14-ui-overhaul-design.md](../specs/2026-05-14-ui-overhaul-design.md)

**Branch:** `feat/ui-overhaul-phase-6-distribution`

---

### Codebase facts verified before edits

- **Wiki has no `WikiService`.** Wiki lives on `CodeService`:
  - `CodeService.WikiPageList(owner, repoName) ([]string, error)` at `internal/service/code_service_wiki.go:18` — currently returns `[]string` slugs.
  - `CodeService.WikiPageGet(owner, repoName, slug) (content string, found bool, err error)` at `internal/service/code_service_wiki.go:52`.
  - No `Services.Wiki` field exists. Use `h.Services.Code.WikiPageList` / `h.Services.Code.WikiPageGet`.
- **Markdown rendering is `markdown.Render(s string) string`** in package `internal/markdown` (called via `markdown.Render(...)`). There is no `renderMarkdown(...)` helper.
- **`@components.MarkdownBody(html)`** already exists at `internal/view/components/markdown_body.templ` — wraps the rendered HTML in a styled prose container. Reuse it; do NOT inline `templ.Raw` again.
- **`internal/view/pages/topic.templ`** already exists and renders a topic header + repo list + pagination using design-system tokens (verified). Phase 6 only needs to confirm it still works with the new subnav model. The page is **not** repo-scoped, so it does NOT get a `RepoSubnav`.
- **`TopicService` exists** in the `Services` struct (`internal/service/services.go:46`) and `topic_handler.go` already calls `h.Services.Topic.ListReposByTopic`.
- **`internal/view/pages/repo_settings.templ`** currently renders these sections (verified): Collaborators, Transfer Ownership (conditional), Deploy Keys, Labels, Branch Protection, Webhooks, Danger Zone (Archive / Unarchive), Template Repository. There is no `General` form, no `Access` toggles, no `Notifications`. The sidebar must match what actually renders.
- **Release assets do NOT exist** in the codebase OR the mockup. `release_service.go` exposes Create, ListByRepo, GetByTag, GetByID, GetLatest, Update, Delete — no `ListAssets`. `mockups/releases.html:233-237` shows a Notes / Diff / Delete action bar, NOT file assets. The `ReleaseAsset` component is therefore deleted from this plan.
- **`withRepoSubnav` is now a `*Handler` method** (changed by the account-navigation feature). Call it as `h.withRepoSubnav(r.Context(), base, repo, active, canManage)` — not the old free-function form. It also populates `BasePage.RepoSwitcher` for the topbar repo-switcher dropdown. The release / wiki / repo-settings handlers this phase ports were already migrated to the method form; match it.
- **Repo-scoped breadcrumbs were removed** (account-navigation feature). `releases.templ`, `wiki_page.templ`, `repo_settings.templ` no longer render a `repoName · Section` breadcrumb above the page `<h1>` — the topbar + pill subnav cover that. Do NOT re-add one when porting these pages.

---

### Task 1: Branch setup

- [ ] `git checkout main && git pull && git checkout -b feat/ui-overhaul-phase-6-distribution`

---

### Task 2: Wiki TOC extractor — TDD

**Files:**
- Create: `internal/service/wiki_toc.go`
- Create: `internal/service/wiki_toc_test.go`

(There is no `wiki_service.go` to modify — wiki logic lives in `code_service_wiki.go`. Keep the TOC helper in its own file, package-level in `service`, so handlers can call `service.ExtractWikiTOC(html)`.)

- [ ] **Step 1: Failing test**

```go
// internal/service/wiki_toc_test.go
package service

import (
    "strings"
    "testing"
)

func TestExtractWikiTOC(t *testing.T) {
    html := `
<h1 id="intro">Intro</h1>
<p>...</p>
<h2 id="setup">Setup</h2>
<p>...</p>
<h2 id="usage">Usage</h2>
<h3 id="basic">Basic</h3>
<h3 id="advanced">Advanced</h3>
`
    toc := ExtractWikiTOC(html)
    if len(toc) != 5 {
        t.Fatalf("expected 5 entries, got %d", len(toc))
    }
    if toc[0].Level != 1 || toc[0].Anchor != "intro" {
        t.Errorf("first entry mismatch: %+v", toc[0])
    }
    if toc[3].Level != 3 || !strings.EqualFold(toc[3].Text, "Basic") {
        t.Errorf("nested entry mismatch: %+v", toc[3])
    }
}

func TestExtractWikiTOC_NoExplicitID(t *testing.T) {
    html := `<h2>Hello World</h2>`
    toc := ExtractWikiTOC(html)
    if len(toc) != 1 || toc[0].Anchor != "hello-world" {
        t.Fatalf("expected slugified anchor, got %+v", toc)
    }
}
```

- [ ] **Step 2: Implement**

```go
// internal/service/wiki_toc.go
package service

import (
    "regexp"
    "strings"
)

type WikiTOCEntry struct {
    Level  int
    Anchor string
    Text   string
}

var hRe = regexp.MustCompile(`(?is)<h([1-6])(?:\s+id="([^"]+)")?[^>]*>(.*?)</h[1-6]>`)
var tagRe = regexp.MustCompile(`<[^>]+>`)

func ExtractWikiTOC(html string) []WikiTOCEntry {
    matches := hRe.FindAllStringSubmatch(html, -1)
    out := make([]WikiTOCEntry, 0, len(matches))
    for _, m := range matches {
        level := int(m[1][0] - '0')
        anchor := m[2]
        text := tagRe.ReplaceAllString(m[3], "")
        text = strings.TrimSpace(text)
        if anchor == "" {
            anchor = slugify(text)
        }
        out = append(out, WikiTOCEntry{Level: level, Anchor: anchor, Text: text})
    }
    return out
}

func slugify(s string) string {
    s = strings.ToLower(s)
    var b strings.Builder
    for _, r := range s {
        switch {
        case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
            b.WriteRune(r)
        case r == ' ', r == '-':
            b.WriteRune('-')
        }
    }
    return strings.Trim(b.String(), "-")
}
```

- [ ] **Step 3: Run, verify, commit**

```bash
go test ./internal/service/ -run TestExtractWikiTOC -v
git add internal/service/wiki_toc.go internal/service/wiki_toc_test.go
git commit -m "feat(service): add wiki TOC extractor from rendered HTML"
```

---

### Task 3: Extend `WikiPageList` to return titled metadata

The mockup sidebar shows each page's display title (e.g. "Home", "Architecture", "Setup-on-mac"), not raw slugs. Pages have no separate "title" column — derive it from the first heading in each markdown file, falling back to the slug if there is no heading.

**Files:**
- Modify: `internal/service/code_service_wiki.go`
- Create / extend: `internal/service/code_service_wiki_test.go`
- Modify: `internal/view/viewmodels_social.go` (`WikiPageData.PageList` switches from `[]string` to `[]service.WikiPageMeta`)
- Modify: `internal/handler/wiki_handler.go`

- [ ] **Step 1: Add the metadata type and a new `WikiPageListMeta` method (additive, keeps the existing `WikiPageList` signature so other callers don't break).**

```go
// internal/service/code_service_wiki.go
type WikiPageMeta struct {
    Slug      string
    Title     string
    UpdatedAt time.Time
}

// WikiPageListMeta returns each wiki page's slug + first-heading title +
// commit author time of the most recent commit touching that file.
// Returns an empty slice when the wiki has no commits yet.
func (s *CodeService) WikiPageListMeta(owner, repoName string) ([]WikiPageMeta, error) { /* ... */ }
```

  Implementation outline:
  - Open the bare wiki repo (same as `WikiPageList`).
  - Walk the HEAD tree.
  - For each `*.md` entry, read first ~200 bytes of contents and pull the title from a leading `# ` line (use a small `firstHeading(content string) string` helper); fall back to the slug.
  - For `UpdatedAt`, use `commit.Author.When` from `repo.Head()` (cheap, good enough for sidebar). A per-file blame is overkill here.

- [ ] **Step 2: Failing test** `code_service_wiki_test.go` — seed a wiki repo with two files (`Home.md` starting with `# Hello`, `Architecture.md` starting with no heading) and assert titles are `"Hello"` and `"Architecture"` respectively.

- [ ] **Step 3: Implement, run, commit.**

```bash
go test ./internal/service/ -run TestWikiPageListMeta -v
git add internal/service/code_service_wiki.go internal/service/code_service_wiki_test.go
git commit -m "feat(service): add WikiPageListMeta with first-heading titles"
```

---

### Task 4: `WikiTOC` component

**Files:**
- Create: `internal/view/components/wiki_toc.templ`
- Create: `internal/view/components/wiki_toc_test.go`

```go
// internal/view/components/wiki_toc.templ
package components

import "github.com/mkappworks-dev/cloudzilla-app/internal/service"

templ WikiTOC(entries []service.WikiTOCEntry) {
    <nav aria-label="On this page" class="w-56 flex-none">
        <p class="text-xs font-medium text-muted-foreground uppercase tracking-wider mb-2">On this page</p>
        <ul class="space-y-1 text-sm">
            for _, e := range entries {
                <li style={ tocIndent(e.Level) }>
                    <a href={ templ.SafeURL("#" + e.Anchor) } class="text-muted-foreground hover:text-foreground block py-0.5">{ e.Text }</a>
                </li>
            }
        </ul>
    </nav>
}

func tocIndent(level int) string {
    switch level {
    case 1: return "padding-left: 0"
    case 2: return "padding-left: 0"
    case 3: return "padding-left: 12px"
    case 4: return "padding-left: 24px"
    default: return "padding-left: 32px"
    }
}
```

- [ ] Test, regenerate, commit:

```bash
~/go/bin/templ generate && go test ./internal/view/components/ -run TestWikiTOC -v
git add internal/view/components/wiki_toc.templ internal/view/components/wiki_toc_templ.go internal/view/components/wiki_toc_test.go
git commit -m "feat(ui): add WikiTOC component"
```

---

### Task 5: `SettingsSidebar` component

**Files:**
- Create: `internal/view/components/settings_sidebar.templ`
- Create: `internal/view/components/settings_sidebar_test.go`

```go
// internal/view/components/settings_sidebar.templ
package components

type SettingsSidebarItem struct {
    Label  string
    Href   string
    Key    string  // matches Active for highlighting
    Danger bool
}

type SettingsSidebarData struct {
    Items  []SettingsSidebarItem
    Active string
}

templ SettingsSidebar(d SettingsSidebarData) {
    <nav aria-label="Settings sections" class="w-56 flex-none">
        <ul class="space-y-1">
            for _, it := range d.Items {
                <li>
                    @settingsSidebarLink(it, d.Active)
                </li>
            }
        </ul>
    </nav>
}

templ settingsSidebarLink(it SettingsSidebarItem, active string) {
    if it.Key == active {
        if it.Danger {
            <a href={ templ.SafeURL(it.Href) } aria-current="page" class="block px-3 py-1.5 rounded text-sm font-medium bg-destructive/10 text-destructive">{ it.Label }</a>
        } else {
            <a href={ templ.SafeURL(it.Href) } aria-current="page" class="block px-3 py-1.5 rounded text-sm font-medium bg-accent text-foreground">{ it.Label }</a>
        }
    } else {
        if it.Danger {
            <a href={ templ.SafeURL(it.Href) } class="block px-3 py-1.5 rounded text-sm text-destructive hover:bg-destructive/10">{ it.Label }</a>
        } else {
            <a href={ templ.SafeURL(it.Href) } class="block px-3 py-1.5 rounded text-sm text-muted-foreground hover:text-foreground hover:bg-accent">{ it.Label }</a>
        }
    }
}
```

Test asserts that rendering with an active key highlights the matching item.

```bash
~/go/bin/templ generate && go test ./internal/view/components/ -run TestSettingsSidebar -v
git add internal/view/components/settings_sidebar.templ internal/view/components/settings_sidebar_templ.go internal/view/components/settings_sidebar_test.go
git commit -m "feat(ui): add SettingsSidebar component"
```

---

### Task 6: Port `releases.templ`

**Files:** Modify `internal/view/pages/releases.templ`, `internal/handler/release_handler.go`, and the releases viewmodel.

The mockup (`mockups/releases.html:215-275`) has TWO concerns:
- **Header + create form** (existing).
- **List of release "cards"** each with: tag chip, latest/pre-release/draft badge, name, rendered markdown body, and a **Notes / Diff / Delete** action row. There are no file assets.

- [ ] **Step 1: Extend viewmodel**

```go
// view package
type ReleasesData struct {
    BasePage  view.BasePage
    Owner     string
    Repo      *model.Repository
    Published []ReleaseView
    Drafts    []ReleaseView
    CanWrite  bool
}

type ReleaseView struct {
    model.Release
    BodyHTML  string  // pre-rendered via markdown.Render
    NotesURL  string  // tag / changelog page
    DiffURL   string  // compare URL between this tag and previous
    DeleteURL string  // POST/DELETE endpoint
}
```

- [ ] **Step 2: Populate in `release_handler.go`**

```go
all, _ := h.Services.Release.ListByRepo(ctx, owner, repoName)
for i, r := range all {
    rv := ReleaseView{
        Release:  r,
        BodyHTML: markdown.Render(r.Body),
        NotesURL: "/" + owner + "/" + repoName + "/releases/tag/" + r.TagName,
        DeleteURL: "/api/repos/" + owner + "/" + repoName + "/releases/" + strconv.FormatInt(r.ID, 10),
    }
    // DiffURL points at the previous tag in chronological order (or empty if r is the oldest).
    if i+1 < len(all) {
        rv.DiffURL = "/" + owner + "/" + repoName + "/compare/" + all[i+1].TagName + "..." + r.TagName
    }
    if r.IsDraft {
        data.Drafts = append(data.Drafts, rv)
    } else {
        data.Published = append(data.Published, rv)
    }
}
```

- [ ] **Step 3: Body**

Render two sections (Drafts above, Published below). Each release card:
- Tag chip (`<code class="...">{r.TagName}</code>`).
- Latest / Pre-release / Draft badge (use the existing `Badge` variants — `BadgeSuccess` for Latest, `BadgeWarning` for Pre-release, `BadgeSecondary` for Draft).
- Author and date.
- `@components.MarkdownBody(rv.BodyHTML)` for the body.
- Action row: `Notes`, `Diff` (omit if empty), `Delete` (only when `CanWrite`).
- Include `@components.RepoSubnav(..., Active: "releases")`.

- [ ] **Step 4: Commit**

```bash
~/go/bin/templ generate && make dev
git add internal/view/pages/releases.templ internal/view/pages/releases_templ.go internal/handler/release_handler.go internal/view/viewmodels_social.go
git commit -m "feat(ui): port releases page (drafts + published with Notes/Diff/Delete actions)"
```

---

### Task 7: Port `wiki_page.templ`

**Files:** Modify `internal/view/pages/wiki_page.templ`, `internal/handler/wiki_handler.go`, `internal/view/viewmodels_social.go`.

- [ ] **Step 1: Wiki page handler computes TOC and titled page list**

```go
// internal/handler/wiki_handler.go (PageWikiPage)
raw, exists, err := h.Services.Code.WikiPageGet(owner, repoName, slug)
// ... error handling
body := markdown.Render(raw)
toc := service.ExtractWikiTOC(body)
pageList, _ := h.Services.Code.WikiPageListMeta(owner, repoName)

h.render(w, r, pages.WikiPage(view.WikiPageData{
    /* existing fields */,
    ContentHTML: body,
    TOC:         toc,
    PageList:    pageList,
    Exists:      exists,
}))
```

  - `WikiPageData.PageList` field becomes `[]service.WikiPageMeta`.
  - Add `TOC []service.WikiTOCEntry`.

- [ ] **Step 2: Body — three-column layout**

  - **Left** (220px): page list (mockup styling — see `mockups/wiki.html:190-202`). Each item highlights when `slug == data.Slug`.
  - **Center**: page title, action buttons (Edit / Delete — Delete only when `CanManage`), then `@components.MarkdownBody(data.ContentHTML)`. Do NOT render a `cloudzilla · Wiki · {Title}` breadcrumb — the wiki breadcrumb was removed by the account-navigation feature; the topbar repo switcher + `wiki` pill subnav tab already convey location.
  - **Right** (224px): `@components.WikiTOC(data.TOC)` — sticky. Hide the right column when `len(data.TOC) == 0` so two-heading pages don't render an empty rail.
  - Include `@components.RepoSubnav(..., Active: "wiki")`.

- [ ] **Step 3: Commit**

```bash
~/go/bin/templ generate && make dev
git add internal/view/pages/wiki_page.templ internal/view/pages/wiki_page_templ.go internal/handler/wiki_handler.go internal/view/viewmodels_social.go
git commit -m "feat(ui): port wiki page with sidebar + on-this-page TOC"
```

---

### Task 8: Confirm / polish `topic.templ`

`internal/view/pages/topic.templ` was already ported in an earlier phase and uses design-system tokens. The page is **not** repo-scoped, so it does NOT include `RepoSubnav` — leave it as-is.

- [ ] **Step 1: Visual sweep only.** Verify in both light and dark themes against `mockups/topic.html`. Confirm `data.TopicName` chip, repo list, pagination, and empty state.
- [ ] **Step 2: No commit needed unless visual fixes are required.**

---

### Task 9: Port `repo_settings.templ` with `SettingsSidebar`

**Files:** Modify `internal/view/pages/repo_settings.templ`, `internal/handler/page_repo_handler.go` (or wherever `RepoSettings` data is populated).

The sidebar must match the sections that the existing page actually renders. From `internal/view/pages/repo_settings.templ` those are: Collaborators, Transfer (conditional), Deploy Keys, Labels, Branch Protection, Webhooks, Danger Zone (Archive), Template Repository.

- [ ] **Step 1: Compute settings nav items**

```go
items := []components.SettingsSidebarItem{
    {Key: "collaborators",      Label: "Collaborators",     Href: "#collaborators"},
    {Key: "deploy-keys",        Label: "Deploy keys",       Href: "#deploy-keys"},
    {Key: "labels",             Label: "Labels",            Href: "#labels"},
    {Key: "branch-protection",  Label: "Branch protection", Href: "#branch-protection"},
    {Key: "webhooks",           Label: "Webhooks",          Href: "#webhooks"},
    {Key: "template",           Label: "Template repository", Href: "#template"},
}
if data.IsOwner {
    items = append(items,
        components.SettingsSidebarItem{Key: "archive",  Label: "Archive",  Href: "#archive",  Danger: true},
    )
}
if data.CanTransfer {
    items = append(items,
        components.SettingsSidebarItem{Key: "transfer", Label: "Transfer", Href: "#transfer", Danger: true},
    )
}
data.SidebarItems = items
data.SidebarActive = "collaborators" // default; sticky sidebar shows the top section on load
```

  All sections live on a single page anchored by id. If routes are added later (split pages), only the `Href` values change.

- [ ] **Step 2: Body**

  - Two-column grid: left `@components.SettingsSidebar(...)`, right column with each section wrapped in `<section id="{Key}" ...>` that matches the sidebar's `Key`. Existing Card components stay; just add the matching `id` attribute.
  - Include `@components.RepoSubnav(..., Active: "settings")`.

- [ ] **Step 3: Commit**

```bash
~/go/bin/templ generate && make dev
git add internal/view/pages/repo_settings.templ internal/view/pages/repo_settings_templ.go internal/handler/page_repo_handler.go
git commit -m "feat(ui): port repo settings with shadcn sidebar nav"
```

---

### Task 10: Verify and open PR

Tests + lint + templ regen + visual sweep on releases / wiki / topic / settings in both themes. Run `pr-review-toolkit:silent-failure-hunter` on the branch. PR title: `feat(ui): UI overhaul phase 6 — distribution & docs`.

---

## Self-review checklist

- [ ] Wiki TOC extractor handles headings without explicit `id=` (slugifies).
- [ ] Releases page splits draft and published correctly and shows Notes / Diff / Delete actions (no file assets).
- [ ] Wiki sidebar shows page **titles** (from first heading) not raw slugs, and highlights the current page.
- [ ] Wiki right rail is hidden when `len(data.TOC) == 0`.
- [ ] All markdown rendering uses `@components.MarkdownBody(html)` (or follows the existing repo-readme pattern), not inline `templ.Raw`.
- [ ] Repo settings sidebar key set matches the section `id`s that the page actually renders.
- [ ] Subnav `Active` values: releases → `releases`, wiki → `wiki`, repo settings → `settings`; topic page has no `RepoSubnav`.
- [ ] No new top-level `WikiService` or `ReleaseAsset` types are introduced; they don't exist in the codebase.
