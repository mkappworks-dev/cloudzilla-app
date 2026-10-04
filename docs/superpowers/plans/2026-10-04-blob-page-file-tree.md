# Blob page keeps the file tree — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Show the repository file tree on `/blob/` pages as on `/tree/` pages, with one file viewer and the open file highlighted by full path.

**Architecture:** The `<aside>` moves into `components.FileTreeSidebar`, which both pages render. `PageBlob` builds the sidebar from the file's path; `buildSidebarTree` marks the matching file `IsActive`. `PageTree` redirects file paths to `/blob/` instead of rendering its own inline viewer.

**Tech Stack:** Go, chi, templ, Alpine.js, Tailwind v4.

**Spec:** [`docs/superpowers/specs/2026-10-04-blob-page-file-tree-design.md`](../specs/2026-10-04-blob-page-file-tree-design.md)

---

## File structure

```
internal/
├── handler/
│   ├── page_repo_handler.go          ← PageTree redirect, PageBlob sidebar, buildSidebarTree IsActive
│   └── page_code_browser_test.go     ← NEW: page + redirect tests
└── view/
    ├── viewmodels_repo.go            ← drop TreeFileView/FileView/ActiveFile; BlobData.Sidebar
    ├── components/
    │   ├── file_tree_sidebar.templ   ← renders the whole <aside>; TreeNode.IsActive
    │   └── file_tree_sidebar_test.go ← new signature
    └── pages/
        ├── tree.templ                ← use component; drop FileView branch
        └── blob.templ                ← grid with sidebar; header truncation
```

---

## Task 1: Failing tests

**Files:** Create `internal/handler/page_code_browser_test.go`

- [x] **Step 1:** `seedCodeRepo` seeds a public repo and commits `README.md`, `lib/README.md`, `lib/config.js` with `CodeService.CommitFile`; returns `newAPIRouterAt` and the `seededRepo`.
- [x] **Step 2:** `TestCodePages_KeepTheFileTree` table over the three blob URLs and `/tree/main/lib`, asserting inside the `<aside>` only (the repo subnav also has `aria-current="page"`): one `aria-current` on the open file's `href`, none on the tree page; `lib/config.js` listed exactly when the path is under `lib/`.
- [x] **Step 3:** `TestPageTree_RedirectsAFileToItsBlobPage`: `/tree/main/lib/config.js` → 302 to `/blob/main/lib/config.js`.
- [x] **Step 4:** Run with `TEST_DATABASE_DSN`; the blob cases fail with "no file tree sidebar", the redirect gets 200, the folder case passes.

## Task 2: Shared sidebar component

**Files:** Modify `internal/view/components/file_tree_sidebar.templ`, `file_tree_sidebar_test.go`, `internal/view/pages/tree.templ`

- [x] **Step 1:** Add `IsActive bool` to `TreeNode`. `FileTreeSidebar(nodes []TreeNode)` renders the `<aside>` (header, filter input, tree) moved verbatim from `tree.templ`; drop the "caller must wrap in x-data" doc comment.
- [x] **Step 2:** `treeItem` loses `activeName`; `aria-current` and `fileItemClass` use `n.IsActive`.
- [x] **Step 3:** `tree.templ` calls `@components.FileTreeSidebar(data.Sidebar)`.
- [x] **Step 4:** Update the component test: `IsActive: true` on `main.go`, call `FileTreeSidebar(nodes)`.

## Task 3: Active file by full path

**Files:** Modify `internal/handler/page_repo_handler.go`

- [x] **Step 1:** `buildSidebarTree(owner, repoName, ref, path)` documents that `path` may be a file. In `buildSidebarLevel`, set `IsActive: !e.IsDir && len(remainingPath) == 1 && e.Name == remainingPath[0]`.
- [x] **Step 2:** Mutation check: matching on the last segment at every level (`e.Name == remainingPath[len(remainingPath)-1]`) makes the shared-name case fail with 2 `aria-current`; restore.

## Task 4: Blob page renders the tree

**Files:** Modify `internal/view/viewmodels_repo.go`, `internal/view/pages/blob.templ`, `internal/handler/page_repo_handler.go`

- [x] **Step 1:** `BlobData.Sidebar []components.TreeNode`; `PageBlob` sets it with `buildSidebarTree(owner, repoName, result.Ref, result.Path)`.
- [x] **Step 2:** In `blob.templ`, wrap the file section and permalink hint in `grid grid-cols-[260px_1fr] gap-4` → sidebar + `<div class="min-w-0">`.
- [x] **Step 3:** File header: icon `shrink-0`, `h2` `truncate`, action group `shrink-0`.

## Task 5: One file viewer

**Files:** Modify `internal/handler/page_repo_handler.go`, `internal/view/viewmodels_repo.go`, `internal/view/pages/tree.templ`

- [x] **Step 1:** Replace `PageTree`'s blob fallback with: if `GetBlob` succeeds, `http.Redirect(..., "/"+owner+"/"+repoName+"/blob/"+ref+"/"+path, http.StatusFound)`. One-line comment on why 302, not 301.
- [x] **Step 2:** Delete `TreeFileView`, `TreeData.FileView`, `TreeData.ActiveFile`, the `FileView` branch of `tree.templ` (de-indent the listing), and `tree.templ`'s now-unused `strconv` import.
- [x] **Step 3:** `make generate-templ`, `go build ./...`, then the Task 1 tests pass.

## Task 6: Verify

- [x] **Step 1:** `TEST_DATABASE_DSN=... go test -count=1 ./...` passes; `make lint` reports 0 issues.
- [x] **Step 2:** Spec review: a subagent checks the diff against the spec and the repo's CLAUDE.md conventions.
- [x] **Step 4:** Review fixes. Add a `/blob/<full sha>/…` case (fails: empty sidebar), then pass the requested `ref` instead of `result.Ref` to `buildSidebarTree` in `PageTree` and `PageBlob`. Assert the sidebar's `x-data` scope and the blob page's `#L1` anchor. Add the redirect and shared sidebar to `docs/code-browser.md`.
- [x] **Step 3:** Visual check on a throwaway server (seeded scratch DB): blob page in a folder and at the root, long-line file, `/tree/<file>` redirect, sidebar filter.
