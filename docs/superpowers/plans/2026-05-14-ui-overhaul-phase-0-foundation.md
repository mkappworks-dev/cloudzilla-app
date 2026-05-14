# UI Overhaul · Phase 0 · Foundation — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Establish the class translation map, promote irreducible utilities to project CSS, sync the layout chrome with the mockups, extract repo and dashboard subnav fragments, and sweep orphan pages onto shadcn tokens. This phase unblocks Phases 1–10.

**Architecture:** All work lands in `tailwind/input.css`, `internal/view/layout/`, `internal/view/fragments/`, and existing orphan templ files. No new services, no migrations. The single deliverable that downstream phases depend on is the class translation map at `docs/ui-overhaul-class-map.md`.

**Tech Stack:** Templ, Tailwind CSS, Go (chi router), shadcn-style token system already in `tailwind/input.css`.

**Spec:** [2026-05-14-ui-overhaul-design.md](../specs/2026-05-14-ui-overhaul-design.md)

**Branch:** `feat/ui-overhaul-phase-0-foundation`

---

### Task 1: Set up the branch

**Files:** none yet.

- [ ] **Step 1: Create and switch to the phase branch**

```bash
git checkout main
git pull
git checkout -b feat/ui-overhaul-phase-0-foundation
```

- [ ] **Step 2: Verify clean tree**

```bash
git status
```

Expected: `nothing to commit, working tree clean`.

---

### Task 2: Write the class translation map

This document is referenced by every later phase. The map locks down how mockup utility classes map to shadcn token utilities so the per-page port is mechanical.

**Files:**
- Create: `docs/ui-overhaul-class-map.md`

- [ ] **Step 1: Write the file**

```markdown
# Mockup → shadcn class translation map

Source of truth for the UI overhaul (spec: [2026-05-14-ui-overhaul-design.md](superpowers/specs/2026-05-14-ui-overhaul-design.md)). Every per-phase page port translates mockup classes mechanically using this map. If a mockup uses a class not in this map, **halt and update the map first** — do not invent ad-hoc replacements during a port.

## Token reference (CSS variables)

| Mockup variable | shadcn variable in `tailwind/input.css` | Tailwind utility |
|---|---|---|
| `--bg` | `--background` | `bg-background` |
| `--surface` | `--card` | `bg-card` |
| `--elevated` | `--accent` | `bg-accent` |
| `--border` | `--border` | `border-border` |
| `--border-strong` | `--border-strong` (new — added in Task 3) | `border-border-strong` |
| `--fg` | `--foreground` | `text-foreground` |
| `--fg-2` | `--muted-foreground` | `text-muted-foreground` |
| `--fg-3` | `--muted-foreground` at 70% opacity | `text-muted-foreground/70` |
| `--focus` | `--ring` | `focus-visible:outline-ring` |
| `--link` | `--primary` | `text-primary` |
| `--success` | `--success` | `text-success`, `bg-success` |
| `--warn` | `--warning` | `text-warning` |
| `--danger` | `--destructive` | `text-destructive` |

## Utility classes

| Mockup class | shadcn replacement | Notes |
|---|---|---|
| `mono` | `font-mono` | |
| `fg2` | `text-muted-foreground` | |
| `fg3` | `text-muted-foreground/70` | |
| `b` (border container) | `border-border` | combine with `border` Tailwind utility |
| `bs` (stronger border) | `border-border-strong` | requires the `--border-strong` token (Task 3) |
| `surface` | `bg-card` | |
| `elevated` | `bg-accent` | |
| `hover-row` | `hover:bg-accent` | apply on table rows / list items |
| `link` | `text-primary` | |
| `tab-active` | `text-foreground border-foreground` | apply to active tab anchor |
| `prose` | `prose` | Long-form Markdown (README, PR description). Use the `@tailwindcss/typography` plugin if installed; otherwise add a minimal `.prose` ruleset to `tailwind/input.css` under `@layer components`. |

## Component-replaced classes

These mockup classes correspond to components in `internal/view/components/`. Use the component, do not copy the class.

| Mockup classes | Replace with component |
|---|---|
| `dropdown-wrap`, `dropdown-panel`, `dropdown-item`, `dropdown-divider`, `dropdown-header` | `components.DropdownMenu*` |
| `avatar` | `components.Avatar` |
| `badge`, `badge--open`, `badge--closed`, `badge--merged`, `badge--draft` | `components.Badge` (variant prop carries the state) |
| `skip-link` (link element only) | already in layout |

## Promoted utility classes (no shadcn equivalent)

These classes get added to `tailwind/input.css` as project-level utilities in Task 3. They are used **by their original names** in templ files.

- `grid-bg` — radial-masked grid background on home hero / auth pages
- `heatmap-cell-0`, `heatmap-cell-1`, `heatmap-cell-2`, `heatmap-cell-3`, `heatmap-cell-4` — commit heatmap intensity scale
- `dark-only`, `light-only` — already in layout (kept)

## Inline `<style>` blocks

**Inline `<style>` blocks in mockup HTML files do not survive the port.** Anything inside an inline `<style>` either:
1. Translates to Tailwind utilities (via this map), or
2. Gets promoted to `tailwind/input.css` (only `grid-bg` and the `heatmap-cell-*` scale qualify so far).

## Updating this map

When a per-phase port discovers a mockup class missing from this map:
1. Stop the port.
2. Decide which case it is:
   - Has a clean shadcn equivalent → add row to the utility classes table.
   - No shadcn equivalent but reusable → add to the promoted utilities section AND to `tailwind/input.css`.
   - One-off → translate inline using a comment in the templ file (`<!-- mockup: .xyz -->`).
3. Commit the map update separately from the port commit, with subject `docs(ui): extend class translation map for <reason>`.
```

- [ ] **Step 2: Commit**

```bash
git add docs/ui-overhaul-class-map.md
git commit -m "docs(ui): add class translation map for UI overhaul"
```

---

### Task 3: Promote irreducible utilities to `tailwind/input.css`

Adds `--border-strong`, `grid-bg`, and the `heatmap-cell-*` scale. These are referenced by their original names in all later phases.

**Files:**
- Modify: `tailwind/input.css`

- [ ] **Step 1: Read current input.css**

```bash
cat tailwind/input.css
```

Confirm `:root` and `.dark` blocks define `--background`, `--card`, `--accent`, `--border`, etc.

- [ ] **Step 2: Add `--border-strong` to both themes**

Inside the `:root` block (light theme), after the existing `--border: 240 5.9% 88%;` line, add:

```css
    --border-strong: 240 5.9% 70%;
```

Inside the `.dark` block, after the existing `--border` declaration, add:

```css
    --border-strong: 0 0% 100% / 0.18;
```

- [ ] **Step 3: Register `border-border-strong` in Tailwind config**

Read `tailwind/tailwind.config.js`. In `theme.extend.colors.border`, change the value from a string to an object:

```js
border: {
  DEFAULT: 'hsl(var(--border))',
  strong: 'hsl(var(--border-strong))',
},
```

This enables both `border-border` (existing) and `border-border-strong` (new) classes.

- [ ] **Step 4: Add the `grid-bg` utility under `@layer utilities`**

At the bottom of `tailwind/input.css`, add (or extend) `@layer utilities`:

```css
@layer utilities {
  .grid-bg {
    background-image:
      linear-gradient(hsl(var(--border)) 1px, transparent 1px),
      linear-gradient(90deg, hsl(var(--border)) 1px, transparent 1px);
    background-size: 56px 56px;
    mask-image: radial-gradient(ellipse at top, black 30%, transparent 70%);
  }
}
```

- [ ] **Step 5: Add the heatmap cell scale**

In the same `@layer utilities` block, append:

```css
@layer utilities {
  .heatmap-cell-0 { background-color: hsl(var(--muted)); }
  .heatmap-cell-1 { background-color: hsl(var(--success) / 0.25); }
  .heatmap-cell-2 { background-color: hsl(var(--success) / 0.50); }
  .heatmap-cell-3 { background-color: hsl(var(--success) / 0.75); }
  .heatmap-cell-4 { background-color: hsl(var(--success)); }
}
```

- [ ] **Step 6: Forced-colors fallback**

Append this `@media` block at the very end of `tailwind/input.css`:

```css
@media (forced-colors: active) {
  .grid-bg { display: none; }
  .heatmap-cell-0,
  .heatmap-cell-1,
  .heatmap-cell-2,
  .heatmap-cell-3,
  .heatmap-cell-4 {
    background-color: ButtonFace;
    border: 1px solid ButtonText;
  }
}
```

- [ ] **Step 7: Rebuild and verify**

```bash
make build-css
```

Expected: build succeeds without warnings. Compiled output appears at `cmd/server/frontend/static/main.css`.

- [ ] **Step 8: Visual smoke test**

Start dev server:

```bash
make dev
```

Open `http://localhost:8080` in a browser. Confirm the home page still renders (nothing is broken by the CSS additions). Stop the server.

- [ ] **Step 9: Commit**

```bash
git add tailwind/input.css tailwind/tailwind.config.js
git commit -m "feat(ui): promote grid-bg, heatmap-cell-*, and --border-strong to project CSS"
```

---

### Task 4: Sync the layout chrome with the mockup top-nav

Mockups show: logo → diagonal divider → workspace switcher trigger (avatar initial badge + username + chevron) → search → Explore → New repo → Notifications → Theme toggle → User menu. Current `layout.templ` is missing the workspace switcher dropdown. Wire it to `OrgService.ListOwnedByUser` (confirmed to exist at `internal/service/org_service.go:59`).

Also: the mockup footer (`mockups/repo.html` lines 568–578) shows `cloudzilla / v0.2.0 · all systems normal` on the left and a `Docs / API / Changelog / Status` nav on the right. The current layout footer (`internal/view/layout/layout.templ` lines 138–141) is a single copyright line — bring it in line with the mockup.

**Files:**
- Modify: `internal/view/layout/layout.templ` (workspace switcher + footer)
- Modify: `internal/handler/page_handler.go` (basePage to inject org list)
- Modify: `internal/view/viewmodels.go` (BasePage struct, add `UserOrgs`)

- [ ] **Step 1: Confirm BasePage location and shape**

```bash
grep -n "type BasePage" /Users/mk/Downloads/app/Cloudzilla/cloudzilla-app/internal/view/viewmodels.go
```

Expected: a single hit in `internal/view/viewmodels.go`. The struct already carries `CurrentUser *middleware.Claims` (Claims-based, not `*model.User`), `UnreadNotifCount int`, `AllowLogin bool`, and `AllowRegistration bool`. We will append `UserOrgs []model.Organization` to this struct.

- [ ] **Step 2: Add `UserOrgs` field to BasePage**

In `internal/view/viewmodels.go`, extend the existing `BasePage` struct (the `model` import is already present):

```go
// UserOrgs is the list of organizations the current user owns.
// Empty if no user is signed in. Used by the layout's workspace switcher.
// CurrentUser is a `*middleware.Claims` (not `*model.User`); use
// `CurrentUser.Username` and `CurrentUser.UserID` from claims when wiring this.
UserOrgs []model.Organization
```

- [ ] **Step 3: Locate the basePage helper that constructs BasePage**

```bash
grep -rn "func.*basePage\|func.*BasePage" /Users/mk/Downloads/app/Cloudzilla/cloudzilla-app/internal/handler/
```

The helper is in `internal/handler/page_handler.go` (or similar). Find the function that builds `BasePage` for every request.

- [ ] **Step 4: Wire UserOrgs in basePage**

Inside the basePage helper, after the current user (claims) is loaded, add:

```go
if claims != nil {
    orgs, err := h.Services.Org.ListOwnedByUser(ctx, claims.UserID)
    if err != nil {
        // Log but don't fail the page render — workspace switcher
        // just shows "Personal" only.
        h.log.Warn("listing user orgs for workspace switcher failed", "err", err, "user_id", claims.UserID)
    } else {
        page.UserOrgs = orgs
    }
}
```

Use whatever logger pattern the file already uses; match the existing style. Use the claim field name actually present on `*middleware.Claims` (typically `UserID`) — if it differs, follow the existing usages in the same file.

- [ ] **Step 5: Add the workspace switcher dropdown to layout.templ**

In `internal/view/layout/layout.templ`, find the existing block that renders the logo followed by the slash divider and username chip (currently around lines 38–52). Replace the block from after the logo `</a>` through the username `</a>` with:

```go
if base.CurrentUser != nil {
    <svg class="text-muted-foreground" width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1" aria-hidden="true" focusable="false"><path d="M16 4L8 20"/></svg>

    @components.DropdownMenu() {
        @components.DropdownMenuTrigger(templ.Attributes{
            "class":      "flex items-center gap-2 -ml-1 px-2 h-7 rounded-md hover:bg-accent text-sm font-medium",
            "aria-label": "Switch account or organization, current: " + base.CurrentUser.Username,
        }) {
            <span class="inline-grid place-items-center h-5 w-5 rounded bg-accent text-[10px] font-medium" aria-hidden="true">
                if len(base.CurrentUser.Username) > 0 {
                    { strings.ToUpper(base.CurrentUser.Username[:1]) }
                }
            </span>
            <span class="font-medium">{ base.CurrentUser.Username }</span>
            <svg class="text-muted-foreground" width="10" height="10" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" aria-hidden="true" focusable="false"><path d="M6 9l6 6 6-6"/></svg>
        }
        @components.DropdownMenuContent(components.DropdownAlignStart) {
            @components.DropdownMenuLabel() {
                { "Switch to" }
            }
            @components.DropdownMenuLink("/"+base.CurrentUser.Username, nil) {
                { base.CurrentUser.Username + " (personal)" }
            }
            for _, org := range base.UserOrgs {
                @components.DropdownMenuLink("/"+org.Name, nil) {
                    { org.Name }
                }
            }
            @components.DropdownMenuSeparator()
            @components.DropdownMenuLink("/organizations/new", nil) {
                { "New organization" }
            }
        }
    }
}
```

Add `"strings"` to the imports at the top of `layout.templ` if not already present.

- [ ] **Step 5b: Update the layout footer to match the mockup**

The current footer in `internal/view/layout/layout.templ` (lines 138–141) is:

```go
<footer role="contentinfo" class="text-center text-muted-foreground py-4 text-xs border-t border-border">
    <p>&copy; 2026 Cloudzilla. Created by <a href="https://mkappworks.com" class="underline hover:text-foreground">mkappworks</a></p>
</footer>
```

Replace it with a two-column footer matching `mockups/repo.html` lines 568–578:

```go
<footer role="contentinfo" class="mt-20 pt-6 border-t border-border flex items-center justify-between text-[11px] text-muted-foreground max-w-[1400px] mx-auto px-6 pb-6">
    <p class="font-mono">
        cloudzilla / v0.2.0 <span aria-hidden="true">·</span>
        <span role="status">
            <span aria-hidden="true" class="text-success">●</span> all systems normal
        </span>
    </p>
    <nav aria-label="Footer">
        <ul class="flex gap-4 list-none">
            <li><a href="/docs" class="hover:text-foreground">Docs</a></li>
            <li><a href="/api" class="hover:text-foreground">API</a></li>
            <li><a href="/changelog" class="hover:text-foreground">Changelog</a></li>
            <li><a href="/status" class="hover:text-foreground">Status</a></li>
        </ul>
    </nav>
</footer>
```

The version string is hardcoded here for Phase 0; a follow-up phase will wire it to the build-time version. The `Docs / API / Changelog / Status` links point to existing or planned routes — leave them as anchor hrefs even if some 404 today; their targets are owned by later phases.

- [ ] **Step 6: Regenerate templ**

```bash
~/go/bin/templ generate
```

Expected: no errors. `internal/view/layout/layout_templ.go` is regenerated.

- [ ] **Step 7: Build and run the server**

```bash
go build ./... && make dev
```

In a separate shell, log in as a test user. Open the home page. Confirm the workspace switcher dropdown appears after the logo, opens on click, lists Personal + any orgs, and shows "New organization" at the bottom.

- [ ] **Step 8: Visual check in both themes**

Toggle theme via the moon/sun button. Confirm the dropdown renders cleanly in both light and dark.

- [ ] **Step 9: Commit**

```bash
git add internal/view/layout/ internal/handler/ internal/view/
~/go/bin/templ generate
git add internal/view/layout/layout_templ.go
git commit -m "feat(ui): add workspace switcher dropdown to layout top-nav"
```

---

### Task 5: Extract repo subnav fragment

Every repo-scoped page in phases 1–6 needs the 9-tab repo subnav. Per `mockups/repo.html` lines 194–228 the tab order and labels are: **Code / Issues / Pull requests / Actions / Discussions / Projects / Wiki / Releases / Settings**. Several tabs carry count badges in the mockup (Issues `23`, Pull requests `12`, Discussions `5`, Releases `3`); the fragment must accept an optional per-tab count.

Tab keys (snake_case; `pull_requests` is plural to keep it distinct from the singular `pull` used by per-PR routes): `code`, `issues`, `pull_requests`, `actions`, `discussions`, `projects`, `wiki`, `releases`, `settings`.

- [ ] **Step 1: Confirm the fragment doesn't already exist**

```bash
ls internal/view/fragments/repo_subnav.templ 2>/dev/null && echo "EXISTS — abort and reconcile" || echo "ok to create"
```

**Files:**
- Create: `internal/view/fragments/repo_subnav.templ`
- Create: `internal/view/fragments/repo_subnav_test.go`

- [ ] **Step 2: Write the failing render test**

In `internal/view/fragments/repo_subnav_test.go`:

```go
package fragments

import (
    "bytes"
    "context"
    "strings"
    "testing"

    "github.com/mkappworks-dev/cloudzilla-app/internal/model"
)

func TestRepoSubnav_RendersAllTabs(t *testing.T) {
    repo := &model.Repository{Name: "demo"}
    data := RepoSubnavData{
        OwnerName: "alice",
        Repo:      repo,
        Active:    "code",
    }
    var buf bytes.Buffer
    if err := RepoSubnav(data).Render(context.Background(), &buf); err != nil {
        t.Fatalf("render: %v", err)
    }
    out := buf.String()
    for _, tab := range []string{"Code", "Issues", "Pull requests", "Actions", "Discussions", "Projects", "Wiki", "Releases", "Settings"} {
        if !strings.Contains(out, tab) {
            t.Errorf("expected %q in output, got: %s", tab, out)
        }
    }
}

func TestRepoSubnav_MarksActiveTab(t *testing.T) {
    repo := &model.Repository{Name: "demo"}
    data := RepoSubnavData{OwnerName: "alice", Repo: repo, Active: "pull_requests"}
    var buf bytes.Buffer
    if err := RepoSubnav(data).Render(context.Background(), &buf); err != nil {
        t.Fatalf("render: %v", err)
    }
    if !strings.Contains(buf.String(), `aria-current="page"`) {
        t.Errorf("expected aria-current on active tab; got: %s", buf.String())
    }
}

func TestRepoSubnav_RendersCountBadges(t *testing.T) {
    repo := &model.Repository{Name: "demo"}
    data := RepoSubnavData{
        OwnerName: "alice",
        Repo:      repo,
        Active:    "code",
        Counts: map[string]int{
            "issues":        23,
            "pull_requests": 12,
            "discussions":   5,
            "releases":      3,
        },
    }
    var buf bytes.Buffer
    if err := RepoSubnav(data).Render(context.Background(), &buf); err != nil {
        t.Fatalf("render: %v", err)
    }
    out := buf.String()
    for _, n := range []string{"23", "12", "5", "3"} {
        if !strings.Contains(out, ">"+n+"<") {
            t.Errorf("expected count badge %q in output", n)
        }
    }
}

func TestRepoSubnav_OmitsZeroAndMissingCounts(t *testing.T) {
    repo := &model.Repository{Name: "demo"}
    data := RepoSubnavData{
        OwnerName: "alice",
        Repo:      repo,
        Active:    "code",
        Counts:    map[string]int{"issues": 0},
    }
    var buf bytes.Buffer
    if err := RepoSubnav(data).Render(context.Background(), &buf); err != nil {
        t.Fatalf("render: %v", err)
    }
    // A zero count must not render a badge (mockup shows badges only for non-zero values).
    if strings.Contains(buf.String(), `aria-label="0`) {
        t.Errorf("did not expect zero-count badge; got: %s", buf.String())
    }
}
```

- [ ] **Step 3: Run the test to verify it fails**

```bash
go test ./internal/view/fragments/ -run TestRepoSubnav -v
```

Expected: FAIL with "undeclared name: RepoSubnav" or "undeclared type: RepoSubnavData".

- [ ] **Step 4: Create the fragment**

In `internal/view/fragments/repo_subnav.templ`:

```go
package fragments

import (
    "strconv"

    "github.com/mkappworks-dev/cloudzilla-app/internal/model"
)

// RepoSubnavData holds the inputs the 9-tab repo subnav needs.
type RepoSubnavData struct {
    OwnerName string
    Repo      *model.Repository
    // Active is one of: "code", "issues", "pull_requests", "actions",
    // "discussions", "projects", "wiki", "releases", "settings". Empty
    // string means no tab highlighted.
    Active string
    // Counts is an optional per-tab count badge keyed by the tab key
    // above. A zero or missing value renders no badge. May be nil.
    Counts map[string]int
}

templ RepoSubnav(data RepoSubnavData) {
    <nav aria-label="Repository sections" class="border-b border-border">
        <div class="max-w-[1400px] mx-auto px-6 flex items-center gap-1 overflow-x-auto">
            @repoSubnavTab(data, "code", "Code", "/"+data.OwnerName+"/"+data.Repo.Name)
            @repoSubnavTab(data, "issues", "Issues", "/"+data.OwnerName+"/"+data.Repo.Name+"/issues")
            @repoSubnavTab(data, "pull_requests", "Pull requests", "/"+data.OwnerName+"/"+data.Repo.Name+"/pulls")
            @repoSubnavTab(data, "actions", "Actions", "/"+data.OwnerName+"/"+data.Repo.Name+"/actions")
            @repoSubnavTab(data, "discussions", "Discussions", "/"+data.OwnerName+"/"+data.Repo.Name+"/discussions")
            @repoSubnavTab(data, "projects", "Projects", "/"+data.OwnerName+"/"+data.Repo.Name+"/projects")
            @repoSubnavTab(data, "wiki", "Wiki", "/"+data.OwnerName+"/"+data.Repo.Name+"/wiki")
            @repoSubnavTab(data, "releases", "Releases", "/"+data.OwnerName+"/"+data.Repo.Name+"/releases")
            @repoSubnavTab(data, "settings", "Settings", "/"+data.OwnerName+"/"+data.Repo.Name+"/settings")
        </div>
    </nav>
}

templ repoSubnavTab(data RepoSubnavData, key, label, href string) {
    if data.Active == key {
        <a href={ templ.SafeURL(href) } aria-current="page" class="px-3 py-2 text-sm font-medium text-foreground border-b-2 border-foreground -mb-px inline-flex items-center gap-1.5">
            { label }
            if count, ok := data.Counts[key]; ok && count > 0 {
                <span class="font-mono text-[11px] text-muted-foreground/70 ml-0.5" aria-label={ strconv.Itoa(count) + " " + label }>{ strconv.Itoa(count) }</span>
            }
        </a>
    } else {
        <a href={ templ.SafeURL(href) } class="px-3 py-2 text-sm text-muted-foreground hover:text-foreground border-b-2 border-transparent -mb-px inline-flex items-center gap-1.5">
            { label }
            if count, ok := data.Counts[key]; ok && count > 0 {
                <span class="font-mono text-[11px] text-muted-foreground/70 ml-0.5" aria-label={ strconv.Itoa(count) + " " + label }>{ strconv.Itoa(count) }</span>
            }
        </a>
    }
}
```

- [ ] **Step 5: Regenerate and run tests**

```bash
~/go/bin/templ generate
go test ./internal/view/fragments/ -run TestRepoSubnav -v
```

Expected: all four tests PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/view/fragments/repo_subnav.templ internal/view/fragments/repo_subnav_templ.go internal/view/fragments/repo_subnav_test.go
git commit -m "feat(ui): extract repo subnav fragment (9 tabs with optional count badges)"
```

---

### Task 6: Extract dashboard subnav fragment

Same pattern for the 8-tab user dashboard subnav. Used by phases 8–9.

**Files:**
- Create: `internal/view/fragments/dashboard_subnav.templ`
- Create: `internal/view/fragments/dashboard_subnav_test.go`

- [ ] **Step 1: Write the failing test**

In `internal/view/fragments/dashboard_subnav_test.go`:

```go
package fragments

import (
    "bytes"
    "context"
    "strings"
    "testing"
)

func TestDashboardSubnav_RendersAllTabs(t *testing.T) {
    data := DashboardSubnavData{Username: "alice", Active: "overview"}
    var buf bytes.Buffer
    if err := DashboardSubnav(data).Render(context.Background(), &buf); err != nil {
        t.Fatalf("render: %v", err)
    }
    out := buf.String()
    for _, tab := range []string{"Overview", "Repositories", "Projects", "Packages", "Stars", "Pulls", "Issues", "Activity"} {
        if !strings.Contains(out, tab) {
            t.Errorf("expected %q in output", tab)
        }
    }
}
```

- [ ] **Step 2: Run test, verify FAIL**

```bash
go test ./internal/view/fragments/ -run TestDashboardSubnav -v
```

Expected: FAIL.

- [ ] **Step 3: Create the fragment**

In `internal/view/fragments/dashboard_subnav.templ`:

```go
package fragments

type DashboardSubnavData struct {
    Username string
    // Active: "overview", "repositories", "projects", "packages",
    // "stars", "pulls", "issues", "activity"
    Active string
}

templ DashboardSubnav(data DashboardSubnavData) {
    <nav aria-label="Dashboard sections" class="border-b border-border">
        <div class="max-w-[1400px] mx-auto px-6 flex items-center gap-1 overflow-x-auto">
            @dashTab(data, "overview", "Overview", "/"+data.Username)
            @dashTab(data, "repositories", "Repositories", "/"+data.Username+"?tab=repositories")
            @dashTab(data, "projects", "Projects", "/"+data.Username+"?tab=projects")
            @dashTab(data, "packages", "Packages", "/"+data.Username+"?tab=packages")
            @dashTab(data, "stars", "Stars", "/stars")
            @dashTab(data, "pulls", "Pulls", "/pulls")
            @dashTab(data, "issues", "Issues", "/issues")
            @dashTab(data, "activity", "Activity", "/activity")
        </div>
    </nav>
}

templ dashTab(data DashboardSubnavData, key, label, href string) {
    if data.Active == key {
        <a href={ templ.SafeURL(href) } aria-current="page" class="px-3 py-2 text-sm font-medium text-foreground border-b-2 border-foreground -mb-px">
            { label }
        </a>
    } else {
        <a href={ templ.SafeURL(href) } class="px-3 py-2 text-sm text-muted-foreground hover:text-foreground border-b-2 border-transparent -mb-px">
            { label }
        </a>
    }
}
```

- [ ] **Step 4: Regenerate and run tests**

```bash
~/go/bin/templ generate
go test ./internal/view/fragments/ -run TestDashboardSubnav -v
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/view/fragments/dashboard_subnav.templ internal/view/fragments/dashboard_subnav_templ.go internal/view/fragments/dashboard_subnav_test.go
git commit -m "feat(ui): extract dashboard subnav fragment (8 tabs)"
```

---

### Task 7: Orphan-page token sweep

Pass over orphan templ files (pages with no mockup), replacing any lingering raw color/border classes with shadcn tokens. **No layout changes.** Pure token substitution so orphans don't look broken next to redesigned pages.

**Files (all modify):**
- `internal/view/pages/admin_settings.templ`
- `internal/view/pages/audit_log.templ`
- `internal/view/pages/code_search.templ`
- `internal/view/pages/milestones.templ`
- `internal/view/pages/notification_settings.templ`
- `internal/view/pages/notifications.templ`
- `internal/view/pages/oauth_apps.templ`
- `internal/view/pages/oauth_authorize.templ`
- `internal/view/pages/saved_replies.templ`
- `internal/view/pages/search.templ`
- `internal/view/pages/security.templ`
- `internal/view/pages/setup.templ`
- `internal/view/pages/sso_settings.templ`
- `internal/view/pages/tokens.templ`
- `internal/view/pages/totp_verify.templ`
- `internal/view/pages/wiki_edit.templ`
- `internal/view/pages/user_gists.templ`
- `internal/view/pages/user_stars.templ`

- [ ] **Step 1: Inventory remaining hand-rolled classes**

```bash
grep -rn -E '(bg-gray-[0-9]+|text-white|text-gray-[0-9]+|border-white/[0-9]+|border-gray-[0-9]+|bg-zinc-[0-9]+|bg-neutral-[0-9]+)' internal/view/pages/ | grep -v '_templ.go' > /tmp/orphan_sweep.txt
wc -l /tmp/orphan_sweep.txt
cat /tmp/orphan_sweep.txt
```

Note the line count.

- [ ] **Step 2: Apply substitutions per file**

For each occurrence in `/tmp/orphan_sweep.txt`, apply per this conversion map:

| Lingering class | Replace with |
|---|---|
| `bg-gray-900`, `bg-zinc-900`, `bg-neutral-900` | `bg-background` |
| `bg-gray-800`, `bg-zinc-800`, `bg-neutral-800` | `bg-card` |
| `bg-gray-700`, `bg-zinc-700`, `bg-neutral-700` | `bg-accent` |
| `text-white` | `text-foreground` |
| `text-gray-300`, `text-gray-400` | `text-muted-foreground` |
| `text-gray-500`, `text-gray-600` | `text-muted-foreground/70` |
| `border-white/10`, `border-gray-800` | `border-border` |
| `border-white/20` | `border-border-strong` |
| `hover:bg-gray-800`, `hover:bg-zinc-800` | `hover:bg-accent` |

Use `Edit` per file or `sed` only if the class appears in a single context per file. **Do not blanket-rewrite** — read the surrounding markup so you don't accidentally swap a brand-colored element.

- [ ] **Step 3: Regenerate templ for all modified files**

```bash
~/go/bin/templ generate
```

Expected: no errors.

- [ ] **Step 4: Build CSS and run server**

```bash
make build-css && make dev
```

- [ ] **Step 5: Visual sweep**

Open each of the modified pages in the browser (most require login as a particular role):
- `/admin/settings` (superadmin)
- `/admin/audit-log` (superadmin)
- `/settings/security`, `/settings/notifications`, `/settings/tokens`, `/settings/sso`, `/settings/oauth-apps`, `/settings/saved-replies`
- `/notifications` (the inbox page)
- `/search?q=test` and `/code-search?q=test`
- `/<owner>/<repo>/milestones`
- `/<owner>/<repo>/wiki/<page>/edit`
- `/<username>/gists`, `/<username>/stars`
- `/login/totp` (mid-2FA flow)
- `/oauth/authorize?...` (during OAuth flow)
- `/setup` (only renders before first user exists)

Toggle theme on each. Confirm: nothing renders as black-on-black, no invisible borders, no white-on-white. **Do not redesign anything** — just confirm tokens render correctly.

- [ ] **Step 6: Run linter**

```bash
make lint && go test ./...
```

Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add internal/view/pages/
git commit -m "feat(ui): sweep orphan pages onto shadcn tokens"
```

---

### Task 8: Verify phase exit criteria

- [ ] **Step 1: Full test + lint pass**

```bash
go test ./... && make lint
```

Expected: PASS on both.

- [ ] **Step 2: Templ regen clean**

```bash
~/go/bin/templ generate
git status
```

Expected: no diff. If diff exists, commit it as `chore: regenerate templ`.

- [ ] **Step 3: silent-failure-hunter agent**

Per `CLAUDE.md`, run the silent-failure-hunter agent against the branch diff. Fix any high-confidence findings before opening the PR.

- [ ] **Step 4: Push and open the PR**

```bash
git push -u origin feat/ui-overhaul-phase-0-foundation
gh pr create --title "feat(ui): UI overhaul phase 0 — foundation" --body "$(cat <<'EOF'
## Summary
- Adds the class translation map at `docs/ui-overhaul-class-map.md` (source of truth for the UI overhaul; downstream phases reference this).
- Promotes `grid-bg`, `heatmap-cell-*`, and `--border-strong` to project-level CSS in `tailwind/input.css`.
- Syncs the layout chrome with the mockups: adds a workspace switcher dropdown wired to `OrgService.ListOwnedByUser`.
- Extracts the 9-tab repo subnav and the 8-tab dashboard subnav into `internal/view/fragments/`.
- Token sweep over orphan templ pages (admin, audit log, settings, notifications, OAuth, search, milestones, gists/stars) so they don't look broken next to redesigned pages. No layout changes.

Spec: `docs/superpowers/specs/2026-05-14-ui-overhaul-design.md`
Plan: `docs/superpowers/plans/2026-05-14-ui-overhaul-phase-0-foundation.md`

## Test plan
- [x] `go test ./...` passes
- [x] `make lint` passes
- [x] `templ generate` clean
- [x] silent-failure-hunter agent run, no high-confidence findings
- [x] Manual visual sweep: workspace switcher opens, lists orgs, navigates correctly
- [x] Manual visual sweep: every orphan page renders cleanly in both light and dark
- [x] Forced-colors test in a Windows VM or Chrome devtools forced-colors emulator

🤖 Generated with [Claude Code](https://claude.com/claude-code)
EOF
)"
```

---

## Self-review checklist

- [ ] Class translation map covers every mockup utility class found in `mockups/home.html` `<style>` block (the most representative).
- [ ] `--border-strong` defined in both `:root` and `.dark`.
- [ ] `grid-bg` works in both themes (uses HSL var, not hardcoded color).
- [ ] Workspace switcher loads `UserOrgs` only when `CurrentUser != nil`.
- [ ] Both subnav fragments have tests that fail before implementation and pass after.
- [ ] Orphan sweep substitution map is conservative — no risk of swapping a brand color.
- [ ] PR description references the spec and plan paths.
