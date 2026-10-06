# 2026-10-04 — Blob page keeps the file tree

**Status:** Implemented on `feat/blob-page-file-tree`
**Affected subsystem:** Code browser ([code-browser](../../code-browser.md)) — `internal/handler/page_repo_handler.go`, `internal/view/pages/{tree,blob}.templ`, `internal/view/components/file_tree_sidebar.templ`, `internal/view/viewmodels_repo.go`

---

## Problem

`/{owner}/{repo}/tree/{ref}/{path}` shows a file tree sidebar beside the directory listing. Every file link, in the sidebar and in the listing, goes to `/{owner}/{repo}/blob/{ref}/{path}`, and `PageBlob` renders `blob.templ`, which has no sidebar. Clicking a file therefore loses the tree. Example: `/acme-labs/brave-pipeline/tree/main/lib` shows the tree, `/acme-labs/brave-pipeline/blob/main/lib/config.js` does not.

Two further problems sit in the same code:

1. **Two file viewers.** When a `/tree/` path turns out to be a file, `PageTree` falls back to `GetBlob` and renders an inline viewer (`TreeFileView`) inside `tree.templ`. That branch duplicates most of `PageBlob` (last-commit lookup, permissions, raw/blame URLs) and a weaker copy of `blob.templ` (no line count or size, no button icons, no permalink hint).
2. **The active file is matched by name.** `FileTreeSidebar(nodes, activeName)` sets `aria-current="page"` on every file whose `Name == activeName`. With `README.md` at the root and `lib/README.md` open, both rows are highlighted.

## Goals

- `/blob/{ref}/{path}` shows the same file tree as `/tree/`, with the folders along the path expanded and only the open file highlighted.
- One file viewer: `blob.templ`.
- No change to the blob page's existing header, actions or line anchors.

## Non-goals

- Making the two-column grid responsive. `grid-cols-[260px_1fr]` has no phone breakpoint on the tree page either; both pages stay consistent.
- Escaping special characters in code-browser URLs. Every blob/tree URL in the codebase is built by plain concatenation; the redirect follows the same idiom.
- Fixing `aria-expanded?={ n.IsOpen }`, which renders a valueless attribute.
- The blame page (`/blame/`) keeps its current layout.

## Design

### 1. Shared sidebar component

`components.FileTreeSidebar(nodes []TreeNode)` renders the whole `<aside>`: header, Alpine filter input (`x-data="{ filter: '' }"`), and the tree. Before, only the `<ol>` lived in the component and its doc comment required every caller to provide the `x-data` wrapper; moving the wrapper in removes that contract. Both `tree.templ` and `blob.templ` call it.

### 2. Blob page gets the tree

- `BlobData` gains `Sidebar []components.TreeNode`.
- `PageBlob` sets `Sidebar: h.buildSidebarTree(owner, repoName, ref, result.Path)`, and `PageTree` now passes the requested `ref` too. `result.Ref` is a display ref that shortens a SHA to 7 characters, which `GetTree` cannot resolve, so with it the sidebar came out empty on every SHA-ref page (the tree page had this bug before this change).
- `blob.templ` keeps its title section full width, then lays out `grid grid-cols-[260px_1fr] gap-4` with the sidebar on the left and a `min-w-0` column holding the file section and the permalink hint. `min-w-0` stops long code lines from widening the grid track; the code table keeps its own `overflow-x-auto`.
- The file header's heading gets `truncate` and its icon and action group get `shrink-0`, since the column is 276px narrower than before.

### 3. One file viewer

When `GetTree` fails on a non-empty path and `GetBlob` succeeds, `PageTree` responds `302 Found` to `/{owner}/{repo}/blob/{ref}/{path}` using the requested `ref` and `path`. 302 rather than 301: whether a path is a file depends on the ref's current commit, and a later push can turn it into a directory, so browsers must not cache the redirect.

A path that is neither a directory nor a file still 404s at its `/tree/` URL; the `GetBlob` check keeps nonexistent paths from redirecting.

Removed: `view.TreeFileView`, `TreeData.FileView`, `TreeData.ActiveFile`, the `FileView` branch of `tree.templ`, and the duplicated handler code.

### 4. Active file by full path

`TreeNode` gains `IsActive bool`. `buildSidebarTree` now receives the page's full path (the file's path on the blob page, the directory's on the tree page). It already walks the path segment by segment to expand folders; at the last segment, a file whose name matches is marked `IsActive`. The component renders `aria-current="page"` and the active style from `IsActive`, and the `activeName` parameter goes away. On the tree page the last segment is a directory, so no file is marked.

## Testing

`internal/handler/page_code_browser_test.go`, through the real router (`newAPIRouterAt`) on a repo with `README.md`, `lib/README.md` and `lib/config.js`:

- `TestCodePages_KeepTheFileTree` — for `/blob/main/lib/config.js`, `/blob/main/lib/README.md`, `/blob/main/README.md`, `/blob/<full sha>/lib/config.js` and `/tree/main/lib`: 200; the sidebar `<aside>` is present with its `x-data="{ filter: '' }"` scope; blob pages keep their `#L1` line anchors; exactly one `aria-current="page"` inside it, on the open file's link (none on the tree page); `lib/`'s children are listed exactly when the path is under `lib/`. Assertions are scoped to the `<aside>` because the repo subnav also uses `aria-current="page"`.
- `TestPageTree_RedirectsAFileToItsBlobPage` — `/tree/main/lib/config.js` → 302 to `/blob/main/lib/config.js`.
- `TestFileTreeSidebar_RendersHierarchy` (component) updated for the new signature and `IsActive`.

Visual check on a throwaway server with seeded data: the blob page shows the tree, the open file is highlighted, and long lines scroll inside the code panel.
