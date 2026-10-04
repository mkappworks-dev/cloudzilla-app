# 2026-10-04 — File tree remembers open folders

**Status:** Implemented on `feat/blob-page-file-tree` (PR #146)
**Builds on:** [2026-10-04-blob-page-file-tree-design.md](2026-10-04-blob-page-file-tree-design.md)
**Affected subsystem:** Code browser — `internal/handler/page_repo_handler.go`, `internal/view/components/file_tree_sidebar.templ`, a new fragment route and static script

---

## Problem

1. **Folders collapse when you click a file outside them.** Every sidebar click is a full page load. `buildSidebarTree` rebuilds the tree from the URL and expands only the folders on the current path. On `/tree/main/lib`, `lib` is open; clicking the root `README.md` loads `/blob/main/README.md`, whose path has no folders, so `lib` comes back closed.
2. **Off-path folders open onto nothing.** Only folders on the path get their children loaded. Every other folder's chevron toggles Alpine's `open` but reveals an empty `<ol>`. Seen at `/northwind-dev/lucid-router/tree/main/lib`, where `src` opens empty.
3. **Misleading header controls.** The burger icon next to "Files" is a decorative SVG with no behaviour. The down chevron is a "Clear filter" button whose icon suggests collapse/expand. It also duplicates the native ✕ of the `type="search"` input in Chrome and Safari.

## Goals

- A folder the user opened stays open across page loads until the user collapses it.
- Opening any folder shows its children.
- The header offers one useful control, Collapse all, with no decorative noise.

## Non-goals

- The filter still matches only rendered items, so it won't find files inside never-opened folders.
- Client-side navigation (swapping only the file panel). Pages stay full loads.
- Remembering state across browser sessions. The cookie is a session cookie.

## Design

### 1. Remembered folders: cookie, read by the server

- **Cookie:** `cz_tree_open`, `Path=/{owner}/{repo}`, `SameSite=Lax`, session-only. Its value is `encodeURIComponent(JSON.stringify(paths))`, where `paths` is a list of folder paths relative to the repo root (e.g. `["lib", "lib/util"]`), capped at 50 entries (oldest dropped first).
- **Path scoping:** cookie path matching is per segment, so `Path=/acme/app` reaches `/acme/app/tree/…` and `/acme/app/blob/…` but not `/acme/app2/…`. The fragment route (below) lives under `/fragments/…` and doesn't receive the cookie; it doesn't need it.
- **Server read:** a handler helper `openTreeFolders(r) map[string]bool` reads the cookie, applies `url.PathUnescape` then `json.Unmarshal` into `[]string`, and keeps at most the first 50 entries. A missing, unescapable or non-JSON cookie yields an empty set and is not logged (it is client-controlled noise).
- **Server render:** `buildSidebarTree(owner, repoName, ref, path string, open map[string]bool)`. In `buildSidebarLevel`, a directory is expanded (children loaded, `IsOpen: true`) when it is on the remaining path **or** `open[entryPath]` is true. Path matching continues only down the path branch; a folder opened from the set recurses with no remaining path but the same `open` set, so remembered subfolders of a remembered folder open too. A remembered folder that doesn't exist at this ref is never visited, because the walk only descends into folders that exist. A subfolder whose parent is closed is never rendered open, because the walk never reaches it.
- **Cost:** one `GetTree` per expanded folder, the same as path folders cost today, bounded by the 50-entry cap plus path depth.

### 2. Client state: Alpine scope on the aside

New static file `cmd/server/frontend/static/file_tree.js`, loaded by `FileTreeSidebar` with `<script src={ assets.URL("/static/file_tree.js") }></script>`. It is not deferred, so it runs during parsing, before the deferred `alpine.min.js`, and registers its component on `alpine:init`. The `<script>` comes before the `<aside>`, per the CLAUDE.md last-child rule.

`Alpine.data('fileTree', …)` on the `<aside>`, which already holds `filter`:

- `filter: ''`, kept from today.
- `init()`: takes the repo path from the aside's `data-repo-path` attribute. It keeps no copy of the list.
- `remember(path)`: re-reads the cookie, moves or appends `path` to the end, trims to the newest 50, writes the cookie.
- `forget(path)`: re-reads the cookie, removes `path` and every entry starting with `path + '/'`, writes the cookie.
- `collapseAll()`: dispatches `cz-tree-collapse-all`, then writes an empty list.
- `remember` and `forget` re-read the cookie on every write, so a second tab or a page restored from the back/forward cache can't overwrite folders opened elsewhere.
- `save` trims the oldest entries until the encoded value is at most 3800 characters, so the cookie stays under browsers' 4 KB limit.

Each folder's wrapper:

```html
<div data-path="lib" x-data="{ open: true }"
     x-init="open && remember($el.dataset.path); $watch('open', o => o ? remember($el.dataset.path) : forget($el.dataset.path))"
     x-on:cz-tree-collapse-all.window="open = false">
```

- Folders rendered open, whether from the path or the cookie, re-remember themselves on load. After visiting `/tree/main/lib`, `lib` is in the cookie, so a click on the root `README.md` keeps it open.
- **Security:** paths reach JS only via `data-path`, which templ escapes as an attribute value. No user-controlled string is spliced into `x-data`, `x-init` or other JS expressions; `x-data` keeps the only interpolation it has today, a bool.

### 3. Lazy children: fragment route

- **Route:** `GET /fragments/{owner}/{repo}/tree/{ref}/*` → `h.FileTreeChildrenFragment`, inside the existing `r.Route("/fragments", …)` group (which applies `optAuthMW`). The prefix `/fragments/{owner}/{repo}` is already in `repoAPIPrefixes`, so `repo_api_access_test.go` checks the private-repo read guard automatically.
- **Handler:**
  - Guards with `h.readableRepoJSON`, then calls `GetTree(owner, repoName, ref, path)`.
  - A missing path, a file path or a bad ref gets 404 (`writeError`).
  - Otherwise it builds the folder's child nodes with `buildSidebarLevel(owner, repoName, ref, path, entries, nil, nil)` and renders `fragments.FileTreeChildren(nodes, depth)`, where `depth = len(strings.Split(path, "/"))`, so indentation matches a server-rendered level.
- **Template:**
  - `components.FileTreeItems(nodes []TreeNode, depth int)` is exported and renders the `<li>` items; `FileTreeSidebar` uses it for the root level.
  - `fragments.FileTreeChildren` wraps it, keeping the CLAUDE.md rule that handlers render pages or fragments.
- **Trigger:**
  - A folder rendered closed gets, on its chevron button, `hx-get={ n.ChildrenURL }`, `hx-trigger="click once"`, `hx-target="next ol"`, `hx-swap="innerHTML"`. The button's Alpine `x-on:click="open = !open"` still toggles visibility. The first click on a closed folder always opens it, so loading once on that click is correct.
  - Folders rendered open carry no `hx-*` attributes; their children are already in the DOM.
  - The swapped-in `<li>`s are initialised by Alpine's mutation observer and resolve `filter`, `remember` and `forget` from the enclosing aside scope, like server-rendered items.
- **Errors:** htmx 4 doesn't swap 4xx/5xx responses, so a failed load leaves the folder open and empty, as today.

`TreeNode` gains `Path string` (repo-relative) and `ChildrenURL string` (directories only: `/fragments/{owner}/{repo}/tree/{ref}/{path}`), both set in `buildSidebarLevel`.

### 4. Header

- **Burger:** the decorative SVG is removed.
- **Collapse all:** the chevron button becomes `title="Collapse all folders"`, `aria-label="Collapse all folders"`, `x-on:click="collapseAll()"`, with the lucide "chevrons-down-up" icon (`m7 20 5-5 5 5` / `m7 4 5 5 5-5`).
- **Filter:** the input gains `x-on:keydown.escape="filter = ''"`. The native search ✕ fires `input`, which `x-model` already handles. Firefox shows no native ✕, so `Esc` covers it there.

## Testing

Handler tests, in `internal/handler/page_code_browser_test.go`, on the existing `seedCodeRepo` fixture extended with `lib/util/helper.js`:

- `cz_tree_open=["lib"]` on `/blob/main/README.md` → `lib`'s children are listed.
- `cz_tree_open=["lib/util"]` without `lib` on `/blob/main/README.md` → `lib/util/helper.js` is not listed (parent closed).
- `cz_tree_open=["lib","lib/util"]` → `helper.js` is listed.
- A garbled cookie (`%zz`, `not-json`) → 200, rendered as if absent.
- Folders rendered closed carry `hx-get` to their fragment URL; folders rendered open don't.

Fragment tests:

- `GET /fragments/{o}/{r}/tree/main/lib` → 200, contains `lib/config.js`'s blob link and `util`'s tree link, with depth-1 indentation (`padding-left: 20px`).
- `…/tree/main/README.md` (a file) and `…/tree/main/nope` → 404.

Component test: the header has a "Collapse all folders" button, no "Clear filter" button, and the filter input has the `Esc` handler.

Browser check on a seeded throwaway server:

- Open `lib`, click the root `README.md`, and `lib` stays open.
- Open off-path `src`: its children load, and a second toggle sends no new request.
- Collapse all closes everything, and it stays closed after the next navigation.
- The filter applies to lazily loaded items (as for server-rendered ones, a nested match is hidden when its ancestor folders don't match — a pre-existing limitation).
- `Esc` clears the filter.
- A file named with quotes renders and toggles without script errors.
