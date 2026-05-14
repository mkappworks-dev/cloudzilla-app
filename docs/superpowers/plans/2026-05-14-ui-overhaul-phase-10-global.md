# UI Overhaul · Phase 10 · Settings & Global Pages — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Consolidate `settings.templ` + `security.templ` + `tokens.templ` + `notification_settings.templ` into a single anchor-scrolled account-settings page. Add new `/docs` and `/changelog` pages. Refine the auth pages (login/register/totp/oauth/invite/404/403) to use a shared `AuthCard` shell that supports six panel variants. Backfill two Phase 1 mockup sections deferred to this phase: the home keyboard-shortcuts reference sidebar and the PR detail notification-subscribe toggle.

**Architecture:** No migrations. `DocsService` parses markdown from a curated `docs/public/` subtree via `embed.FS` at startup. `ChangelogService` parses `CHANGELOG.md` (release-please format) at startup. `AuthCard` is a children-based templ shell that adapts to six panel variants via an explicit `variant` field plus optional hero/icon slots.

**Prerequisites:** Phase 6 (delivers `ExtractWikiTOC` / `markdown.Render` polish in the wiki) **and** Phase 9 merged. The wiki TOC extractor introduced in Phase 6 is reused by `DocsService`.

**Spec:** [2026-05-14-ui-overhaul-design.md](../specs/2026-05-14-ui-overhaul-design.md)

**Branch:** `feat/ui-overhaul-phase-10-global`

---

### Task 1: Branch setup

- [ ] `git checkout main && git pull && git checkout -b feat/ui-overhaul-phase-10-global`

---

### Task 2: `DocsService` for /docs

**Important — what to embed:** `docs/` at the repo root contains internal architecture notes (`superpowers/`, `roadmap.md`, `pr-merge.md`, `access-control.md`, etc.) that must NOT be exposed to end users. Create a **new** `docs/public/` subdirectory and embed *only that*. Starter pages required as part of this task:

- `docs/public/getting-started/install.md`
- `docs/public/getting-started/first-repo.md`
- `docs/public/getting-started/ssh.md`
- `docs/public/reference/api-reference.md` (move/adapt from `docs/api-reference.md`)
- `docs/public/reference/cli.md`
- `docs/public/reference/webhooks.md` (adapt from `docs/webhooks.md`)
- `docs/public/operate/deploy.md` (adapt from `docs/deployment.md`)
- `docs/public/operate/upgrade.md`
- `docs/public/operate/backup.md`

Each file lives under a single category directory; the directory name becomes the sidebar group ("Getting started" / "Reference" / "Operate") to match the `mockups/docs.html` grouped sidebar.

**Files:**
- Create: `internal/service/docs_service.go`
- Create: `internal/service/docs_service_test.go`
- Create: `cmd/server/docs_fs.go` (embed.FS reference)
- Create: `internal/handler/docs_handler.go`
- Create: `docs/public/**/*.md` (starter pages listed above)
- Modify: `internal/router/router.go`

- [ ] **Step 1: Test**

```go
func TestDocsService_ListAndRender(t *testing.T) {
    src := map[string]string{
        "getting-started/install.md": "# Install\n\nWelcome.",
        "reference/api.md":           "# API\n\n## Auth",
    }
    svc := NewDocsServiceFromMap(src)

    pages := svc.ListPages()
    if len(pages) != 2 { t.Fatalf("expected 2 pages, got %d", len(pages)) }

    page, err := svc.GetPage("getting-started/install")
    if err != nil { t.Fatalf("GetPage: %v", err) }
    if !strings.Contains(page.HTML, "Welcome") {
        t.Errorf("rendered HTML missing prose: %s", page.HTML)
    }
    if page.Category != "Getting started" {
        t.Errorf("category should be derived from top-level dir, got %q", page.Category)
    }

    groups := svc.Groups()
    if len(groups) != 2 { t.Fatalf("expected 2 groups, got %d", len(groups)) }
}
```

- [ ] **Step 2: Implement**

```go
// internal/service/docs_service.go
package service

import (
    "fmt"
    "io/fs"
    "path"
    "sort"
    "strings"

    "github.com/mkappworks-dev/cloudzilla-app/internal/markdown"
)

type DocPage struct {
    Slug     string // e.g. "getting-started/install"
    Title    string
    Category string // derived from first path segment ("Getting started")
    HTML     string
    TOC      []WikiTOCEntry
}

// DocGroup is the sidebar grouping derived from the first directory segment.
type DocGroup struct {
    Name  string
    Pages []DocPage
}

type DocsService struct {
    pages []DocPage
    byKey map[string]*DocPage
}

func NewDocsServiceFromFS(fsys fs.FS) *DocsService {
    svc := &DocsService{byKey: map[string]*DocPage{}}
    _ = fs.WalkDir(fsys, ".", func(p string, d fs.DirEntry, err error) error {
        if err != nil || d.IsDir() || !strings.HasSuffix(p, ".md") {
            return nil
        }
        data, err := fs.ReadFile(fsys, p)
        if err != nil {
            return nil
        }
        svc.add(p, data)
        return nil
    })
    sort.Slice(svc.pages, func(i, j int) bool {
        if svc.pages[i].Category != svc.pages[j].Category {
            return svc.pages[i].Category < svc.pages[j].Category
        }
        return svc.pages[i].Title < svc.pages[j].Title
    })
    return svc
}

func NewDocsServiceFromMap(src map[string]string) *DocsService {
    var paths []string
    for k := range src {
        paths = append(paths, k)
    }
    sort.Strings(paths)
    svc := &DocsService{byKey: map[string]*DocPage{}}
    for _, p := range paths {
        svc.add(p, []byte(src[p]))
    }
    return svc
}

// add parses one markdown file. Slug = path minus ".md"; Category =
// humanised first directory segment ("getting-started" -> "Getting started").
func (s *DocsService) add(p string, data []byte) {
    slug := strings.TrimSuffix(p, ".md")
    parts := strings.SplitN(slug, "/", 2)
    category := ""
    if len(parts) == 2 {
        category = humaniseDir(parts[0])
    }
    html := markdown.Render(string(data))
    page := DocPage{
        Slug:     slug,
        Title:    docTitleFromMarkdown(data),
        Category: category,
        HTML:     html,
        TOC:      ExtractWikiTOC(html), // delivered by Phase 6
    }
    s.pages = append(s.pages, page)
    s.byKey[slug] = &s.pages[len(s.pages)-1]
}

func (s *DocsService) ListPages() []DocPage { return s.pages }

func (s *DocsService) GetPage(slug string) (*DocPage, error) {
    p, ok := s.byKey[slug]
    if !ok {
        return nil, fmt.Errorf("page %q not found", slug)
    }
    return p, nil
}

// Groups returns sidebar groups in stable order, preserving the order in
// which categories first appeared in the sorted page list.
func (s *DocsService) Groups() []DocGroup {
    var groups []DocGroup
    byName := map[string]*DocGroup{}
    for _, p := range s.pages {
        g, ok := byName[p.Category]
        if !ok {
            groups = append(groups, DocGroup{Name: p.Category})
            g = &groups[len(groups)-1]
            byName[p.Category] = g
        }
        g.Pages = append(g.Pages, p)
    }
    return groups
}

func humaniseDir(dir string) string {
    // "getting-started" -> "Getting started"
    s := strings.ReplaceAll(dir, "-", " ")
    s = strings.ReplaceAll(s, "_", " ")
    if s == "" { return s }
    return strings.ToUpper(s[:1]) + s[1:]
}

func docTitleFromMarkdown(data []byte) string {
    for _, line := range strings.Split(string(data), "\n") {
        line = strings.TrimSpace(line)
        if strings.HasPrefix(line, "# ") {
            return strings.TrimSpace(strings.TrimPrefix(line, "# "))
        }
    }
    return ""
}
```

`markdown.Render(string) string` already exists in `internal/markdown/markdown.go` (used by wiki/PRs/comments). `ExtractWikiTOC` is introduced by Phase 6.

- [ ] **Step 3: embed the docs directory**

Embed only the curated public subtree. Because `cmd/server/` lives below the repo root, the public docs are copied next to the binary's source via a Makefile step rather than a `..`-relative embed path (`//go:embed` rejects parent traversal — see https://pkg.go.dev/embed):

```makefile
# Makefile
.PHONY: stage-embeds
stage-embeds:
	rm -rf cmd/server/embedded_docs cmd/server/embedded_changelog
	mkdir -p cmd/server/embedded_docs
	cp -R docs/public/. cmd/server/embedded_docs/
	cp CHANGELOG.md cmd/server/embedded_changelog.md

build: stage-embeds build-css
	go build -o dist/cloudzilla ./cmd/server

dev: stage-embeds
	# existing dev recipe
```

Add `cmd/server/embedded_docs/` and `cmd/server/embedded_changelog.md` to `.gitignore`.

```go
// cmd/server/docs_fs.go
package main

import "embed"

//go:embed all:embedded_docs
var docsFS embed.FS
```

Update `cmd/server/main.go` to construct `DocsService`:

```go
docsSub, _ := fs.Sub(docsFS, "embedded_docs")
docsSvc := service.NewDocsServiceFromFS(docsSub)
services.Docs = docsSvc
```

Add `Docs *DocsService` to `Services` struct.

- [ ] **Step 4: Handler + routes**

```go
// internal/handler/docs_handler.go
func (h *Handler) PageDocsIndex(w http.ResponseWriter, r *http.Request) {
    pages := h.Services.Docs.ListPages()
    if len(pages) == 0 { http.NotFound(w, r); return }
    first := pages[0]
    http.Redirect(w, r, "/docs/"+first.Slug, http.StatusFound)
}

func (h *Handler) PageDocsPage(w http.ResponseWriter, r *http.Request) {
    // Slugs are nested ("getting-started/install"); use chi wildcard.
    slug := strings.TrimPrefix(chi.URLParam(r, "*"), "/")
    page, err := h.Services.Docs.GetPage(slug)
    if err != nil { http.NotFound(w, r); return }
    data := view.DocsPageData{
        BasePage: h.basePage(r),
        Groups:   h.Services.Docs.Groups(),
        Current:  page,
    }
    h.render(w, r, pages.Docs(data), "Docs · "+page.Title)
}
```

In `internal/router/router.go`:

```go
r.Get("/docs", h.PageDocsIndex)
r.Get("/docs/*", h.PageDocsPage) // wildcard so nested slugs (cat/page) resolve
```

- [ ] **Step 5: Commit**

```bash
go test ./internal/service/ -run TestDocsService -v
git add internal/service/docs_service.go internal/service/docs_service_test.go internal/handler/docs_handler.go internal/router/router.go internal/service/services.go cmd/server/main.go cmd/server/docs_fs.go
git commit -m "feat(service): add DocsService + /docs routes"
```

---

### Task 3: `ChangelogService` for /changelog

**Files:**
- Create: `internal/service/changelog_service.go`
- Create: `internal/service/changelog_service_test.go`
- Create: `internal/handler/changelog_handler.go`
- Modify: `internal/router/router.go`

- [ ] **Step 1: Test**

The real `CHANGELOG.md` is generated by release-please and uses a different heading format than the placeholder spec assumed. Example actual headings from the repo:

```markdown
## [0.2.0](https://github.com/mkappworks-dev/cloudzilla-app/compare/v0.1.0...v0.2.0) (2026-05-12)


### Features

* **access-token:** implement Personal Access Tokens (Phase 5.1) ([5bdd229](...))
* add admin CLI with cobra ([2f5b510](...))

### Bug Fixes

* **audit:** address security review findings ([830d9cc](...))
```

Differences from the legacy assumption:
- Version is a markdown link: `[X.Y.Z](url)` (no spaces).
- Date is in **parentheses**, not after a hyphen.
- Subsection heading is `Bug Fixes` (capitalised) and items use `*` bullets, not `-`.
- Some categories use `Documentation`, `Build`, `CI`, `Tests`, `Refactors` — fall back to `"other"`.

Tests must cover both the live release-please format and the simpler legacy `## X.Y.Z - YYYY-MM-DD` fallback. Embed the real CHANGELOG.md as a fixture so regressions are caught when release-please changes format.

```go
//go:embed testdata/changelog_real.md
var changelogRealFixture []byte

func TestChangelogService_ParseReleasePlease(t *testing.T) {
    entries, err := ParseChangelog(changelogRealFixture)
    if err != nil { t.Fatalf("Parse: %v", err) }
    if len(entries) == 0 {
        t.Fatalf("expected >=1 release-please entry, got 0")
    }
    if entries[0].Version != "0.2.0" {
        t.Errorf("first entry should be 0.2.0, got %q", entries[0].Version)
    }
    if entries[0].Date.Format("2006-01-02") != "2026-05-12" {
        t.Errorf("date should be 2026-05-12, got %v", entries[0].Date)
    }
    var features, fixes int
    for _, c := range entries[0].Categories {
        switch c.Kind {
        case "feat": features = len(c.Items)
        case "fix":  fixes = len(c.Items)
        }
    }
    if features < 10 {
        t.Errorf("expected many Features in 0.2.0, got %d", features)
    }
    if fixes == 0 {
        t.Errorf("expected Bug Fixes in 0.2.0")
    }
}

func TestChangelogService_ParseLegacyFormat(t *testing.T) {
    src := `# Changelog

## [0.2.0] - 2026-05-12

### Features
- foo
- bar

### Bug fixes
- baz

## 0.1.0 - 2026-04-01

### Features
- initial release
`
    entries, err := ParseChangelog([]byte(src))
    if err != nil { t.Fatalf("Parse: %v", err) }
    if len(entries) != 2 {
        t.Fatalf("expected 2 versions, got %d", len(entries))
    }
    if entries[0].Version != "0.2.0" || entries[1].Version != "0.1.0" {
        t.Errorf("versions mismatched: %+v", entries)
    }
}
```

Copy the real `CHANGELOG.md` into `internal/service/testdata/changelog_real.md` so the fixture is hermetic.

- [ ] **Step 2: Implement**

```go
// internal/service/changelog_service.go
package service

import (
    "bufio"
    "bytes"
    "regexp"
    "strings"
    "time"
)

type ChangelogEntry struct {
    Version    string
    Date       time.Time
    Categories []ChangelogCategory
}

type ChangelogCategory struct {
    Kind  string // "feat", "fix", "breaking", "other"
    Items []string
}

// Two regexes — release-please's primary format first, then the simpler
// legacy `## X.Y.Z - YYYY-MM-DD` fallback. Both produce (version, date).
//
// release-please example:
//   ## [0.2.0](https://github.com/owner/repo/compare/v0.1.0...v0.2.0) (2026-05-12)
//
// legacy example:
//   ## [0.2.0] - 2026-05-12
//   ## 0.2.0 - 2026-05-12
var (
    versionReReleasePlease = regexp.MustCompile(`^##\s+\[([0-9]+\.[0-9]+\.[0-9]+)\]\([^)]+\)\s+\(([0-9]{4}-[0-9]{2}-[0-9]{2})\)`)
    versionReLegacy        = regexp.MustCompile(`^##\s+\[?([0-9]+\.[0-9]+\.[0-9]+)\]?\s*-\s*([0-9]{4}-[0-9]{2}-[0-9]{2})`)
)

func matchVersion(line string) (version, date string, ok bool) {
    if m := versionReReleasePlease.FindStringSubmatch(line); m != nil {
        return m[1], m[2], true
    }
    if m := versionReLegacy.FindStringSubmatch(line); m != nil {
        return m[1], m[2], true
    }
    return "", "", false
}

func ParseChangelog(data []byte) ([]ChangelogEntry, error) {
    var out []ChangelogEntry
    var cur *ChangelogEntry
    var catKind string

    scanner := bufio.NewScanner(bytes.NewReader(data))
    scanner.Buffer(make([]byte, 64*1024), 1<<20) // release-please files can be large
    flushCat := func(items []string) {
        if cur == nil || catKind == "" { return }
        cur.Categories = append(cur.Categories, ChangelogCategory{Kind: catKind, Items: items})
    }
    var items []string

    for scanner.Scan() {
        line := scanner.Text()
        if v, d, ok := matchVersion(line); ok {
            if cur != nil {
                flushCat(items)
                items, catKind = nil, ""
                out = append(out, *cur)
            }
            t, _ := time.Parse("2006-01-02", d)
            cur = &ChangelogEntry{Version: v, Date: t}
            continue
        }
        if strings.HasPrefix(line, "### ") {
            flushCat(items)
            items = nil
            heading := strings.ToLower(strings.TrimSpace(strings.TrimPrefix(line, "### ")))
            switch {
            case strings.Contains(heading, "break"):    catKind = "breaking"
            case strings.Contains(heading, "feat"):     catKind = "feat"
            case strings.Contains(heading, "fix"):      catKind = "fix" // matches "Bug Fixes"
            case strings.Contains(heading, "perf"):     catKind = "perf"
            case strings.Contains(heading, "doc"):      catKind = "docs"
            default:                                    catKind = "other"
            }
            continue
        }
        // release-please uses "* ", legacy uses "- "
        if strings.HasPrefix(line, "- ") || strings.HasPrefix(line, "* ") {
            items = append(items, strings.TrimSpace(line[2:]))
        }
    }
    if cur != nil {
        flushCat(items)
        out = append(out, *cur)
    }
    return out, scanner.Err()
}

type ChangelogService struct {
    entries []ChangelogEntry
}

func NewChangelogServiceFromBytes(data []byte) *ChangelogService {
    entries, _ := ParseChangelog(data)
    return &ChangelogService{entries: entries}
}

func (s *ChangelogService) Entries() []ChangelogEntry { return s.entries }
```

- [ ] **Step 3: Load CHANGELOG.md at startup**

`//go:embed` cannot use `..` paths — `//go:embed ../../CHANGELOG.md` is rejected by the Go compiler. The `stage-embeds` Makefile target introduced in Task 2 copies the repo-root `CHANGELOG.md` to `cmd/server/embedded_changelog.md`. Embed *that* file:

```go
// cmd/server/changelog_fs.go
package main

import _ "embed"

//go:embed embedded_changelog.md
var changelogData []byte
```

Then in `cmd/server/main.go`:

```go
services.Changelog = service.NewChangelogServiceFromBytes(changelogData)
```

`cmd/server/embedded_changelog.md` is gitignored; it's regenerated on every `make build` / `make dev`. The CI build must run `make stage-embeds` (or just `make build`) before `go build`.

- [ ] **Step 4: Handler + route**

```go
// internal/handler/changelog_handler.go
func (h *Handler) PageChangelog(w http.ResponseWriter, r *http.Request) {
    data := view.ChangelogData{
        BasePage: h.basePage(r),
        Entries:  h.Services.Changelog.Entries(),
    }
    h.render(w, r, pages.Changelog(data), "Changelog")
}
```

```go
// internal/router/router.go
r.Get("/changelog", h.PageChangelog)
```

- [ ] **Step 5: Commit**

```bash
go test ./internal/service/ -run TestChangelogService -v
git add internal/service/changelog_service.go internal/service/changelog_service_test.go internal/handler/changelog_handler.go internal/router/router.go cmd/server/main.go
git commit -m "feat(service): add ChangelogService + /changelog route"
```

---

### Task 4: `AuthCard` component

**Files:**
- Create: `internal/view/components/auth_card.templ`
- Create: `internal/view/components/auth_card_test.go`

`AuthCard` covers six mockup variants (`mockups/auth.html`): login, 2FA, OAuth consent, invite, 404, 403. The first four share a centered ~28rem card with optional cloud-icon-or-status-icon hero, mono eyebrow, headline, subtitle, then a form body. 404 and 403 reuse the same card but with a grid-bg backdrop and a giant mono numeral hero in place of the icon.

To keep the API simple, use Templ's children block for the form body and pass the variant + hero parts as fields:

```go
// internal/view/components/auth_card.templ
package components

// AuthVariant selects hero + backdrop styling.
//
//   AuthVariantLogo      — cloud logo (login, register, invite)
//   AuthVariantIcon      — circular status icon (2FA, password reset)
//   AuthVariantNumeral   — huge mono numeral + grid backdrop (404, 403)
//   AuthVariantPlain     — no hero (OAuth consent)
type AuthVariant string

const (
    AuthVariantLogo    AuthVariant = "logo"
    AuthVariantIcon    AuthVariant = "icon"
    AuthVariantNumeral AuthVariant = "numeral"
    AuthVariantPlain   AuthVariant = "plain"
)

type AuthCardProps struct {
    Variant  AuthVariant
    Eyebrow  string         // mono uppercase eyebrow ("Cloudzilla", "Verify identity", "404 · Not found")
    Title    string
    Subtitle string         // optional one-line description under title
    Numeral  string         // shown when Variant == AuthVariantNumeral (e.g. "404")
    Icon     templ.Component // shown when Variant == AuthVariantIcon (e.g. lock SVG)
}

templ AuthCard(p AuthCardProps) {
    <main class={ "min-h-[80vh] grid place-items-center px-4",
                  templ.KV("relative overflow-hidden", p.Variant == AuthVariantNumeral) }>
        if p.Variant == AuthVariantNumeral {
            <div class="absolute inset-0 grid-bg opacity-40 pointer-events-none" aria-hidden="true"></div>
        }
        <div class="relative w-full max-w-sm">
            <div class="rounded-lg border border-border bg-card p-6 shadow-sm text-center">
                switch p.Variant {
                case AuthVariantLogo:
                    <a href="/" class="inline-flex" aria-label="Cloudzilla home">
                        @CloudIcon(36)
                    </a>
                case AuthVariantIcon:
                    if p.Icon != nil {
                        <span class="inline-grid place-items-center w-12 h-12 rounded-full border border-border bg-success/10">
                            @p.Icon
                        </span>
                    }
                case AuthVariantNumeral:
                    <p class="text-[7rem] font-mono font-semibold leading-none tracking-tight text-muted-foreground/70">{ p.Numeral }</p>
                }
                if p.Eyebrow != "" {
                    <p class="mt-4 font-mono text-[11px] text-muted-foreground uppercase tracking-wider">{ p.Eyebrow }</p>
                }
                <h1 class="mt-1 text-xl font-semibold tracking-tight">{ p.Title }</h1>
                if p.Subtitle != "" {
                    <p class="mt-2 text-sm text-muted-foreground">{ p.Subtitle }</p>
                }
                <div class="mt-5 text-left">
                    { children... }
                </div>
            </div>
        </div>
    </main>
}

// CloudIcon is exported because both AuthCard and the top header use it.
templ CloudIcon(size int) {
    <svg width={ fmt.Sprintf("%d", size) } height={ fmt.Sprintf("%d", size) } viewBox="0 0 64 64" fill="currentColor" aria-hidden="true">
        <circle cx="22" cy="36" r="9"/>
        <circle cx="32" cy="28" r="11"/>
        <circle cx="44" cy="36" r="9"/>
        <rect x="22" y="36" width="22" height="9" rx="4.5"/>
    </svg>
}
```

Callers use Templ's children syntax (`{ children... }`):

```go
@components.AuthCard(components.AuthCardProps{
    Variant: components.AuthVariantLogo,
    Eyebrow: "Cloudzilla",
    Title:   "Sign in to continue",
}) {
    <form>...</form>
}
```

Test, regenerate, commit.

---

### Task 5: Refactor existing auth pages onto `AuthCard`

**Files:** Modify `internal/view/pages/login.templ`, `register.templ`, `totp_verify.templ`, `oauth_authorize.templ`, `invite.templ`, `not_found.templ`, `forbidden.templ`.

- [ ] **Step 1: Replace each page's outer wrapper**

For each page, swap the existing `<main>` / card markup for an `AuthCard` invocation with the variant from the mockup. The form body inside the page goes inside the `{ ... }` children block.

| Page             | Variant            | Eyebrow                  | Title                     | Notes                                       |
|------------------|--------------------|--------------------------|---------------------------|---------------------------------------------|
| login            | Logo               | "Cloudzilla"             | "Sign in to continue"     | Body: email/password form + Google button   |
| register         | Logo               | "Cloudzilla"             | "Create your account"     | Body: signup form                           |
| invite           | Logo               | "You've been invited"    | "Join Cloudzilla"         | Body: invite form with locked email field   |
| totp_verify      | Icon (lock SVG)    | "Verify identity"        | "Two-factor authentication" | Subtitle: "Enter the 6-digit code..."     |
| oauth_authorize  | Plain              | "Authorization"          | "Authorize <app>"         | Body: permission `<ul>` + Authorize/Deny    |
| not_found        | Numeral ("404")    | "404 · Not found"        | "Page not found"          | Body: dual buttons (Go home / Report)       |
| forbidden        | Numeral ("403")    | "403 · Forbidden"        | "Access denied"           | Body: dual buttons (Go home / Sign in)      |

- [ ] **Step 2: Port the full 404 / 403 markup**

The current `not_found.templ` and `forbidden.templ` render only a single `<a>` linking home. The mockup (see `mockups/auth.html`, sections starting at lines 110 and 180) has: eyebrow text, a giant mono numeral (rendered by the `Numeral` variant), heading, paragraph, and two action buttons (primary + secondary). Port the full markup:

```go
// not_found.templ
@components.AuthCard(components.AuthCardProps{
    Variant: components.AuthVariantNumeral,
    Numeral: "404",
    Eyebrow: "404 · Not found",
    Title:   "Page not found",
    Subtitle: "This page doesn't exist or has been moved. Check the URL or jump back to your dashboard.",
}) {
    <div class="mt-5 flex items-center justify-center gap-2">
        <a href="/" class="h-9 px-4 rounded-md font-medium text-sm inline-flex items-center bg-foreground text-background">Go home</a>
        <a href="/issues/new" class="h-9 px-4 border border-border rounded-md text-sm text-muted-foreground inline-flex items-center hover:bg-accent">Report broken link</a>
    </div>
}

// forbidden.templ — same shape, numeral "403", "Sign in" as secondary action.
```

- [ ] **Step 3: Regenerate, verify, commit**

```bash
~/go/bin/templ generate && make dev
git add internal/view/pages/login.templ internal/view/pages/register.templ internal/view/pages/totp_verify.templ internal/view/pages/oauth_authorize.templ internal/view/pages/invite.templ internal/view/pages/not_found.templ internal/view/pages/forbidden.templ
git add internal/view/pages/*_templ.go
git commit -m "feat(ui): refactor auth + 404 + 403 pages onto AuthCard shell"
```

---

### Task 6: Consolidate account-settings page

**Architecture decision — anchor-scrolled single page (matches mockup):**

`mockups/account_settings.html` renders ALL settings sections in one DOM (Profile, Security, SSH keys, Access tokens, Notifications, Danger zone) with `<a href="#section">` sidebar links and a sticky sidebar that uses Alpine.js scroll-spy to highlight the active section. Do **not** implement a `?section=` server-switched variant — it would double the templ branches and breaks the mockup's "scroll to learn" UX.

**Naming decision:** keep the existing `pages.Settings(...)` templ function name (and `PageSettings` handler). Only the body changes. This avoids touching the `pageNames` slice in `router.go`, the handler registry, or any callers — minimal disruption.

**Out-of-scope (deferred to a future phase):** `mockups/account_settings.html` shows two extra sidebar entries — `Sessions` and `Emails` — that would each require a brand-new store + service (`SessionService` for active-session listing/revocation, `SecondaryEmailService` for verified secondary emails). Those balloon scope well beyond a UI consolidation. Drop both from this phase and document them as follow-up work; the sidebar therefore has 5 sections + Danger zone (six total entries), not the mockup's eight.

Final sidebar entries for this phase: **Profile / Security / SSH keys / Access tokens / Notifications / Danger zone (Delete account)**.

**Files:**
- Modify: `internal/view/pages/settings.templ` (replace body)
- Delete: `internal/view/pages/security.templ`, `tokens.templ`, `notification_settings.templ` (and their `_templ.go` siblings)
- Modify: `internal/router/router.go` (collapse routes; keep POST endpoints in place)
- Modify: `internal/handler/page_handler.go` (extend `PageSettings` to fetch all section data)
- Modify: `internal/view/view.go` (extend `AccountSettingsData` viewmodel)

- [ ] **Step 1: Combine handler**

```go
func (h *Handler) PageSettings(w http.ResponseWriter, r *http.Request) {
    claims := authClaims(r)
    if claims == nil { http.Redirect(w, r, "/login", http.StatusFound); return }
    ctx := r.Context()
    uid := claims.UserID

    // Fetch every section's data up-front; the page renders the lot.
    sshKeys, _ := h.Services.SSHKey.List(ctx, uid)
    tokens, _  := h.Services.AccessToken.List(ctx, uid)
    notifPrefs, _ := h.Services.User.GetNotificationPrefs(ctx, uid)
    totpEnabled, _ := h.Services.User.IsTOTPEnabled(ctx, uid)

    data := view.AccountSettingsData{
        BasePage:     h.basePage(r),
        SSHKeys:      sshKeys,
        Tokens:       tokens,
        NotifPrefs:   notifPrefs,
        TOTPEnabled:  totpEnabled,
    }
    h.render(w, r, pages.Settings(data), "Account settings")
}
```

- [ ] **Step 2: Update routes — GET consolidation only, keep POSTs**

Only GET handlers for the deprecated section pages move. The existing POST endpoints (`POST /settings/notifications`, `POST /settings/security/setup`, `POST /api/user/tokens/...`, `POST /api/user/totp/...`, SSH key CRUD, etc. — see `internal/router/router.go` lines 63–67, 390–392, 406, 416–419) continue to serve the form submissions and HTMX fragments from inside the consolidated page. Migrating those POSTs is out of scope and unnecessary.

```go
r.With(authMW).Get("/settings", h.PageSettings)
// Legacy GETs 301 to the anchor on the consolidated page:
r.Get("/settings/security",      redirectTo("/settings#security"))
r.Get("/settings/tokens",        redirectTo("/settings#tokens"))
r.Get("/settings/notifications", redirectTo("/settings#notifications"))

func redirectTo(target string) http.HandlerFunc {
    return func(w http.ResponseWriter, r *http.Request) {
        http.Redirect(w, r, target, http.StatusMovedPermanently)
    }
}
```

Remove the legacy GET handler registrations (`PageSecuritySettings`, `PageTokens`, `PageNotificationSettings`) but keep the handler functions for now if any POST routes reuse helpers — delete only what's clearly orphaned.

- [ ] **Step 3: Body — anchor sections + Alpine scroll-spy**

Render every section as `<section id="profile">...</section>` etc., all in the DOM. The sticky sidebar uses Alpine to track which section is in view:

```html
<div x-data="scrollSpy(['profile','security','ssh-keys','tokens','notifications','delete'])" class="grid grid-cols-12 gap-8">
  <aside class="col-span-3">
    <nav class="sticky top-4 flex flex-col gap-0.5">
      <a href="#profile"       :class="active==='profile'       && 'nav-item-active'" class="nav-item">Profile</a>
      <a href="#security"      :class="active==='security'      && 'nav-item-active'" class="nav-item">Security</a>
      <a href="#ssh-keys"      :class="active==='ssh-keys'      && 'nav-item-active'" class="nav-item">SSH keys</a>
      <a href="#tokens"        :class="active==='tokens'        && 'nav-item-active'" class="nav-item">Access tokens</a>
      <a href="#notifications" :class="active==='notifications' && 'nav-item-active'" class="nav-item">Notifications</a>
      <p class="mono text-[10px] fg3 uppercase tracking-wider mt-4 mb-1 px-2.5">Danger zone</p>
      <a href="#delete"        :class="active==='delete'        && 'nav-item-active'" class="nav-item text-destructive">Delete account</a>
    </nav>
  </aside>
  <div class="col-span-9 space-y-10">
    <section id="profile">...</section>
    <section id="security">...</section>     <!-- lifted from security.templ -->
    <section id="ssh-keys">...</section>     <!-- lifted from settings.templ (SSH keys form) -->
    <section id="tokens">...</section>       <!-- lifted from tokens.templ -->
    <section id="notifications">...</section><!-- lifted from notification_settings.templ -->
    <section id="delete">...</section>
  </div>
</div>
```

`scrollSpy()` is a tiny Alpine helper in `frontend/static/alpine-scrollspy.js` that uses `IntersectionObserver` to set the active id.

- [ ] **Step 4: Commit**

```bash
~/go/bin/templ generate && make dev
git add internal/view/pages/settings.templ internal/view/pages/settings_templ.go internal/handler/ internal/router/router.go internal/view/view.go
git rm internal/view/pages/security.templ internal/view/pages/security_templ.go \
       internal/view/pages/tokens.templ internal/view/pages/tokens_templ.go \
       internal/view/pages/notification_settings.templ internal/view/pages/notification_settings_templ.go
git commit -m "feat(ui): consolidate account settings into single anchor-scrolled page"
```

---

### Task 7: Add `/docs` and `/changelog` page templates

**Files:**
- Create: `internal/view/pages/docs.templ`
- Create: `internal/view/pages/changelog.templ`
- Modify: `internal/router/router.go` `pageNames` slice

- [ ] **Step 1: Docs template**

```go
// internal/view/pages/docs.templ
package pages

import (
    "github.com/mkappworks-dev/cloudzilla-app/internal/service"
    "github.com/mkappworks-dev/cloudzilla-app/internal/view"
    "github.com/mkappworks-dev/cloudzilla-app/internal/view/components"
    "github.com/mkappworks-dev/cloudzilla-app/internal/view/layout"
)

templ Docs(data view.DocsPageData) {
    @layout.Base(data.BasePage, "Docs · " + data.Current.Title) {
        <div class="max-w-[1280px] mx-auto px-6 pt-10 pb-20 grid grid-cols-[220px_1fr_220px] gap-10">
            <aside aria-label="Documentation sections" class="sticky top-4 self-start">
                for _, g := range data.Groups {
                    <p class="font-mono text-[10px] text-muted-foreground uppercase tracking-wider mb-2 px-2">{ g.Name }</p>
                    <nav class="flex flex-col gap-0.5 mb-6">
                        for _, p := range g.Pages {
                            if p.Slug == data.Current.Slug {
                                <a href={ templ.SafeURL("/docs/" + p.Slug) } aria-current="page" class="doc-nav-item doc-nav-item-active">{ p.Title }</a>
                            } else {
                                <a href={ templ.SafeURL("/docs/" + p.Slug) } class="doc-nav-item">{ p.Title }</a>
                            }
                        }
                    </nav>
                }
            </aside>
            <main class="prose max-w-none min-w-0">
                if data.Current.Category != "" {
                    <p class="font-mono text-[11px] text-muted-foreground uppercase tracking-wider mb-2">Docs · { data.Current.Category }</p>
                }
                @components.MarkdownBody(data.Current.HTML)
            </main>
            <aside class="sticky top-4 self-start text-[12.5px] text-muted-foreground">
                <p class="font-mono text-[10px] uppercase tracking-wider mb-2">On this page</p>
                @components.WikiTOC(data.Current.TOC)
            </aside>
        </div>
    }
}
```

- [ ] **Step 2: Changelog template**

```go
// internal/view/pages/changelog.templ
package pages

templ Changelog(data view.ChangelogData) {
    @layout.Base(data.BasePage, "Changelog") {
        <div class="max-w-3xl mx-auto px-6 py-8">
            <h1 class="text-3xl font-semibold tracking-tight mb-2">Changelog</h1>
            <p class="text-muted-foreground mb-8">Release notes for Cloudzilla.</p>
            <ol class="relative border-l border-border space-y-8 ml-2">
                for _, e := range data.Entries {
                    <li class="ml-6">
                        <span class="absolute -left-1.5 mt-1.5 w-3 h-3 bg-primary rounded-full"></span>
                        <div class="flex items-baseline gap-3">
                            <h2 class="text-xl font-semibold tracking-tight">{ e.Version }</h2>
                            <time class="text-xs font-mono text-muted-foreground">{ e.Date.Format("Jan 2, 2006") }</time>
                        </div>
                        <div class="mt-2 space-y-3">
                            for _, cat := range e.Categories {
                                <section>
                                    @categoryHeading(cat.Kind)
                                    <ul class="mt-1 space-y-0.5 text-sm">
                                        for _, item := range cat.Items {
                                            <li class="text-muted-foreground">{ item }</li>
                                        }
                                    </ul>
                                </section>
                            }
                        </div>
                    </li>
                }
            </ol>
        </div>
    }
}

templ categoryHeading(kind string) {
    switch kind {
    case "feat":
        <h3 class="text-xs font-mono text-success uppercase tracking-wider">Features</h3>
    case "fix":
        <h3 class="text-xs font-mono text-warning uppercase tracking-wider">Bug fixes</h3>
    case "breaking":
        <h3 class="text-xs font-mono text-destructive uppercase tracking-wider">Breaking</h3>
    default:
        <h3 class="text-xs font-mono text-muted-foreground uppercase tracking-wider">Other</h3>
    }
}
```

- [ ] **Step 3: Wire view types**

```go
// internal/view/view.go
type DocsPageData struct {
    BasePage BasePage
    Groups   []service.DocGroup // sidebar groups: "Getting started", "Reference", "Operate"
    Current  *service.DocPage
}

type ChangelogData struct {
    BasePage BasePage
    Entries  []service.ChangelogEntry
}
```

- [ ] **Step 4: Add "Docs" and "Changelog" to pageNames slice**

In `internal/router/router.go`.

- [ ] **Step 5: Commit**

```bash
~/go/bin/templ generate && make dev
# Visit /docs (redirects to first page) and /changelog
git add internal/view/pages/docs.templ internal/view/pages/changelog.templ internal/view/pages/docs_templ.go internal/view/pages/changelog_templ.go internal/router/router.go internal/view/view.go
git commit -m "feat(ui): add Docs and Changelog page templates"
```

---

### Task 8: Home page — Keyboard shortcuts sidebar

Port the `Shortcuts` section from [mockups/home.html](../../../mockups/home.html) lines 591–618 into the home page right-rail aside. This was deferred from Phase 1.

**Scope:** Display-only reference card. Wiring real hotkey handlers (`G I`, `G P`, `N R`, etc.) is out of scope — leave a `// TODO: wire shortcuts` note where the future Alpine/JS handler will attach.

- [ ] **Step 1:** Add a `Shortcuts` view-model in `internal/handler/viewmodels.go` (slice of `{Label string; Keys []string}`) populated by a static helper — no service/store layer needed.

- [ ] **Step 2:** Add a `homeShortcuts` templ component in `internal/view/pages/home.templ` rendering the `<section aria-labelledby="shortcuts-heading">` block from the mockup. Use `<kbd>` elements with the existing `elevated b border rounded` classes already used by the mockup.

- [ ] **Step 3:** Insert the new component as the final child of the home page right-rail aside, below the existing Templates sidebar. Verify the Phase 1 "Needs attention" / "Recent activity" / "Templates" order is unchanged.

- [ ] **Step 4:** Regenerate templ (`~/go/bin/templ generate`), run `go test ./...`, visual sweep in both themes.

- [ ] **Step 5:** Commit.

```bash
git add internal/handler/viewmodels.go internal/view/pages/home.templ internal/view/pages/home_templ.go
git commit -m "feat(ui): add keyboard shortcuts sidebar to home page"
```

---

### Task 9: PR detail — Notifications subscribe toggle

Port the `Notifications` section from [mockups/pr.html](../../../mockups/pr.html) lines 661–668 into the PR detail right-rail aside. This was deferred from Phase 1.

**Scope:** UI toggle + HTMX endpoint that flips a subscription row. The notification dispatch system already exists ([docs/notifications.md](../../notifications.md)) — this task only adds explicit per-PR subscribe state.

- [ ] **Step 1:** Migration. Add `pull_subscriptions(user_id BIGINT, pull_id BIGINT, PRIMARY KEY(user_id, pull_id), created_at TIMESTAMPTZ NOT NULL DEFAULT NOW())` in `migrations/` (next sequential number).

- [ ] **Step 2:** Store. Add `PullSubscriptionStore` with `IsSubscribed(ctx, userID, pullID) (bool, error)`, `Subscribe(ctx, ...)`, `Unsubscribe(ctx, ...)`. Wire into `Stores` struct.

- [ ] **Step 3:** Service. Add `PullService.ToggleSubscription(ctx, userID, pullID) (subscribed bool, err error)`. Wire into `Services`.

- [ ] **Step 4:** Handler. Add `POST /{owner}/{repo}/pulls/{number}/subscribe` returning a re-rendered button fragment. Use HTMX `hx-post` + `hx-swap="outerHTML"`.

- [ ] **Step 5:** Templ. Add `pullSubscribeButton` fragment in `internal/view/fragments/` rendering the `<button aria-pressed="...">` from the mockup with conditional label ("Subscribed" / "Subscribe") and bell icon. Insert below the Linked Issues section in `pull_detail.templ`, above the Phase 2 `TODO` anchor.

- [ ] **Step 6:** View-model. Extend `PullDetailViewModel` with `IsSubscribed bool`; populate in `page_handler.go`. Auto-subscribe the PR author and any reviewer when their record is first created (handled in service, not handler).

- [ ] **Step 7:** Wire the dispatch path in `internal/service/notification_service.go` to short-circuit when no subscription row exists for the recipient (respecting the existing `actorID == authorID` rule).

- [ ] **Step 8:** Regenerate templ, run migrations, `go test ./...`, visual sweep.

- [ ] **Step 9:** Commit.

```bash
git add migrations/ internal/store/ internal/service/ internal/handler/ internal/view/ internal/router/router.go
git commit -m "feat(pr): add per-PR notification subscribe toggle"
```

---

### Task 10: Verify and open PR

- [ ] Tests + lint + templ regen + visual sweep across account-settings / docs / changelog / auth pages / home (shortcuts) / PR detail (subscribe) in both themes. silent-failure-hunter.

- [ ] **Open PR:**

```bash
git push -u origin feat/ui-overhaul-phase-10-global
gh pr create --title "feat(ui): UI overhaul phase 10 — settings & global" --body "$(cat <<'EOF'
## Summary
- New /docs route with embedded markdown rendering and on-this-page TOC.
- New /changelog route parsed from CHANGELOG.md at startup.
- Account settings consolidated into single sidebar-nav page (Profile / Security / SSH / PATs / Notifications / Danger).
- Auth pages (login, register, totp, oauth, invite, 404, 403) refactored onto a shared AuthCard shell.
- Home page: keyboard shortcuts reference sidebar (deferred from Phase 1).
- PR detail: per-PR notification subscribe toggle wired to a new pull_subscriptions table (deferred from Phase 1).

Spec: docs/superpowers/specs/2026-05-14-ui-overhaul-design.md
Plan: docs/superpowers/plans/2026-05-14-ui-overhaul-phase-10-global.md

## Test plan
- [x] `go test ./...` passes
- [x] DocsService + ChangelogService unit tests pass
- [x] PullSubscriptionStore + PullService.ToggleSubscription unit tests pass
- [x] Visual: settings / docs / changelog / auth pages / home shortcuts sidebar / PR subscribe button in both themes
- [x] Legacy /settings/security etc. redirect to /settings?section=...
- [x] Subscribe toggle round-trips via HTMX and dispatch respects subscription rows
- [x] silent-failure-hunter clean

🤖 Generated with [Claude Code](https://claude.com/claude-code)
EOF
)"
```

---

## Self-review checklist

- [ ] DocsService loads from embed.FS (`embedded_docs`, staged via Makefile) at startup; no disk reads per request.
- [ ] Only `docs/public/**` is embedded — internal architecture docs (`superpowers/`, `roadmap.md`, etc.) are NOT shipped.
- [ ] Docs sidebar renders grouped by category ("Getting started" / "Reference" / "Operate"), not as a flat list.
- [ ] Changelog parser handles release-please format (`## [X.Y.Z](url) (YYYY-MM-DD)`) AND legacy fallback; real `CHANGELOG.md` fixture test passes (>=1 entry parsed, "Bug Fixes" maps to `fix`).
- [ ] CHANGELOG.md embedded via Makefile `stage-embeds` step (no `..` paths in `//go:embed`).
- [ ] All references use `markdown.Render(...)` (not the non-existent `renderMarkdown`).
- [ ] `ExtractWikiTOC` is reused from Phase 6 wiki work — Phase 6 must be merged before this phase.
- [ ] Account settings is anchor-scrolled (all sections in DOM, Alpine scroll-spy), not `?section=`-switched.
- [ ] Account settings sidebar has 5 sections + Danger zone; Sessions and Emails are deferred and documented as follow-up.
- [ ] Legacy `/settings/security|tokens|notifications` GETs 301 to `/settings#anchor`; existing POST endpoints stay at their current paths.
- [ ] AuthCard uses Templ children (`{ children... }`), supports all four variants (Logo / Icon / Numeral / Plain), and ports the full mockup markup for 404/403 (eyebrow + giant numeral + dual buttons), not just a "Go home" link.
- [ ] No orphan files: `security.templ`, `tokens.templ`, `notification_settings.templ` and their `_templ.go` siblings deleted.
- [ ] `pages.Settings(...)` function name preserved; `pageNames` slice in `router.go` only adds "docs" and "changelog".
- [ ] Home shortcuts sidebar is display-only — no hotkey JS wired up; `// TODO: wire shortcuts` comment marks the future attach point.
- [ ] PR subscribe button is HTMX-driven (`hx-post` + `hx-swap="outerHTML"`), not a full page reload.
- [ ] `pull_subscriptions` migration uses `BIGINT` ids, `TIMESTAMPTZ NOT NULL DEFAULT NOW()`, and composite PK — follows the PostgreSQL conventions in CLAUDE.md.
- [ ] PR author + reviewers auto-subscribe; notification dispatch short-circuits when no subscription row exists (preserving the `actorID == authorID` rule).
