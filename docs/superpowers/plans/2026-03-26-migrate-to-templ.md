# Migrate html/template → Templ Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace Go's `html/template` with [Templ](https://templ.guide) for all server-rendered HTML, gaining compile-time type safety, IDE completion, and clean component composition — with zero behaviour changes.

**Architecture:** Viewmodels move from `internal/handler/viewmodels.go` to a new `internal/view` package (breaking the circular import that would arise if `view/pages` imported `handler`). `markdown.Render` return type changes from `template.HTML` to `string`; call sites are unchanged. Templates become Go-compiled components organised in `internal/view/layout/`, `internal/view/pages/`, and `internal/view/fragments/`. The `Handler` struct loses its `Pages` and `Frags` fields; `render()` now accepts a `templ.Component`. `embed.go` drops template embedding; only static files remain embedded.

**Tech Stack:** Go 1.26, PostgreSQL, chi router, **Templ v0.3.x**, HTMX, Tailwind CSS

---

## Templ Syntax Quick-Reference (read before Task 5)

| html/template | Templ equivalent |
|---|---|
| `{{.Field}}` | `{ data.Field }` |
| `{{if .Cond}}…{{end}}` | `if data.Cond { … }` |
| `{{if eq .X "y"}}` | `if data.X == "y" { … }` |
| `{{range .Items}}…{{end}}` | `for _, item := range data.Items { … }` |
| `{{.CreatedAt.Format "Jan 2, 2006"}}` | `{ data.CreatedAt.Format("Jan 2, 2006") }` |
| `href="/{{.Owner}}/{{.Repo}}"` | `href={ "/"+data.Owner+"/"+data.Repo }` |
| `{{.BodyHTML}}` (pre-rendered markdown) | `@templ.Raw(data.BodyHTML)` |
| `{{emojiChar .Emoji}}` | `{ view.EmojiChar(data.Emoji) }` |
| `{{add .A .B}}` | `{ strconv.Itoa(data.A + data.B) }` |
| `{{percent .Part .Total}}` | `{ strconv.Itoa(view.Percent(data.Part, data.Total)) }` |
| Conditional CSS classes (multiline) | `class={ templ.KV("cls-a", cond), templ.KV("cls-b", !cond) }` |
| `<input>` void elements | `<input/>` (must self-close) |
| `{{define "fragment-X"}}` | `templ X(data view.XData) { … }` in `fragments` package |

---

## File Map

| File | Action | Purpose |
|---|---|---|
| `go.mod` | Edit | Add `github.com/a-h/templ` |
| `Makefile` | Edit | Add `setup-templ` + `generate-templ`; wire into `dev` and `build` |
| `tailwind/tailwind.config.js` | Edit | Add `./internal/view/**/*.templ` to content paths |
| `cmd/server/embed.go` | Edit | Embed only static files, not templates |
| `internal/view/viewmodels.go` | Create | Move all view-model structs (previously `handler/viewmodels.go`) |
| `internal/view/helpers.go` | Create | `EmojiChar`, `Percent` helper funcs used in templates |
| `internal/view/layout/layout.templ` | Create | Base HTML shell (replaces `layout.html`) |
| `internal/view/pages/*.templ` | Create (31 files) | One component per page |
| `internal/view/fragments/*.templ` | Create (26 files) | One component per fragment |
| `internal/handler/viewmodels.go` | Delete | Replaced by `internal/view/viewmodels.go` |
| `internal/handler/handler.go` | Edit | Remove template fields; new `render()` using templ |
| `internal/router/router.go` | Edit | Remove `mustParseTemplates`; slim `New()` |
| `internal/markdown/markdown.go` | Edit | Change return type `template.HTML` → `string` |
| `internal/handler/*.go` (callers) | Edit | Update `h.render` / `h.renderFragment` calls + import `view/pages`, `view/fragments` |
| `cmd/server/frontend/templates/` | Delete | All `.html` files removed after migration |

---

### Task 1: Add Templ Dependency and Update Tooling

**Files:**
- Edit: `go.mod`
- Edit: `Makefile`
- Edit: `tailwind/tailwind.config.js`
- Edit: `.gitignore`

- [ ] **Step 1: Install the templ CLI**

```bash
go install github.com/a-h/templ/cmd/templ@latest
```

Expected: `templ` binary available on `$PATH`. Verify with `templ version`.

- [ ] **Step 2: Add templ to go.mod**

```bash
go get github.com/a-h/templ@latest
```

Expected: `go.mod` and `go.sum` updated with `github.com/a-h/templ vX.Y.Z`.

- [ ] **Step 3: Add `setup-templ` and `generate-templ` targets to Makefile**

In `Makefile`, add after the `setup-tailwind` target:

```makefile
setup-templ:                       ## Install the templ CLI
	go install github.com/a-h/templ/cmd/templ@latest

generate-templ:                    ## Generate *_templ.go from *.templ files
	templ generate ./internal/view/...
```

- [ ] **Step 4: Wire `generate-templ` into `dev` and `build` in Makefile**

Replace the existing `dev` and `build` lines:

```makefile
dev: download-mermaid build-css generate-templ  ## Run backend + Tailwind + templ watch
	@(trap 'kill 0' SIGINT; \
		$(GO) run ./cmd/server/. & \
		$(TAILWIND) -c tailwind/tailwind.config.js -i tailwind/input.css \
		  -o $(TAILWIND_OUT) --watch & \
		templ generate --watch ./internal/view/... & \
		wait)

build: download-mermaid build-css generate-templ build-backend build-cli  ## Full build
```

- [ ] **Step 5: Update Tailwind content paths to include `.templ` files**

In `tailwind/tailwind.config.js`, update the `content` array:

```js
content: [
  "./cmd/server/frontend/templates/**/*.html",
  "./internal/view/**/*.templ",
],
```

- [ ] **Step 6: Ensure generated `_templ.go` files are committed (not gitignored)**

Check `.gitignore` — there must be no `*_templ.go` entry. Generated files are committed so the binary builds without requiring the templ CLI at deploy time. No change needed if absent.

- [ ] **Step 7: Commit**

```bash
git add go.mod go.sum Makefile tailwind/tailwind.config.js
git commit -m "chore(templ): add templ dependency and tooling"
```

---

### Task 2: Move Viewmodels + Fix Markdown Return Type

**Files:**
- Create: `internal/view/viewmodels.go`
- Edit: `internal/markdown/markdown.go`
- Delete: `internal/handler/viewmodels.go`
- Edit: all handler files that use `template.HTML` type

- [ ] **Step 1: Create `internal/view/viewmodels.go`**

Copy the full contents of `internal/handler/viewmodels.go`, change the package declaration from `package handler` to `package view`, and change every `template.HTML` field to `string`. Remove the `"html/template"` import. The full file:

```go
package view

import (
	"github.com/mkappworks/cloudzilla/internal/middleware"
	"github.com/mkappworks/cloudzilla/internal/model"
	"github.com/mkappworks/cloudzilla/internal/service"
)

// RenderedComment wraps a model.Comment with its body pre-rendered as HTML.
type RenderedComment struct {
	model.Comment
	BodyHTML string
}

// BasePage contains common data for all pages
type BasePage struct {
	CurrentUser      *middleware.Claims
	UnreadNotifCount int
	AllowLogin       bool
}

type HomeData struct {
	BasePage
	Repos []model.Repository
}

type LoginData struct {
	BasePage
	Error string
}

type UserData struct {
	BasePage
	User  model.User
	Repos []model.Repository
}

type OrgData struct {
	BasePage
	Org       model.Organization
	Repos     []model.Repository
	Members   []model.OrgMember
	CanManage bool
}

type OrgSettingsData struct {
	BasePage
	Org     model.Organization
	Members []model.OrgMember
}

type OrgMembersFragData struct {
	OrgName   string
	Members   []model.OrgMember
	CanManage bool
}

type RepoData struct {
	BasePage
	Repo          model.Repository
	Owner         string
	RepoName      string
	CloneHTTP     string
	CloneSSH      string
	CanWrite      bool
	ReadmeHTML    string
	StarCount     int
	IsStarred     bool
	ForkCount     int
	IsFork        bool
	ForkOfPath    string
	LatestRelease *model.Release
}

type ReleasesData struct {
	BasePage
	Repo     model.Repository
	Owner    string
	RepoName string
	Releases []model.Release
	CanWrite bool
}

type ReleaseDetailData struct {
	BasePage
	Repo     model.Repository
	Owner    string
	RepoName string
	Release  model.Release
	BodyHTML string
	CanWrite bool
}

type RepoSettingsData struct {
	BasePage
	Repo              model.Repository
	Owner             string
	RepoName          string
	Webhooks          []model.Webhook
	Collabs           []model.Permission
	Labels            []model.Label
	DeployKeys        []model.DeployKey
	BranchProtections []*model.BranchProtection
	CanManage         bool
	CanTransfer       bool
}

type BranchProtectionsFragData struct {
	Owner     string
	RepoName  string
	RepoID    int64
	Rules     []*model.BranchProtection
	CanManage bool
}
```

Then open the current `internal/handler/viewmodels.go` and copy the remaining structs not listed above (all the issue, PR, milestone, code, search, notification, admin structs). Add them all to `internal/view/viewmodels.go`, replacing every `template.HTML` field type with `string` and removing any `"html/template"` import reference.

- [ ] **Step 2: Run `go build ./...` — expect compilation errors pointing at handler files using `template.HTML`**

```bash
go build ./...
```

Expected: errors like `cannot use template.HTML value as string` in handler files. These are fixed in the next steps.

- [ ] **Step 3: Update `internal/markdown/markdown.go` return type**

Change the function signature from `func Render(src string) template.HTML` to `func Render(src string) string`, and the return statements:

```go
// Render converts markdown src to safe HTML.
func Render(src string) string {
	var buf bytes.Buffer
	md := goldmark.New(
		goldmark.WithExtensions(extension.GFM),
		goldmark.WithParserOptions(parser.WithAutoHeadingID()),
		goldmark.WithRendererOptions(
			ghtml.WithHardWraps(),
			renderer.WithNodeRenderers(
				util.Prioritized(&mermaidRenderer{}, 1),
			),
		),
	)
	if err := md.Convert([]byte(src), &buf); err != nil {
		return template.HTMLEscapeString(src)
	}
	return buf.String()
}
```

Also remove `"html/template"` from imports and add `"html"` for `html.EscapeString`:

```go
import (
	"bytes"
	"html"
	// ... rest unchanged
)

// In error return:
return html.EscapeString(src)
```

- [ ] **Step 4: Delete `internal/handler/viewmodels.go`**

```bash
rm internal/handler/viewmodels.go
```

- [ ] **Step 5: Update all handler files that reference `handler.RenderedComment`, `handler.BasePage`, etc.**

Each handler file that currently uses these types needs to import `view` instead. Find all affected files:

```bash
grep -rl "handler\.BasePage\|handler\.RenderedComment\|template\.HTML" internal/handler/
```

In each affected file, add the import `"github.com/mkappworks/cloudzilla/internal/view"` and change `handler.X` references to `view.X`. Also change any local `RenderedComment{..., BodyHTML: markdown.Render(...)}` — the assignment is already `string` now, no cast needed.

The `basePage()` helper function in `page_handler.go` returns `BasePage` — update its return type to `view.BasePage`:

```go
func basePage(r *http.Request, svcs *service.Services) view.BasePage {
	// ... same body
	return view.BasePage{ ... }
}
```

- [ ] **Step 6: Run `go build ./...` — must pass with zero errors**

```bash
go build ./...
```

Expected: clean build. Fix any remaining `template.HTML` references.

- [ ] **Step 7: Commit**

```bash
git add internal/view/viewmodels.go internal/markdown/markdown.go internal/handler/
git commit -m "refactor(view): move viewmodels to internal/view, change BodyHTML to string"
```

---

### Task 3: Create View Helpers and Layout Component

**Files:**
- Create: `internal/view/helpers.go`
- Create: `internal/view/layout/layout.templ`

- [ ] **Step 1: Create `internal/view/helpers.go`**

```go
package view

// EmojiChar converts an emoji shortcode to its Unicode character.
func EmojiChar(emoji string) string {
	m := map[string]string{
		"+1": "👍", "-1": "👎", "laugh": "😄", "hooray": "🎉",
		"confused": "😕", "heart": "❤️", "rocket": "🚀", "eyes": "👀",
	}
	if ch, ok := m[emoji]; ok {
		return ch
	}
	return emoji
}

// Percent returns part*100/total, returning 0 if total is zero.
func Percent(part, total int) int {
	if total == 0 {
		return 0
	}
	return part * 100 / total
}
```

- [ ] **Step 2: Create `internal/view/layout/layout.templ`**

```go
package layout

import "github.com/mkappworks/cloudzilla/internal/view"

templ Base(base view.BasePage, title string) {
	<!DOCTYPE html>
	<html lang="en">
		<head>
			<meta charset="UTF-8"/>
			<meta name="viewport" content="width=device-width, initial-scale=1.0"/>
			<title>{ title } — Cloudzilla</title>
			<link rel="stylesheet" href="/static/main.css"/>
		</head>
		<body class="bg-gray-50 text-gray-900 text-sm font-sans min-h-screen flex flex-col">
			<header class="bg-gray-900 text-white h-12 flex items-center px-4 gap-6 shadow-md">
				<a href="/" class="text-lg font-bold">☁ Cloudzilla</a>
				<nav class="flex gap-4 text-sm text-gray-300 flex-1 items-center">
					<a href="/" class="hover:text-white">Explore</a>
					<form action="/search" method="GET" class="flex items-center">
						<input type="text" name="q" placeholder="Search..." value="" class="bg-gray-800 text-gray-200 placeholder-gray-500 border border-gray-700 rounded px-2 py-1 text-xs focus:outline-none focus:ring-1 focus:ring-blue-500 w-40"/>
					</form>
					if base.CurrentUser != nil {
						<a href="/settings" class="hover:text-white">Settings</a>
						if base.CurrentUser.IsSuperadmin {
							<a href="/admin/settings" class="hover:text-white text-yellow-400">Admin</a>
						}
						<span class="text-gray-500">|</span>
						<span class="text-gray-400">{ base.CurrentUser.Username }</span>
						<a href="/notifications" class="relative text-gray-300 hover:text-white">
							🔔
							if base.UnreadNotifCount > 0 {
								<span class="absolute -top-1 -right-2 bg-red-500 text-white text-xs rounded-full px-1 leading-4">{ strconv.Itoa(base.UnreadNotifCount) }</span>
							}
						</a>
						<form method="POST" action="/api/auth/logout" class="inline">
							<button type="submit" class="hover:text-white">Sign out</button>
						</form>
					} else {
						if base.AllowLogin {
							<a href="/login" class="hover:text-white">Sign in</a>
						}
					}
				</nav>
			</header>
			<main class="max-w-5xl mx-auto my-6 px-4 flex-1 w-full">
				{ children... }
			</main>
			<footer class="text-center text-gray-500 py-4 text-xs border-t border-gray-200">
				<p>&copy; 2026 Cloudzilla. Built with Go, HTMX, and Tailwind.</p>
			</footer>
			<script src="/htmx.min.js" defer></script>
			<script src="/mermaid.min.js"></script>
			<script>
				mermaid.initialize({ startOnLoad: false, theme: 'default' });
				document.addEventListener('DOMContentLoaded', function () { mermaid.run(); });
				document.addEventListener('htmx:afterSwap', function () { mermaid.run(); });
			</script>
		</body>
	</html>
}
```

Add `"strconv"` to the import block (templ auto-adds the `templ` import; `strconv` must be explicit).

- [ ] **Step 3: Run `templ generate` and verify output**

```bash
templ generate ./internal/view/...
```

Expected: `internal/view/layout/layout_templ.go` created with no errors.

- [ ] **Step 4: Run `go build ./...`**

```bash
go build ./...
```

Expected: clean build (layout component not yet wired into handlers).

- [ ] **Step 5: Commit**

```bash
git add internal/view/helpers.go internal/view/layout/
git commit -m "feat(view): add view helpers and layout templ component"
```

---

### Task 4: Update Handler Infrastructure and Embed

**Files:**
- Edit: `internal/handler/handler.go`
- Edit: `internal/router/router.go`
- Edit: `cmd/server/embed.go`

- [ ] **Step 1: Rewrite `internal/handler/handler.go`**

```go
package handler

import (
	"encoding/json"
	"net/http"

	"github.com/a-h/templ"
	"github.com/mkappworks/cloudzilla/internal/config"
	"github.com/mkappworks/cloudzilla/internal/service"
)

type Handler struct {
	Services *service.Services
	Cfg      *config.Config
}

func New(services *service.Services, cfg *config.Config) *Handler {
	return &Handler{Services: services, Cfg: cfg}
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

func (h *Handler) render(w http.ResponseWriter, r *http.Request, component templ.Component) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := component.Render(r.Context(), w); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}
```

Note: `renderFragment` is gone — `render` handles both pages and fragments since all are `templ.Component`.

- [ ] **Step 2: Update `internal/router/router.go`**

Remove the `mustParseTemplates` function entirely. Update the `New` function signature and body to drop template parsing:

```go
func New(services *service.Services, cfg *config.Config, frontend fs.FS) http.Handler {
	r := chi.NewRouter()
	h := handler.New(services, cfg)          // ← no template args
	// ... rest of routes unchanged ...

	// Static file serving (templates no longer embedded)
	staticFS, _ := fs.Sub(frontend, "frontend")
	fileServer := http.FileServer(http.FS(staticFS))
	r.Get("/*", fileServer.ServeHTTP)

	return r
}
```

Remove the imports `"html/template"` from `router.go`. Keep `"io/fs"`.

- [ ] **Step 3: Update `cmd/server/embed.go` to exclude templates**

```go
package main

import "embed"

//go:embed all:frontend/static frontend/htmx.min.js
var frontendFS embed.FS
```

This keeps serving `/static/main.css`, `/static/mermaid.min.js`, and `/htmx.min.js` but stops embedding the `.html` templates (which are now compiled Go code).

- [ ] **Step 4: Run `go build ./...`**

```bash
go build ./...
```

Expected: compile errors in all page handler files because `h.render(w, "page", data)` no longer matches the new signature. This is expected — they are fixed in Tasks 5–13.

- [ ] **Step 5: Commit the infrastructure changes**

```bash
git add internal/handler/handler.go internal/router/router.go cmd/server/embed.go
git commit -m "refactor(handler): replace html/template with templ component rendering"
```

---

### Task 5: Convert Auth + Setup Pages

**Files:**
- Create: `internal/view/pages/login.templ`
- Create: `internal/view/pages/setup.templ`
- Create: `internal/view/pages/invite.templ`
- Edit: `internal/handler/page_handler.go` (render calls for these three pages)

These are the canonical pattern examples — all subsequent page tasks follow the same structure.

- [ ] **Step 1: Create `internal/view/pages/login.templ`**

```go
package pages

import (
	"github.com/mkappworks/cloudzilla/internal/view"
	"github.com/mkappworks/cloudzilla/internal/view/layout"
)

templ Login(data view.LoginData) {
	@layout.Base(data.BasePage, "Sign In") {
		<div class="max-w-md mx-auto mt-12">
			<div class="bg-white border border-gray-200 rounded-lg shadow-md p-8">
				<h1 class="text-2xl font-bold mb-6">Sign In to Cloudzilla</h1>
				if data.Error != "" {
					<div class="mb-4 p-3 bg-red-50 border border-red-200 rounded text-red-700 text-sm">
						{ data.Error }
					</div>
				}
				<form method="POST" action="/login" class="space-y-4">
					<div>
						<label for="email" class="block text-sm font-medium text-gray-700 mb-1">Email</label>
						<input type="email" id="email" name="email" required class="w-full px-3 py-2 border border-gray-300 rounded-md focus:outline-none focus:ring-2 focus:ring-blue-500"/>
					</div>
					<div>
						<label for="password" class="block text-sm font-medium text-gray-700 mb-1">Password</label>
						<input type="password" id="password" name="password" required class="w-full px-3 py-2 border border-gray-300 rounded-md focus:outline-none focus:ring-2 focus:ring-blue-500"/>
					</div>
					<button type="submit" class="w-full px-4 py-2 bg-blue-600 text-white rounded-md font-medium hover:bg-blue-700 transition-colors">
						Sign In
					</button>
				</form>
				<div class="mt-6">
					<div class="relative">
						<div class="absolute inset-0 flex items-center">
							<div class="w-full border-t border-gray-200"></div>
						</div>
						<div class="relative flex justify-center text-sm">
							<span class="bg-white px-3 text-gray-500">or</span>
						</div>
					</div>
					<div class="mt-4">
						<a href="/auth/google" class="flex w-full items-center justify-center gap-3 rounded-md border border-gray-300 bg-white px-4 py-2 text-sm font-medium text-gray-700 shadow-sm hover:bg-gray-50 transition-colors">
							<svg class="h-5 w-5" viewBox="0 0 24 24">
								<path fill="#4285F4" d="M22.56 12.25c0-.78-.07-1.53-.2-2.25H12v4.26h5.92c-.26 1.37-1.04 2.53-2.21 3.31v2.77h3.57c2.08-1.92 3.28-4.74 3.28-8.09z"></path>
								<path fill="#34A853" d="M12 23c2.97 0 5.46-.98 7.28-2.66l-3.57-2.77c-.98.66-2.23 1.06-3.71 1.06-2.86 0-5.29-1.93-6.16-4.53H2.18v2.84C3.99 20.53 7.7 23 12 23z"></path>
								<path fill="#FBBC05" d="M5.84 14.09c-.22-.66-.35-1.36-.35-2.09s.13-1.43.35-2.09V7.07H2.18C1.43 8.55 1 10.22 1 12s.43 3.45 1.18 4.93l2.85-2.22.81-.62z"></path>
								<path fill="#EA4335" d="M12 5.38c1.62 0 3.06.56 4.21 1.64l3.15-3.15C17.45 2.09 14.97 1 12 1 7.7 1 3.99 3.47 2.18 7.07l3.66 2.84c.87-2.6 3.3-4.53 6.16-4.53z"></path>
							</svg>
							Sign in with Google
						</a>
					</div>
				</div>
			</div>
		</div>
	}
}
```

- [ ] **Step 2: Create `internal/view/pages/setup.templ`**

Open `cmd/server/frontend/templates/pages/setup.html`. Create `internal/view/pages/setup.templ` with:
- Package: `package pages`
- Imports: `view`, `view/layout`
- Component: `templ Setup(data view.SetupData)`
- Wrap with `@layout.Base(data.BasePage, "Setup Cloudzilla")`
- Convert all `{{.Field}}` → `{ data.Field }`, `{{if}}` → `if`, `{{range}}` → `for range`
- No `template.HTML` fields in SetupData — straightforward conversion

- [ ] **Step 3: Create `internal/view/pages/invite.templ`**

Open `cmd/server/frontend/templates/pages/invite.html`. Create `internal/view/pages/invite.templ`:
- Component: `templ Invite(data view.InviteData)`
- Wrap with `@layout.Base(data.BasePage, "Accept Invitation")`
- Straightforward conversion — no special helpers needed

- [ ] **Step 4: Run `templ generate`**

```bash
templ generate ./internal/view/...
```

Expected: three `*_templ.go` files generated under `internal/view/pages/`.

- [ ] **Step 5: Update render calls in `internal/handler/page_handler.go` for these three pages**

Find the three handler functions (`PageLogin`, `PageSetup`, `PageInvite` etc.) and update their render calls. Add imports at top of file:

```go
import (
    // ... existing imports ...
    "github.com/mkappworks/cloudzilla/internal/view"
    "github.com/mkappworks/cloudzilla/internal/view/pages"
)
```

Replace:
```go
// Before:
h.render(w, "login", LoginData{...})
// After:
h.render(w, r, pages.Login(view.LoginData{...}))
```

Do the same for `PageLoginSubmit` error returns, `PageSetup`, `PageSetupSubmit`, `PageInvite`, `PageInviteSubmit`.

- [ ] **Step 6: Run `go build ./...`**

```bash
go build ./...
```

Expected: builds with remaining render-call errors only in handlers not yet updated.

- [ ] **Step 7: Commit**

```bash
git add internal/view/pages/login.templ internal/view/pages/setup.templ internal/view/pages/invite.templ internal/handler/page_handler.go
git commit -m "feat(view): convert login, setup, invite pages to templ"
```

---

### Task 6: Convert User, Org, and Profile Pages

**Files:**
- Create: `internal/view/pages/home.templ`
- Create: `internal/view/pages/user.templ`
- Create: `internal/view/pages/org.templ`
- Create: `internal/view/pages/org_settings.templ`
- Create: `internal/view/pages/notifications.templ`
- Create: `internal/view/pages/settings.templ`
- Create: `internal/view/pages/tokens.templ`
- Create: `internal/view/pages/search.templ`

- [ ] **Step 1: Create `internal/view/pages/home.templ`**

```go
package pages

import (
	"github.com/mkappworks/cloudzilla/internal/view"
	"github.com/mkappworks/cloudzilla/internal/view/layout"
)

templ Home(data view.HomeData) {
	@layout.Base(data.BasePage, "Explore Repositories") {
		<div class="space-y-6">
			<div class="flex justify-between items-center">
				<h1 class="text-3xl font-bold">Repositories</h1>
			</div>
			if len(data.Repos) > 0 {
				<div class="grid gap-4">
					for _, repo := range data.Repos {
						<a href={ templ.SafeURL("/" + repo.OwnerName + "/" + repo.Name) } class="block bg-white border border-gray-200 rounded-lg p-4 hover:border-blue-300 hover:shadow-md transition-all">
							<h2 class="text-xl font-semibold text-blue-600">{ repo.OwnerName }/{ repo.Name }</h2>
							if repo.Description != "" {
								<p class="text-gray-600 mt-1">{ repo.Description }</p>
							}
							<div class="mt-2 text-xs text-gray-500 flex gap-3">
								if repo.Private {
									<span>Private</span>
								} else {
									<span>Public</span>
								}
								<span>Updated { repo.UpdatedAt.Format("Jan 2, 2006") }</span>
							</div>
						</a>
					}
				</div>
			} else {
				<div class="bg-white border border-gray-200 rounded-lg p-8 text-center">
					<p class="text-gray-500">No repositories yet.</p>
				</div>
			}
		</div>
	}
}
```

- [ ] **Step 2: Create remaining 7 page templ files**

For each of `user`, `org`, `org_settings`, `notifications`, `settings`, `tokens`, `search`:

1. Open the corresponding `.html` file in `cmd/server/frontend/templates/pages/`
2. Create the `.templ` file in `internal/view/pages/`
3. Apply the syntax transformations from the quick-reference table at the top of this plan
4. Component signature: `templ X(data view.XData)` where `X` matches the page name in PascalCase
5. Wrap body with `@layout.Base(data.BasePage, "Page Title Here")`

**Key special cases for this batch:**
- `notifications.templ`: uses `{{range}}` over notifications — convert to `for _, n := range data.Notifications`
- `settings.templ`: HTMX attributes stay as-is on HTML elements; no special templ treatment needed
- `search.templ`: has a tab bar with active-state classes — use `templ.KV` for conditional classes:
  ```go
  class={ templ.KV("border-b-2 border-blue-600 text-blue-600", data.Type == "code"), templ.KV("text-gray-600", data.Type != "code") }
  ```

- [ ] **Step 3: Run `templ generate` and `go build ./...`**

```bash
templ generate ./internal/view/... && go build ./...
```

Expected: new `_templ.go` files; compile errors only for not-yet-updated render calls.

- [ ] **Step 4: Update render calls in `page_handler.go` for all 8 pages in this task**

Follow the same pattern as Task 5 Step 5 — replace each `h.render(w, "pagename", data)` with `h.render(w, r, pages.PageName(view.PageNameData{...}))`.

- [ ] **Step 5: Commit**

```bash
git add internal/view/pages/ internal/handler/page_handler.go
git commit -m "feat(view): convert home, user, org, settings, notifications, search pages to templ"
```

---

### Task 7: Convert Repo Overview Pages

**Files:**
- Create: `internal/view/pages/repo.templ`
- Create: `internal/view/pages/repo_settings.templ`
- Create: `internal/view/pages/stargazers.templ`
- Create: `internal/view/pages/user_stars.templ`
- Edit: `internal/handler/page_handler.go`

- [ ] **Step 1: Create `internal/view/pages/repo.templ`**

```go
package pages

import (
	"github.com/mkappworks/cloudzilla/internal/view"
	"github.com/mkappworks/cloudzilla/internal/view/layout"
)

templ Repo(data view.RepoData) {
	@layout.Base(data.BasePage, data.Owner+"/"+data.RepoName) {
		<div class="space-y-6">
			<div class="bg-white border border-gray-200 rounded-lg p-6">
				<div class="flex justify-between items-start mb-4">
					<div>
						<h1 class="text-3xl font-bold">
							<a href={ templ.SafeURL("/" + data.Owner) } class="text-blue-600 hover:underline">{ data.Owner }</a>/<span>{ data.RepoName }</span>
						</h1>
						if data.Repo.Description != "" {
							<p class="text-gray-600 mt-2">{ data.Repo.Description }</p>
						}
						if data.IsFork {
							<p class="text-sm text-gray-500 mt-1">Forked from
								<a href={ templ.SafeURL("/" + data.ForkOfPath) } class="text-blue-600 hover:underline">{ data.ForkOfPath }</a>
							</p>
						}
					</div>
					<div class="flex items-center gap-3">
						<span class="px-3 py-1 rounded-full text-xs font-medium bg-gray-100 text-gray-700">
							if data.Repo.Private {
								Private
							} else {
								Public
							}
						</span>
						<div id="fork-button" class="flex items-center gap-1">
							if data.BasePage.CurrentUser != nil {
								<button hx-post={ "/api/repos/" + data.Owner + "/" + data.RepoName + "/fork" } hx-target="#fork-button" hx-swap="outerHTML" class="inline-flex items-center gap-1 px-3 py-1.5 border border-gray-300 bg-white text-gray-700 rounded text-sm font-medium hover:bg-gray-50 transition-colors">
									&#x2442; Fork
								</button>
							} else {
								<a href="/login" class="inline-flex items-center gap-1 px-3 py-1.5 border border-gray-300 bg-white text-gray-700 rounded text-sm font-medium hover:bg-gray-50 transition-colors">
									&#x2442; Fork
								</a>
							}
							<span class="px-2 py-1.5 border border-gray-300 bg-white text-gray-700 rounded text-sm">
								{ strconv.Itoa(data.ForkCount) }
							</span>
						</div>
						<div id="star-button" class="flex items-center gap-1">
							if data.BasePage.CurrentUser != nil {
								if data.IsStarred {
									<button hx-delete={ "/api/repos/" + data.Owner + "/" + data.RepoName + "/star" } hx-target="#star-button" hx-swap="outerHTML" class="inline-flex items-center gap-1 px-3 py-1.5 border border-yellow-400 bg-yellow-50 text-yellow-700 rounded text-sm font-medium hover:bg-yellow-100 transition-colors">
										&#9733; Starred
									</button>
								} else {
									<button hx-post={ "/api/repos/" + data.Owner + "/" + data.RepoName + "/star" } hx-target="#star-button" hx-swap="outerHTML" class="inline-flex items-center gap-1 px-3 py-1.5 border border-gray-300 bg-white text-gray-700 rounded text-sm font-medium hover:bg-gray-50 transition-colors">
										&#9734; Star
									</button>
								}
							} else {
								<a href="/login" class="inline-flex items-center gap-1 px-3 py-1.5 border border-gray-300 bg-white text-gray-700 rounded text-sm font-medium hover:bg-gray-50 transition-colors">
									&#9734; Star
								</a>
							}
							<a href={ templ.SafeURL("/" + data.Owner + "/" + data.RepoName + "/stargazers") } class="px-2 py-1.5 border border-gray-300 bg-white text-gray-700 rounded text-sm hover:bg-gray-50 transition-colors">
								{ strconv.Itoa(data.StarCount) }
							</a>
						</div>
					</div>
				</div>
				<div class="text-sm text-gray-500">
					<p>Default branch: <code class="bg-gray-100 px-2 py-1 rounded">{ data.Repo.DefaultBranch }</code></p>
				</div>
			</div>
			<div class="bg-white border border-gray-200 rounded-lg p-6">
				<h3 class="font-semibold mb-3">Clone this repository</h3>
				<div class="space-y-3">
					<div>
						<label class="block text-xs font-medium text-gray-500 mb-1">HTTPS</label>
						<code class="block bg-gray-50 border border-gray-200 rounded px-3 py-2 text-sm font-mono">{ data.CloneHTTP }</code>
					</div>
					<div>
						<label class="block text-xs font-medium text-gray-500 mb-1">SSH</label>
						<code class="block bg-gray-50 border border-gray-200 rounded px-3 py-2 text-sm font-mono">{ data.CloneSSH }</code>
					</div>
				</div>
			</div>
			if data.ReadmeHTML != "" {
				<div class="bg-white border border-gray-200 rounded-lg p-6">
					<div class="prose prose-sm max-w-none">
						@templ.Raw(data.ReadmeHTML)
					</div>
				</div>
			}
			<div class="grid grid-cols-2 gap-4">
				<a href={ templ.SafeURL("/" + data.Owner + "/" + data.RepoName + "/issues") } class="block bg-white border border-gray-200 rounded-lg p-4 hover:border-blue-300 hover:shadow-md transition-all">
					<h3 class="text-lg font-semibold">Issues</h3>
					<p class="text-gray-500 text-sm mt-1">Track and discuss problems</p>
				</a>
				<a href={ templ.SafeURL("/" + data.Owner + "/" + data.RepoName + "/pulls") } class="block bg-white border border-gray-200 rounded-lg p-4 hover:border-blue-300 hover:shadow-md transition-all">
					<h3 class="text-lg font-semibold">Pull Requests</h3>
					<p class="text-gray-500 text-sm mt-1">Review and merge changes</p>
				</a>
				<a href={ templ.SafeURL("/" + data.Owner + "/" + data.RepoName + "/tree/" + data.Repo.DefaultBranch) } class="block bg-white border border-gray-200 rounded-lg p-4 hover:border-blue-300 hover:shadow-md transition-all">
					<h3 class="text-lg font-semibold">Browse Code</h3>
					<p class="text-gray-500 text-sm mt-1">Explore files and history</p>
				</a>
				<a href={ templ.SafeURL("/" + data.Owner + "/" + data.RepoName + "/commits/" + data.Repo.DefaultBranch) } class="block bg-white border border-gray-200 rounded-lg p-4 hover:border-blue-300 hover:shadow-md transition-all">
					<h3 class="text-lg font-semibold">Commits</h3>
					<p class="text-gray-500 text-sm mt-1">Browse commit history</p>
				</a>
				<a href={ templ.SafeURL("/" + data.Owner + "/" + data.RepoName + "/milestones") } class="block bg-white border border-gray-200 rounded-lg p-4 hover:border-blue-300 hover:shadow-md transition-all">
					<h3 class="text-lg font-semibold">Milestones</h3>
					<p class="text-gray-500 text-sm mt-1">Track progress with sprints</p>
				</a>
				<a href={ templ.SafeURL("/" + data.Owner + "/" + data.RepoName + "/releases") } class="block bg-white border border-gray-200 rounded-lg p-4 hover:border-blue-300 hover:shadow-md transition-all">
					<h3 class="text-lg font-semibold">Releases</h3>
					if data.LatestRelease != nil {
						<p class="text-gray-500 text-sm mt-1">Latest: <span class="font-mono text-blue-600">{ data.LatestRelease.TagName }</span></p>
					} else {
						<p class="text-gray-500 text-sm mt-1">No releases yet</p>
					}
				</a>
				<a href={ templ.SafeURL("/" + data.Owner + "/" + data.RepoName + "/refs") } class="block bg-white border border-gray-200 rounded-lg p-4 hover:border-blue-300 hover:shadow-md transition-all">
					<h3 class="text-lg font-semibold">Branches &amp; Tags</h3>
					<p class="text-gray-500 text-sm mt-1">Manage refs and create branches</p>
				</a>
				if data.CanWrite {
					<a href={ templ.SafeURL("/" + data.Owner + "/" + data.RepoName + "/settings") } class="block bg-white border border-gray-200 rounded-lg p-4 hover:border-blue-300 hover:shadow-md transition-all">
						<h3 class="text-lg font-semibold">Settings</h3>
						<p class="text-gray-500 text-sm mt-1">Manage webhooks and repo settings</p>
					</a>
				}
			</div>
		</div>
	}
}
```

Add `"strconv"` to imports.

- [ ] **Step 2: Create `internal/view/pages/repo_settings.templ`**

Open `cmd/server/frontend/templates/pages/repo_settings.html` (341 lines). Create `internal/view/pages/repo_settings.templ`:
- Component: `templ RepoSettings(data view.RepoSettingsData)`
- Title: `data.Owner + "/" + data.RepoName + " — Settings"`
- Uses `@templ.Raw` nowhere (no markdown)
- HTMX fragment targets like `#repo-collaborators`, `#webhooks-list` remain as HTML attributes
- Conditional danger zone section: `if data.CanTransfer { ... }`

- [ ] **Step 3: Create `internal/view/pages/stargazers.templ` and `user_stars.templ`**

Both are short list pages. Apply standard conversions:
- `stargazers.templ`: `templ Stargazers(data view.StargazersData)`, title `"Stargazers — owner/repo"`
- `user_stars.templ`: `templ UserStars(data view.UserStarsData)`, title `"Stars — username"`

- [ ] **Step 4: Run `templ generate && go build ./...`**

```bash
templ generate ./internal/view/... && go build ./...
```

- [ ] **Step 5: Update render calls in `page_handler.go` for the 4 pages in this task**

- [ ] **Step 6: Commit**

```bash
git add internal/view/pages/ internal/handler/page_handler.go
git commit -m "feat(view): convert repo, repo_settings, stargazers, user_stars pages to templ"
```

---

### Task 8: Convert Code Browser Pages

**Files:**
- Create: `internal/view/pages/tree.templ`
- Create: `internal/view/pages/blob.templ`
- Create: `internal/view/pages/blame.templ`
- Create: `internal/view/pages/commits.templ`
- Create: `internal/view/pages/commit.templ`
- Create: `internal/view/pages/refs.templ`
- Edit: `internal/handler/page_handler.go` (code browser render calls)
- Edit: `internal/handler/code_handler.go` or similar (if render calls live there)

- [ ] **Step 1: Create all 6 code browser templ files**

For each page, open the corresponding `.html` file and create the `.templ` equivalent. Special notes:

**`tree.templ`** — `templ Tree(data view.TreeData)`, title `"owner/repo — tree/ref"`:
- Iterates over directory entries; no markdown
- Uses `templ.SafeURL` for file/dir links

**`blob.templ`** — `templ Blob(data view.BlobData)`, title `"path — owner/repo"`:
- Shows syntax-highlighted code in `<pre><code>` — the code is plain text, no `templ.Raw` needed unless `BlobHTML` is pre-rendered; check `view.BlobData` struct
- If `BlobData.ContentHTML` is `string` (pre-rendered via goldmark): use `@templ.Raw(data.ContentHTML)`

**`blame.templ`** — `templ Blame(data view.BlameData)`:
- Table of blame lines; straightforward loop

**`commits.templ`** — `templ Commits(data view.CommitsData)`:
- List of commit rows with author, SHA, message; loop with `for _, c := range data.Commits`

**`commit.templ`** — `templ Commit(data view.CommitData)`, title `"SHA — owner/repo"`:
- Diff table; use `@templ.Raw` if diff HTML is pre-rendered, otherwise plain `{ data.Field }`

**`refs.templ`** — `templ Refs(data view.RefsData)`:
- Branch list + tag list with HTMX delete buttons; straightforward

- [ ] **Step 2: Run `templ generate && go build ./...`**

```bash
templ generate ./internal/view/... && go build ./...
```

- [ ] **Step 3: Update all code browser render calls in the relevant handler files**

- [ ] **Step 4: Commit**

```bash
git add internal/view/pages/ internal/handler/
git commit -m "feat(view): convert code browser pages (tree, blob, blame, commits, commit, refs) to templ"
```

---

### Task 9: Convert Issue and Pull Request Pages

**Files:**
- Create: `internal/view/pages/issues.templ`
- Create: `internal/view/pages/issue_detail.templ`
- Create: `internal/view/pages/issue_new.templ`
- Create: `internal/view/pages/pulls.templ`
- Create: `internal/view/pages/pull_detail.templ`
- Create: `internal/view/pages/pull_new.templ`
- Edit: relevant handler files for render calls

- [ ] **Step 1: Create all 6 issue/PR templ files**

For each page, open the `.html` file and convert. Special notes per file:

**`issue_detail.templ`** — `templ IssueDetail(data view.IssueDetailData)`:
- `data.Issue.BodyHTML` → `@templ.Raw(data.BodyHTML)` (pre-rendered markdown)
- Comment list with `@templ.Raw(c.BodyHTML)` for each rendered comment body
- Conditional state badge: use `templ.KV`:
  ```go
  class={ "px-3 py-1 rounded-full text-sm font-medium", templ.KV("bg-green-100 text-green-700", data.Issue.State == "open"), templ.KV("bg-red-100 text-red-700", data.Issue.State == "closed") }
  ```
- HTMX close/reopen buttons remain as HTML attributes

**`pull_detail.templ`** — `templ PullDetail(data view.PullDetailData)`:
- This is the most complex page (353 lines). Convert methodically section by section
- `@templ.Raw(data.BodyHTML)` for PR description
- `@templ.Raw(c.BodyHTML)` for each comment
- Diff table: if diff content is plain text lines, use `{ line }` not `@templ.Raw`; if it's pre-rendered HTML use `@templ.Raw`
- Auto-merge section: conditional based on `data.AutoMergeEnabled`

**`issues.templ`**, **`pulls.templ`** — simple list pages with filter links; straightforward conversion

**`issue_new.templ`**, **`pull_new.templ`** — forms with template body pre-fill; no markdown rendering

- [ ] **Step 2: Run `templ generate && go build ./...`**

```bash
templ generate ./internal/view/... && go build ./...
```

- [ ] **Step 3: Update render calls in the relevant handler files**

- [ ] **Step 4: Commit**

```bash
git add internal/view/pages/ internal/handler/
git commit -m "feat(view): convert issue and pull request pages to templ"
```

---

### Task 10: Convert Remaining Pages

**Files:**
- Create: `internal/view/pages/milestones.templ`
- Create: `internal/view/pages/releases.templ`
- Create: `internal/view/pages/release_detail.templ`
- Create: `internal/view/pages/admin_settings.templ`
- Edit: relevant handler files for render calls

- [ ] **Step 1: Create the 4 remaining page templ files**

**`milestones.templ`** — `templ Milestones(data view.MilestonesData)`:
- Progress bars use `view.Percent(data.Closed, data.Total)` helper
- Pattern: `style={ "width: " + strconv.Itoa(view.Percent(m.ClosedCount, m.IssueCount)) + "%" }`

**`releases.templ`** — `templ Releases(data view.ReleasesData)`:
- List of releases; no markdown

**`release_detail.templ`** — `templ ReleaseDetail(data view.ReleaseDetailData)`:
- `@templ.Raw(data.BodyHTML)` for release notes (markdown)

**`admin_settings.templ`** — `templ AdminSettings(data view.AdminSettingsData)`:
- Site settings form + invitation management; HTMX fragment targets

- [ ] **Step 2: Run `templ generate && go build ./...`**

```bash
templ generate ./internal/view/... && go build ./...
```

- [ ] **Step 3: Update render calls**

- [ ] **Step 4: Commit**

```bash
git add internal/view/pages/ internal/handler/
git commit -m "feat(view): convert milestones, releases, admin_settings pages to templ"
```

---

### Task 11: Convert Comment and Reaction Fragments

**Files:**
- Create: `internal/view/fragments/comment.templ`
- Create: `internal/view/fragments/comments.templ`
- Create: `internal/view/fragments/reactions.templ`
- Create: `internal/view/fragments/pr_reviews.templ`
- Create: `internal/view/fragments/line_comments.templ`
- Edit: handler files that call `h.renderFragment` for these fragments

Fragments do **not** use the layout component — they return bare HTML snippets.

- [ ] **Step 1: Create `internal/view/fragments/comment.templ`**

```go
package fragments

import "github.com/mkappworks/cloudzilla/internal/view"

templ Comment(data view.CommentFragData) {
	<div class="bg-white border border-gray-200 rounded-lg p-4">
		<div class="font-semibold text-sm text-gray-700">{ data.Comment.AuthorName }</div>
		<div class="text-xs text-gray-500 mb-2">{ data.Comment.CreatedAt.Format("Jan 2, 2006 at 3:04 PM") }</div>
		<div class="prose prose-sm max-w-none text-gray-800">
			@templ.Raw(data.BodyHTML)
		</div>
	</div>
}
```

- [ ] **Step 2: Create `internal/view/fragments/reactions.templ`**

```go
package fragments

import (
	"github.com/mkappworks/cloudzilla/internal/view"
)

templ Reactions(data view.ReactionsFragData) {
	<div id={ "reactions-" + strconv.FormatInt(data.CommentID, 10) } class="flex flex-wrap items-center gap-1 mt-2">
		for _, r := range data.Reactions {
			<button
				hx-post={ "/api/repos/" + data.Owner + "/" + data.RepoName + "/comments/" + strconv.FormatInt(data.CommentID, 10) + "/reactions" }
				hx-vals={ `{"emoji":"` + r.Emoji + `"}` }
				hx-target={ "#reactions-" + strconv.FormatInt(data.CommentID, 10) }
				hx-swap="outerHTML"
				class={ "inline-flex items-center gap-1 px-2 py-0.5 rounded-full text-sm border transition-colors hover:border-blue-400", templ.KV("bg-blue-50 border-blue-400 text-blue-700", r.UserReacted), templ.KV("bg-white border-gray-300 text-gray-600", !r.UserReacted) }>
				{ view.EmojiChar(r.Emoji) } <span class="text-xs font-medium">{ strconv.Itoa(r.Count) }</span>
			</button>
		}
		if data.LoggedIn {
			<details class="relative">
				<summary class="list-none inline-flex items-center justify-center w-7 h-7 rounded-full border border-gray-200 text-gray-400 hover:border-blue-400 hover:text-blue-500 cursor-pointer text-sm transition-colors">+</summary>
				<div class="absolute z-10 left-0 top-8 bg-white border border-gray-200 rounded-lg shadow-md p-2 flex gap-1 flex-wrap">
					for _, emoji := range []string{"+1", "-1", "laugh", "hooray", "confused", "heart", "rocket", "eyes"} {
						<button
							hx-post={ "/api/repos/" + data.Owner + "/" + data.RepoName + "/comments/" + strconv.FormatInt(data.CommentID, 10) + "/reactions" }
							hx-vals={ `{"emoji":"` + emoji + `"}` }
							hx-target={ "#reactions-" + strconv.FormatInt(data.CommentID, 10) }
							hx-swap="outerHTML"
							class="text-lg hover:scale-125 transition-transform leading-none">
							{ view.EmojiChar(emoji) }
						</button>
					}
				</div>
			</details>
		}
	</div>
}
```

Add `"strconv"` to imports.

- [ ] **Step 3: Create `internal/view/fragments/comments.templ`**

Open `cmd/server/frontend/templates/fragments/comments.html`. Create the templ equivalent:
- Component: `templ Comments(data view.CommentsFragData)`
- Uses `@templ.Raw(c.BodyHTML)` for each rendered comment body
- HTMX edit/delete buttons remain as HTML attributes

- [ ] **Step 4: Create `internal/view/fragments/pr_reviews.templ`**

Open `cmd/server/frontend/templates/fragments/pr_reviews.html`. Create:
- Component: `templ PRReviews(data view.PRReviewsFragData)`
- Loop over reviews; use `templ.KV` for approval/rejection state classes

- [ ] **Step 5: Create `internal/view/fragments/line_comments.templ`**

Open `cmd/server/frontend/templates/fragments/line_comments.html`. Create:
- Component: `templ LineComments(data view.LineCommentsFragData)`
- The one `onclick="this.closest('form').remove()"` stays as a plain HTML attribute

- [ ] **Step 6: Run `templ generate && go build ./...`**

```bash
templ generate ./internal/view/... && go build ./...
```

- [ ] **Step 7: Update `h.renderFragment` calls in handler files for all 5 fragments**

In each handler file, replace:
```go
h.renderFragment(w, "fragment-comment", data)
h.renderFragment(w, "fragment-reactions", data)
// etc.
```
with:
```go
h.render(w, r, fragments.Comment(view.CommentFragData{...}))
h.render(w, r, fragments.Reactions(view.ReactionsFragData{...}))
// etc.
```

Add imports `"github.com/mkappworks/cloudzilla/internal/view/fragments"`.

- [ ] **Step 8: Commit**

```bash
git add internal/view/fragments/ internal/handler/
git commit -m "feat(view): convert comment and reaction fragments to templ"
```

---

### Task 12: Convert Sidebar and Button Fragments

**Files:**
- Create: `internal/view/fragments/issue_assignees.templ`
- Create: `internal/view/fragments/issue_labels.templ`
- Create: `internal/view/fragments/issue_detail.templ`
- Create: `internal/view/fragments/pull_assignees.templ`
- Create: `internal/view/fragments/pull_labels.templ`
- Create: `internal/view/fragments/pull_detail.templ`
- Create: `internal/view/fragments/fork_button.templ`
- Create: `internal/view/fragments/star_button.templ`
- Create: `internal/view/fragments/milestone_sidebar.templ`
- Edit: relevant handler files for `h.renderFragment` calls

- [ ] **Step 1: Create all 9 fragment templ files**

For each, open the corresponding `.html` file in `fragments/` and convert. Key notes:

**`star_button.templ`** — `templ StarButton(data view.StarButtonFragData)`:
- Conditional star/unstar button; use `templ.KV` for starred state classes
- HTMX post/delete stay as attributes

**`fork_button.templ`** — `templ ForkButton(data view.ForkButtonFragData)`:
- Straightforward; shows fork count

**`issue_assignees.templ`**, **`pull_assignees.templ`** — `templ IssueAssignees(data view.AssigneesFragData)`:
- List of assignee avatars/names with HTMX delete; add form with HTMX post

**`issue_labels.templ`**, **`pull_labels.templ`** — `templ IssueLabels(data view.LabelsFragData)`:
- Color-styled label pills; HTMX add/remove

**`milestone_sidebar.templ`** — `templ MilestoneSidebar(data view.MilestoneSidebarFragData)`:
- Milestone select with HTMX post; progress bar uses `view.Percent`

**`issue_detail.templ`** (fragment, not page) — `templ IssueDetailFrag(data view.IssueDetailFragData)`:
- Used by HTMX swap after edit; contains `@templ.Raw(data.BodyHTML)`

**`pull_detail.templ`** (fragment) — `templ PullDetailFrag(data view.PullDetailFragData)`:
- HTMX swap target for PR detail updates; uses `@templ.Raw`

- [ ] **Step 2: Run `templ generate && go build ./...`**

```bash
templ generate ./internal/view/... && go build ./...
```

- [ ] **Step 3: Update `h.renderFragment` calls for all 9 fragments**

- [ ] **Step 4: Commit**

```bash
git add internal/view/fragments/ internal/handler/
git commit -m "feat(view): convert sidebar and button fragments to templ"
```

---

### Task 13: Convert Settings and Admin Fragments

**Files:**
- Create: `internal/view/fragments/ssh_keys.templ`
- Create: `internal/view/fragments/tokens.templ`
- Create: `internal/view/fragments/deploy_keys.templ`
- Create: `internal/view/fragments/webhooks.templ`
- Create: `internal/view/fragments/repo_collaborators.templ`
- Create: `internal/view/fragments/repo_labels.templ`
- Create: `internal/view/fragments/refs.templ`
- Create: `internal/view/fragments/org_members.templ`
- Create: `internal/view/fragments/branch_protections.templ`
- Create: `internal/view/fragments/notifications.templ`
- Create: `internal/view/fragments/admin_settings.templ`
- Create: `internal/view/fragments/admin_invitations.templ`
- Edit: all handler files with remaining `h.renderFragment` calls

- [ ] **Step 1: Create all 12 fragment templ files**

For each, open the corresponding `.html` file and convert. All follow the established pattern. Key notes:

**`branch_protections.templ`** — `templ BranchProtections(data view.BranchProtectionsFragData)`:
- Table of rules; HTMX edit/delete; straightforward

**`repo_collaborators.templ`** — `templ RepoCollaborators(data view.RepoCollaboratorsFragData)`:
- ID `#repo-collaborators`; list with role badges; HTMX add/remove

**`refs.templ`** (fragment) — `templ RefsFrag(data view.RefsFragData)`:
- Branch + tag table rows; HTMX delete with confirm

All others are straightforward table/list fragments with HTMX CRUD — no markdown, no special helpers.

- [ ] **Step 2: Run `templ generate && go build ./...`**

```bash
templ generate ./internal/view/... && go build ./...
```

Expected: **zero errors**. All `h.render` and `h.renderFragment` calls should now be updated. If errors remain, grep for remaining `h.renderFragment` and `h.render(w, "` patterns:

```bash
grep -rn 'h\.renderFragment\|h\.render(w, "' internal/handler/
```

Fix any remaining occurrences.

- [ ] **Step 3: Update remaining `h.renderFragment` calls**

- [ ] **Step 4: Run the server and manually verify a few pages load correctly**

```bash
make dev
```

Open in browser: `/`, `/login`, a repo page, an issue detail page, the settings page. Check that:
- Pages render without errors
- HTMX fragments swap correctly (test adding a comment, toggling a star)
- Mermaid diagrams render if present

- [ ] **Step 5: Commit**

```bash
git add internal/view/fragments/ internal/handler/
git commit -m "feat(view): convert all remaining settings and admin fragments to templ"
```

---

### Task 14: Remove Old Templates and Final Cleanup

**Files:**
- Delete: `cmd/server/frontend/templates/` (all `.html` files)
- Edit: `tailwind/tailwind.config.js` (remove old html content path)
- Edit: `CLAUDE.md`

- [ ] **Step 1: Verify `go build ./...` is clean before deleting anything**

```bash
go build ./...
```

Must pass with zero errors.

- [ ] **Step 2: Verify no remaining references to old template system**

```bash
grep -rn 'html/template\|mustParseTemplates\|pageNames\|h\.render(w, "\|h\.renderFragment' internal/ cmd/ --include="*.go"
```

Expected: zero matches. Fix any that appear.

- [ ] **Step 3: Delete old HTML templates**

```bash
rm -rf cmd/server/frontend/templates/
```

- [ ] **Step 4: Remove the old html content path from Tailwind config**

In `tailwind/tailwind.config.js`, remove the `html` entry — keep only templ:

```js
content: [
  "./internal/view/**/*.templ",
],
```

- [ ] **Step 5: Run full build**

```bash
make build
```

Expected: clean binary in `dist/cloudzilla`.

- [ ] **Step 6: Update `CLAUDE.md`**

In the Tech Stack section, change `html/template` to `Templ`. In the Code Conventions section, update the Templates subsection to describe the templ component pattern. Remove the VS Code formatter warning about `{{if}}` inside class attributes (that problem no longer exists with templ). Update the "Adding a New Feature" checklist step 8 to reference `.templ` files.

- [ ] **Step 7: Run `go test ./...`**

```bash
go test ./...
```

Expected: all tests pass (no test files depend on the old template system).

- [ ] **Step 8: Final commit**

```bash
git add -A
git commit -m "feat(view): complete html/template → templ migration; remove old templates"
```

---

## Self-Review Checklist

- **Spec coverage:** Dependency + tooling ✓ | Viewmodel move ✓ | Markdown return type ✓ | Layout component ✓ | Handler infrastructure ✓ | All 31 pages ✓ | All 26 fragments ✓ | Embed update ✓ | Tailwind config ✓ | Old template removal ✓ | CLAUDE.md update ✓
- **Placeholder scan:** No TBDs — each task has exact file paths, component signatures, and code
- **Type consistency:** All render calls use `view.XData` types consistently; `BodyHTML` is `string` throughout; `view.EmojiChar` / `view.Percent` helper names match helpers.go

---

**Plan complete and saved to `docs/superpowers/plans/2026-03-26-migrate-to-templ.md`.**

**Two execution options:**

**1. Subagent-Driven (recommended)** — Dispatch a fresh subagent per task, review between tasks, fast iteration. Use `superpowers:subagent-driven-development`.

**2. Inline Execution** — Execute tasks in this session using `superpowers:executing-plans`, batch execution with checkpoints.

**Which approach?**
