# File tree remembers open folders — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Folders the viewer opens in the code browser's file tree stay open across page loads, off-path folders load their children when opened, and the header's dead burger and mislabelled chevron become a Collapse all button.

**Architecture:** A session cookie `cz_tree_open` (per repo) lists open folders. The server reads it and renders those folders open, and a small Alpine scope in `static/file_tree.js` keeps it in sync as folders toggle. A folder rendered closed fetches its children once from a new HTMX fragment route.

**Tech Stack:** Go, chi, templ v0.3, htmx 4.0.0, Alpine.js 3, Tailwind v4.

**Spec:** [`docs/superpowers/specs/2026-10-04-file-tree-remembers-folders-design.md`](../specs/2026-10-04-file-tree-remembers-folders-design.md)

## Global Constraints

- Cookie: name `cz_tree_open`, `Path=/{owner}/{repo}`, `SameSite=Lax`, session-only (no `max-age`/`expires`), value `encodeURIComponent(JSON.stringify(paths))`, at most 50 entries, newest last.
- Fragment route: `GET /fragments/{owner}/{repo}/tree/{ref}/*`, registered inside the existing `r.Route("/fragments", …)` group in `internal/router/router.go`.
- No user-controlled string (file or folder names, paths) may be spliced into an Alpine/JS expression. Paths reach JS only through `data-path` / `data-repo-path` attributes.
- Edit `.templ` files, then run `make generate-templ`. Never hand-edit `*_templ.go`, and commit the regenerated files with their sources.
- Tailwind v4 utilities only. Comments record only a *why* the code can't show; one line by default. Don't add narration comments ("Task 2:", "added for…").
- Work only in the worktree `/Users/mk/Downloads/app/Cloudzilla/cloudzilla-app/.worktrees/feat+blob-page-file-tree` (branch `feat/blob-page-file-tree`). Use literal absolute paths, run one `git` command per Bash call, and never put the word "git" inside a heredoc or `python3 -c` string (a guard refuses those).
- Stage files by explicit path (`git add <paths>`), never `git add -A` / `.`; never stage `.claude/`. Commit messages end with `Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>`. Don't push.
- Integration tests need `TEST_DATABASE_DSN='postgres://cloudzilla:test@localhost:5493/cloudzilla_test?sslmode=disable'` (the controller keeps that database running).

---

## File structure

```
cmd/server/frontend/static/
└── file_tree.js                        ← NEW (Task 3): Alpine fileTree scope, cookie sync
internal/
├── handler/
│   ├── file_tree_handler.go            ← NEW (Task 1): openTreeFolders, buildSidebarTree/Level (moved), fragment handler (Task 2)
│   ├── page_repo_handler.go            ← Task 1: drop moved funcs, pass openTreeFolders(r)
│   └── page_code_browser_test.go       ← Tasks 1–3: tests
├── router/router.go                    ← Task 2: fragment route
└── view/
    ├── components/
    │   ├── file_tree_sidebar.templ     ← Task 2: FileTreeItems, ChildrenURL, hx-*; Task 3: header, Path, Alpine wiring
    │   └── file_tree_sidebar_test.go   ← Task 3
    ├── fragments/file_tree_children.templ ← NEW (Task 2)
    └── pages/{tree,blob}.templ         ← Task 3: pass repo path
docs/code-browser.md                    ← Task 3
```

---

## Task 1: Server renders remembered folders open

**Files:**
- Create: `internal/handler/file_tree_handler.go`
- Modify: `internal/handler/page_repo_handler.go` (the two `buildSidebarTree` call sites at ~lines 542 and 620; delete `buildSidebarTree` and `buildSidebarLevel` at ~lines 787–838)
- Test: `internal/handler/page_code_browser_test.go`

**Interfaces:**
- Produces: `func openTreeFolders(r *http.Request) map[string]bool`; `func (h *Handler) buildSidebarTree(owner, repoName, ref, path string, open map[string]bool) []components.TreeNode`; `func (h *Handler) buildSidebarLevel(owner, repoName, ref, dirPath string, entries []service.TreeEntry, remainingPath []string, open map[string]bool) []components.TreeNode`; constants `treeOpenCookie = "cz_tree_open"`, `maxOpenTreeFolders = 50`.

- [ ] **Step 1: Extend the fixture and write the failing test**

In `internal/handler/page_code_browser_test.go`, add `"encoding/json"` and `"net/url"` to the imports. Change `seedCodeRepo`'s doc comment and file list:

```go
// seedCodeRepo commits README.md at the root and in lib/, so a sidebar that
// matched the active file by name would mark both, and lib/util/helper.js for
// a folder inside a folder.
```

```go
	for _, path := range []string{"README.md", "lib/README.md", "lib/config.js", "lib/util/helper.js"} {
```

Append:

```go
func openFoldersCookie(folders ...string) string {
	b, _ := json.Marshal(folders)
	return url.PathEscape(string(b))
}

func getWithOpenFolders(h http.Handler, path, cookie string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, path, nil)
	req.AddCookie(&http.Cookie{Name: "cz_tree_open", Value: cookie})
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	return rr
}

func TestFileTree_KeepsRememberedFoldersOpen(t *testing.T) {
	h, r, _ := seedCodeRepo(t)
	tests := []struct {
		name, cookie      string
		libOpen, utilOpen bool
	}{
		{"remembered folder", openFoldersCookie("lib"), true, false},
		{"remembered subfolder under a closed folder", openFoldersCookie("lib/util"), false, false},
		{"remembered folder and subfolder", openFoldersCookie("lib", "lib/util"), true, true},
		{"unescapable cookie", "%zz", false, false},
		{"cookie that isn't JSON", "not-json", false, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rr := getWithOpenFolders(h, r.path+"/blob/main/README.md", tt.cookie)
			if rr.Code != http.StatusOK {
				t.Fatalf("want 200, got %d: %s", rr.Code, rr.Body.String())
			}
			sidebar := fileTreeSidebar(t, rr.Body.String())
			if got := strings.Contains(sidebar, `/lib/config.js"`); got != tt.libOpen {
				t.Errorf("lib/ children listed = %v, want %v:\n%s", got, tt.libOpen, sidebar)
			}
			if got := strings.Contains(sidebar, `/lib/util/helper.js"`); got != tt.utilOpen {
				t.Errorf("lib/util/ children listed = %v, want %v:\n%s", got, tt.utilOpen, sidebar)
			}
		})
	}
}
```

- [ ] **Step 2: Run it and confirm it fails**

Run: `TEST_DATABASE_DSN='postgres://cloudzilla:test@localhost:5493/cloudzilla_test?sslmode=disable' go test -count=1 -run 'TestFileTree_|TestCodePages_|TestPageTree_' ./internal/handler/`
Expected: FAIL in `remembered folder` and `remembered folder and subfolder` ("lib/ children listed = false, want true"). The other cases and the existing `TestCodePages_KeepTheFileTree` / `TestPageTree_RedirectsAFileToItsBlobPage` pass.

- [ ] **Step 3: Implement**

Create `internal/handler/file_tree_handler.go`, moving `buildSidebarTree` and `buildSidebarLevel` out of `page_repo_handler.go` and adding the open set:

```go
package handler

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"net/url"
	"strings"

	"github.com/mkappworks-dev/cloudzilla-app/internal/service"
	"github.com/mkappworks-dev/cloudzilla-app/internal/view/components"
)

// Must match static/file_tree.js, which writes the cookie.
const (
	treeOpenCookie     = "cz_tree_open"
	maxOpenTreeFolders = 50
)

// openTreeFolders returns the folders the viewer left open in the file tree.
// The cookie is client-written, so anything unparseable counts as none.
func openTreeFolders(r *http.Request) map[string]bool {
	c, err := r.Cookie(treeOpenCookie)
	if err != nil {
		return nil
	}
	raw, err := url.PathUnescape(c.Value)
	if err != nil {
		return nil
	}
	var paths []string
	if err := json.Unmarshal([]byte(raw), &paths); err != nil {
		return nil
	}
	if len(paths) > maxOpenTreeFolders {
		paths = paths[:maxOpenTreeFolders]
	}
	open := make(map[string]bool, len(paths))
	for _, p := range paths {
		open[p] = true
	}
	return open
}

// buildSidebarTree returns the root tree with the folders along path and the
// open folders expanded and, when path is a file, that file marked active.
// Pass the requested ref, not a result's display Ref, which shortens a SHA
// past resolving.
func (h *Handler) buildSidebarTree(owner, repoName, ref, path string, open map[string]bool) []components.TreeNode {
	root, err := h.Services.Code.GetTree(owner, repoName, ref, "")
	if err != nil {
		slog.Warn("sidebar: root GetTree failed",
			"owner", owner, "repo", repoName, "ref", ref, "error", err)
		return nil
	}
	var segs []string
	if path != "" {
		segs = strings.Split(path, "/")
	}
	return h.buildSidebarLevel(owner, repoName, ref, "", root.Entries, segs, open)
}

// buildSidebarLevel expands the directory matching remainingPath[0] and every
// directory in open, recursively.
func (h *Handler) buildSidebarLevel(owner, repoName, ref, dirPath string, entries []service.TreeEntry, remainingPath []string, open map[string]bool) []components.TreeNode {
	nodes := make([]components.TreeNode, 0, len(entries))
	for _, e := range entries {
		var entryPath string
		if dirPath == "" {
			entryPath = e.Name
		} else {
			entryPath = dirPath + "/" + e.Name
		}
		kind := "tree"
		if !e.IsDir {
			kind = "blob"
		}
		href := "/" + owner + "/" + repoName + "/" + kind + "/" + ref + "/" + entryPath
		node := components.TreeNode{
			Name:     e.Name,
			IsDir:    e.IsDir,
			Href:     href,
			IsActive: !e.IsDir && len(remainingPath) == 1 && e.Name == remainingPath[0],
		}
		onPath := len(remainingPath) > 0 && e.Name == remainingPath[0]
		if e.IsDir && (onPath || open[entryPath]) {
			node.IsOpen = true
			var rest []string
			if onPath {
				rest = remainingPath[1:]
			}
			child, err := h.Services.Code.GetTree(owner, repoName, ref, entryPath)
			if err != nil {
				slog.Warn("sidebar: child GetTree failed",
					"owner", owner, "repo", repoName, "ref", ref, "path", entryPath, "error", err)
			} else {
				node.Children = h.buildSidebarLevel(owner, repoName, ref, entryPath, child.Entries, rest, open)
			}
		}
		nodes = append(nodes, node)
	}
	return nodes
}
```

In `page_repo_handler.go`, delete the old `buildSidebarTree` and `buildSidebarLevel` (including their doc comments), and change both call sites to:

```go
		Sidebar:      h.buildSidebarTree(owner, repoName, ref, result.Path, openTreeFolders(r)),
```

Then remove any import that `page_repo_handler.go` no longer uses (`go build` will name it). `strings` and `components` are probably still used elsewhere in the file, so check before deleting.

- [ ] **Step 4: Run the tests and confirm they pass**

Run: `go build ./... && TEST_DATABASE_DSN='postgres://cloudzilla:test@localhost:5493/cloudzilla_test?sslmode=disable' go test -count=1 -run 'TestFileTree_|TestCodePages_|TestPageTree_' ./internal/handler/`
Expected: PASS (all cases).

- [ ] **Step 5: Commit**

```bash
git add internal/handler/file_tree_handler.go internal/handler/page_repo_handler.go internal/handler/page_code_browser_test.go
git commit -m "feat(ui): render remembered file-tree folders open" -m "Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

## Task 2: Closed folders load their children from a fragment

**Files:**
- Modify: `internal/view/components/file_tree_sidebar.templ`
- Create: `internal/view/fragments/file_tree_children.templ`
- Modify: `internal/handler/file_tree_handler.go`
- Modify: `internal/router/router.go` (the `r.Route("/fragments", …)` block, ~line 506)
- Test: `internal/handler/page_code_browser_test.go`

**Interfaces:**
- Consumes: `buildSidebarLevel(owner, repoName, ref, dirPath string, entries []service.TreeEntry, remainingPath []string, open map[string]bool)` from Task 1.
- Produces: `components.TreeNode.ChildrenURL string`; `templ components.FileTreeItems(nodes []TreeNode, depth int)`; `templ fragments.FileTreeChildren(nodes []components.TreeNode, depth int)`; `func (h *Handler) FileTreeChildrenFragment(w http.ResponseWriter, r *http.Request)`.

- [ ] **Step 1: Write the failing tests**

Append to `internal/handler/page_code_browser_test.go`:

```go
func TestFileTree_ClosedFoldersLoadTheirChildren(t *testing.T) {
	h, r, _ := seedCodeRepo(t)
	rr := getAnonymous(h, r.path+"/tree/main/lib")
	if rr.Code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", rr.Code, rr.Body.String())
	}
	sidebar := fileTreeSidebar(t, rr.Body.String())
	if want := `hx-get="/fragments` + r.path + `/tree/main/lib/util"`; !strings.Contains(sidebar, want) {
		t.Errorf("closed folder lacks %q:\n%s", want, sidebar)
	}
	if strings.Contains(sidebar, `hx-get="/fragments`+r.path+`/tree/main/lib"`) {
		t.Errorf("open folder must not fetch the children it already has:\n%s", sidebar)
	}
}

func TestFileTreeChildrenFragment(t *testing.T) {
	h, r, _ := seedCodeRepo(t)

	rr := getAnonymous(h, "/fragments"+r.path+"/tree/main/lib")
	if rr.Code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", rr.Code, rr.Body.String())
	}
	body := rr.Body.String()
	for _, want := range []string{
		`href="` + r.path + `/blob/main/lib/config.js"`,
		`href="` + r.path + `/tree/main/lib/util"`,
		`hx-get="/fragments` + r.path + `/tree/main/lib/util"`,
		`padding-left: 20px`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("want %q in fragment:\n%s", want, body)
		}
	}
	if strings.Contains(body, "<aside") {
		t.Errorf("fragment must hold only the folder's items:\n%s", body)
	}

	for _, path := range []string{"/tree/main/README.md", "/tree/main/nope"} {
		if rr := getAnonymous(h, "/fragments"+r.path+path); rr.Code != http.StatusNotFound {
			t.Errorf("%s: want 404, got %d", path, rr.Code)
		}
	}
}
```

- [ ] **Step 2: Run them and confirm they fail**

Run: `TEST_DATABASE_DSN='postgres://cloudzilla:test@localhost:5493/cloudzilla_test?sslmode=disable' go test -count=1 -run 'TestFileTree' ./internal/handler/`
Expected: FAIL. `TestFileTree_ClosedFoldersLoadTheirChildren` reports "closed folder lacks…", and `TestFileTreeChildrenFragment` gets a non-200 (the route doesn't exist yet, so the request falls through to another handler).

- [ ] **Step 3: Implement the component changes**

In `internal/view/components/file_tree_sidebar.templ`:

1. Add `ChildrenURL string` to `TreeNode`, after `Href`.
2. Add an exported items template and use it at both levels:

```templ
templ FileTreeItems(nodes []TreeNode, depth int) {
	for _, n := range nodes {
		@treeItem(n, depth)
	}
}
```

Replace the root loop inside `<ol … role="tree">`:

```templ
			<ol class="overflow-y-auto py-1 flex-1 text-[12.5px]" role="tree">
				@FileTreeItems(nodes, 0)
			</ol>
```

and the child loop inside `<ol x-show="open" role="group">`:

```templ
				<ol x-show="open" role="group">
					@FileTreeItems(n.Children, depth+1)
				</ol>
```

3. On the folder toggle button, add the lazy load for folders rendered closed:

```templ
					<button
						type="button"
						x-on:click="open = !open"
						if !n.IsOpen {
							hx-get={ n.ChildrenURL }
							hx-trigger="click once"
							hx-target="next ol"
							hx-swap="innerHTML"
						}
						class="shrink-0 h-5 w-5 flex items-center justify-center text-muted-foreground/50"
						aria-label="Toggle folder"
					>
```

Create `internal/view/fragments/file_tree_children.templ`:

```templ
package fragments

import "github.com/mkappworks-dev/cloudzilla-app/internal/view/components"

templ FileTreeChildren(nodes []components.TreeNode, depth int) {
	@components.FileTreeItems(nodes, depth)
}
```

- [ ] **Step 4: Implement the handler and route**

In `internal/handler/file_tree_handler.go`, set the URL in `buildSidebarLevel` right after building `node`:

```go
		if e.IsDir {
			node.ChildrenURL = "/fragments/" + owner + "/" + repoName + "/tree/" + ref + "/" + entryPath
		}
```

and add the handler (add `"github.com/go-chi/chi/v5"` and `"github.com/mkappworks-dev/cloudzilla-app/internal/view/fragments"` to the imports):

```go
// FileTreeChildrenFragment renders a folder's entries for a file-tree folder
// the page rendered closed.
func (h *Handler) FileTreeChildrenFragment(w http.ResponseWriter, r *http.Request) {
	owner := chi.URLParam(r, "owner")
	repoName := chi.URLParam(r, "repo")
	ref := chi.URLParam(r, "ref")
	path := chi.URLParam(r, "*")
	if _, ok := h.readableRepoJSON(w, r, owner, repoName); !ok {
		return
	}
	if path == "" {
		writeError(w, http.StatusNotFound, "folder not found")
		return
	}
	dir, err := h.Services.Code.GetTree(owner, repoName, ref, path)
	if err != nil {
		writeError(w, http.StatusNotFound, "folder not found")
		return
	}
	nodes := h.buildSidebarLevel(owner, repoName, ref, path, dir.Entries, nil, nil)
	h.render(w, r, fragments.FileTreeChildren(nodes, strings.Count(path, "/")+1))
}
```

The repo check comes first on purpose: `repo_api_access_test.go` requires every `/fragments/{owner}/{repo}` route to answer a private or missing repo with the identical repo 404.

In `internal/router/router.go`, inside the fragments group:

```go
	r.Route("/fragments", func(r chi.Router) {
		r.Use(optAuthMW)
		r.Get("/{owner}/{repo}/issues/{number}/comments", h.IssueCommentsFragment)
		r.Get("/{owner}/{repo}/tree/{ref}/*", h.FileTreeChildrenFragment)
	})
```

- [ ] **Step 5: Regenerate and run the tests**

Run: `make generate-templ && go build ./... && TEST_DATABASE_DSN='postgres://cloudzilla:test@localhost:5493/cloudzilla_test?sslmode=disable' go test -count=1 ./internal/handler/ ./internal/view/... ./internal/router/`
Expected: PASS, including `TestRepoAPI_*_LooksLikeMissingRepo` (it now also covers the new route). If `padding-left: 20px` is missing, print the fragment body and check how templ renders the `style` attribute before changing the assertion.

- [ ] **Step 6: Commit**

```bash
git add internal/view/components/file_tree_sidebar.templ internal/view/components/file_tree_sidebar_templ.go internal/view/fragments/file_tree_children.templ internal/view/fragments/file_tree_children_templ.go internal/handler/file_tree_handler.go internal/router/router.go internal/handler/page_code_browser_test.go
git commit -m "feat(ui): load a closed file-tree folder's children on open" -m "Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

## Task 3: Client keeps the cookie in sync; header becomes Collapse all

**Files:**
- Create: `cmd/server/frontend/static/file_tree.js`
- Modify: `internal/view/components/file_tree_sidebar.templ`
- Modify: `internal/view/pages/tree.templ`, `internal/view/pages/blob.templ` (the `@components.FileTreeSidebar(data.Sidebar)` call in each)
- Modify: `internal/handler/file_tree_handler.go` (set `Path`)
- Modify: `docs/code-browser.md`
- Test: `internal/view/components/file_tree_sidebar_test.go`, `internal/handler/page_code_browser_test.go`

**Interfaces:**
- Consumes: `TreeNode.ChildrenURL`, `FileTreeItems` from Task 2.
- Produces: `TreeNode.Path string`; `templ FileTreeSidebar(repoPath string, nodes []TreeNode)`; Alpine component `fileTree` with `filter`, `remember(path)`, `forget(path)`, `collapseAll()`; window event `cz-tree-collapse-all`.

- [ ] **Step 1: Write the failing component tests**

Replace `internal/view/components/file_tree_sidebar_test.go` with:

```go
package components

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

func renderSidebar(t *testing.T, nodes []TreeNode) string {
	t.Helper()
	var buf bytes.Buffer
	if err := FileTreeSidebar("/o/r", nodes).Render(context.Background(), &buf); err != nil {
		t.Fatalf("render: %v", err)
	}
	return buf.String()
}

func TestFileTreeSidebar_RendersHierarchy(t *testing.T) {
	out := renderSidebar(t, []TreeNode{
		{Name: "internal", IsDir: true, Href: "/o/r/tree/main/internal", Path: "internal", ChildrenURL: "/fragments/o/r/tree/main/internal"},
		{Name: "main.go", IsDir: false, Href: "/o/r/blob/main/main.go", Path: "main.go", IsActive: true},
	})
	for _, want := range []string{
		"internal", "main.go",
		`aria-current="page"`,
		`data-path="internal"`,
		`x-on:cz-tree-collapse-all.window="open = false"`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("want %q in:\n%s", want, out)
		}
	}
}

func TestFileTreeSidebar_Header(t *testing.T) {
	out := renderSidebar(t, nil)
	for _, want := range []string{
		`/static/file_tree.js`,
		`x-data="fileTree"`,
		`data-repo-path="/o/r"`,
		`aria-label="Collapse all folders"`,
		`x-on:click="collapseAll()"`,
		`x-on:keydown.escape="filter = ''"`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("want %q in:\n%s", want, out)
		}
	}
	for _, gone := range []string{`aria-label="Clear filter"`, `M3 6h18M3 12h18M3 18h18`} {
		if strings.Contains(out, gone) {
			t.Errorf("want no %q in:\n%s", gone, out)
		}
	}
}

func TestFileTreeSidebar_PathsStayOutOfScript(t *testing.T) {
	evil := `x" x-init="alert(1)`
	out := renderSidebar(t, []TreeNode{{Name: evil, IsDir: true, Path: evil, Href: "/o/r/tree/main/x"}})
	if strings.Contains(out, `x-init="alert(1)"`) {
		t.Errorf("a folder name escaped its attribute:\n%s", out)
	}
}
```

- [ ] **Step 2: Run them and confirm they fail**

Run: `go test -count=1 ./internal/view/components/ -run TestFileTreeSidebar`
Expected: compile error: `too many arguments in call to FileTreeSidebar` / `unknown field Path`.

- [ ] **Step 3: Write `cmd/server/frontend/static/file_tree.js`**

```js
// Keeps the file tree's open folders in a cookie that the server reads to
// render them open on the next page (internal/handler/file_tree_handler.go).
document.addEventListener('alpine:init', () => {
  const COOKIE = 'cz_tree_open';
  const MAX_FOLDERS = 50; // the server ignores entries past maxOpenTreeFolders

  Alpine.data('fileTree', () => ({
    filter: '',
    openFolders: [],
    repoPath: '/',

    init() {
      this.repoPath = this.$el.dataset.repoPath;
      const match = document.cookie.match(/(?:^|; )cz_tree_open=([^;]*)/);
      try {
        const saved = match ? JSON.parse(decodeURIComponent(match[1])) : [];
        this.openFolders = Array.isArray(saved) ? saved.filter((p) => typeof p === 'string') : [];
      } catch {
        this.openFolders = [];
      }
    },

    remember(path) {
      this.openFolders = this.openFolders.filter((p) => p !== path).concat(path).slice(-MAX_FOLDERS);
      this.save();
    },

    forget(path) {
      this.openFolders = this.openFolders.filter((p) => p !== path && !p.startsWith(path + '/'));
      this.save();
    },

    collapseAll() {
      window.dispatchEvent(new CustomEvent('cz-tree-collapse-all'));
      this.openFolders = [];
      this.save();
    },

    save() {
      document.cookie = COOKIE + '=' + encodeURIComponent(JSON.stringify(this.openFolders)) +
        '; path=' + this.repoPath + '; SameSite=Lax';
    },
  }));
});
```

- [ ] **Step 4: Update the component**

In `internal/view/components/file_tree_sidebar.templ`:

1. Add `"github.com/mkappworks-dev/cloudzilla-app/internal/assets"` to the imports (alongside `strconv`), and add `Path string` to `TreeNode`, after `Href`.
2. Replace `FileTreeSidebar`'s signature, the opening `<aside>` tag and the `<header>` with the following. The script stays non-deferred so it registers `fileTree` before the deferred Alpine bundle starts:

```templ
templ FileTreeSidebar(repoPath string, nodes []TreeNode) {
	<script src={ assets.URL("/static/file_tree.js") }></script>
	<aside
		x-data="fileTree"
		data-repo-path={ repoPath }
		class="rounded-md border border-border bg-card sticky top-4 self-start max-h-[calc(100vh-5rem)] overflow-hidden flex flex-col"
		aria-label="Repository file tree"
	>
		<header class="px-3 py-2 border-b border-border flex items-center gap-2 bg-muted/40 shrink-0">
			<span class="text-[12px] font-medium">Files</span>
			<button
				type="button"
				x-on:click="collapseAll()"
				title="Collapse all folders"
				aria-label="Collapse all folders"
				class="ml-auto h-6 w-6 grid place-items-center rounded-sm hover:bg-muted text-muted-foreground"
			>
				<svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true" focusable="false">
					<path d="m7 20 5-5 5 5"></path>
					<path d="m7 4 5 5 5-5"></path>
				</svg>
			</button>
		</header>
```

3. On the filter `<input>`, add `x-on:keydown.escape="filter = ''"` after `x-model="filter"`.
4. Replace the folder wrapper `<div x-data={ "{ open: " + boolStr(n.IsOpen) + " }" }>` with:

```templ
			<div
				data-path={ n.Path }
				x-data={ "{ open: " + boolStr(n.IsOpen) + " }" }
				x-init="open && remember($el.dataset.path); $watch('open', o => o ? remember($el.dataset.path) : forget($el.dataset.path))"
				x-on:cz-tree-collapse-all.window="open = false"
			>
```

The `x-init` and listener strings are static. The path arrives only via `data-path`, which templ escapes, and must never be concatenated into these expressions.

5. In `pages/tree.templ` and `pages/blob.templ`, change the call to:

```templ
				@components.FileTreeSidebar("/"+data.Owner+"/"+data.RepoName, data.Sidebar)
```

6. In `internal/handler/file_tree_handler.go`'s `buildSidebarLevel`, add `Path: entryPath,` to the `components.TreeNode{…}` literal.

7. In `internal/handler/page_code_browser_test.go`, `TestCodePages_KeepTheFileTree` checks the old scope. Change:

```go
			if !strings.Contains(sidebar, `x-data="{ filter: '' }"`) {
				t.Errorf("sidebar lacks the filter's x-data scope:\n%s", sidebar)
			}
```

to:

```go
			if !strings.Contains(sidebar, `x-data="fileTree"`) {
				t.Errorf("sidebar lacks its fileTree scope:\n%s", sidebar)
			}
```

Note that `fileTreeSidebar` slices from `<aside`, so the preceding `<script>` isn't part of `sidebar`.

8. In `docs/code-browser.md`, replace the sentence `Tree and blob pages share the file tree sidebar (`components.FileTreeSidebar`), built by `buildSidebarTree` from the requested ref and path.` with:

```markdown
Tree and blob pages share the file tree sidebar (`components.FileTreeSidebar`), built by `buildSidebarTree` from the requested ref and path. Folders the viewer opens stay open: `static/file_tree.js` keeps them in the per-repo session cookie `cz_tree_open`, which the server reads to render them open. A folder rendered closed loads its children once from `/fragments/{owner}/{repo}/tree/{ref}/{path}`.
```

- [ ] **Step 5: Regenerate and run everything**

Run: `make generate-templ && go build ./... && TEST_DATABASE_DSN='postgres://cloudzilla:test@localhost:5493/cloudzilla_test?sslmode=disable' go test -count=1 ./internal/handler/ ./internal/view/... ./internal/router/ && make lint`
Expected: PASS and `0 issues.`

- [ ] **Step 6: Commit**

```bash
git add cmd/server/frontend/static/file_tree.js internal/view/components/file_tree_sidebar.templ internal/view/components/file_tree_sidebar_templ.go internal/view/components/file_tree_sidebar_test.go internal/view/pages/tree.templ internal/view/pages/tree_templ.go internal/view/pages/blob.templ internal/view/pages/blob_templ.go internal/handler/file_tree_handler.go internal/handler/page_code_browser_test.go docs/code-browser.md
git commit -m "feat(ui): remember open file-tree folders; collapse-all header button" -m "Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

## Task 4: Verify in a browser (controller)

- [ ] **Step 1:** Rebuild the throwaway server (`go build -o <scratchpad>/srv/cz ./cmd/server/`, `make build-css`) and run it on free ports against the seeded `cloudzilla_ui` database.
- [ ] **Step 2:** On a seeded repo with nested folders:
  - Open folder A, then click a root file: A stays open.
  - Open an off-path folder: its children appear, and toggling it again sends no new request.
  - Collapse all closes everything, and it stays closed after the next navigation.
  - The filter matches lazily loaded items.
  - `Esc` clears the filter.
  - The console shows no errors.
- [ ] **Step 3:** Commit a folder whose name contains `"` and `'` into the throwaway repo; confirm it renders and toggles without console errors.
- [ ] **Step 4:** Full `go test ./...` and `make lint`, then a final subagent review of the whole branch diff against both specs.
