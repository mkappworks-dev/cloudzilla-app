# 2026-10-06 — File tree filter shows nested matches

**Status:** Approved
**Builds on:** [2026-10-04-file-tree-remembers-folders-design.md](2026-10-04-file-tree-remembers-folders-design.md)
**Affected subsystem:** Code browser — `cmd/server/frontend/static/file_tree.js`, `internal/view/components/file_tree_sidebar.templ`

---

## Problem

The sidebar filter hides an item unless its own name contains the filter text. A folder whose name doesn't match is hidden even when a descendant does, which hides the whole subtree. Typing `inner` hides `lib/`, so `lib/util/deep/inner.js` can't be seen. A match inside a closed folder that has already loaded its children stays hidden, because the folder's `<ol>` is shown only when `open`.

## Goals

- A matching item is visible along with every folder above it.
- Folders above a match show their children while the filter is set, and their chevrons point down, without changing `open`.
- Clearing the filter (typing it away or pressing Esc) brings back the open and closed state from before filtering.
- Filtering never writes the `cz_tree_open` cookie.
- Items that arrive by htmx swap (a folder's lazy load, or the Expand all button) are filtered as soon as they land.

## Non-goals

- Matching items in folders that have never loaded. The filter still sees only rendered items, as the earlier spec says. Expand all is the way to search the whole tree.
- Fuzzy or path matching. The filter is still a case-insensitive substring match on the item's name.
- Keeping `aria-expanded` in sync. It is server-rendered and Alpine never updates it, which predates this fix and is tracked separately.

## Design

Visibility is computed from the DOM on each evaluation. It is never stored in Alpine state.

### `fileTree` scope (`file_tree.js`)

- `rev: 0`. It is bumped by an `htmx:after:swap` listener on the aside, added in `init()`. Alpine can't track DOM queries, so the bump re-runs every binding that reads `rev` after new items are inserted. Swap events bubble from their target, so both lazy-loaded folder lists and the Expand all swap of the root `<ol>` reach it.
- `matches(li)` checks whether `li.dataset.name.toLowerCase()` includes `filter.toLowerCase()`.
- `revealsChildren(el)` reads `rev`. It is true when `filter` is non-empty and some `li[data-name]` below `el` matches.
- `shows(li)` reads `rev`. It is true when `filter` is empty, when `matches(li)` is true, or when `revealsChildren(li)` is true.

### Template (`file_tree_sidebar.templ`)

| Element        | Before                                  | After                                                       |
|----------------|-----------------------------------------|-------------------------------------------------------------|
| `<li>`         | `x-show="!filter \|\| …includes(…)"`     | `x-show="shows($el)"`                                       |
| folder `<ol>`  | `x-show="open"`                         | `x-show="open \|\| revealsChildren($el)"`                    |
| chevron        | `:class="{ 'rotate-90': open }"`        | `:class="{ 'rotate-90': open \|\| revealsChildren($el.closest('li')) }"` |

The `li` and the folder `ol` contain the same descendant items, so one `revealsChildren` serves both.

Names reach JS only through `data-name`. No name is spliced into an expression.

### Behaviour of a folder whose own name matches

A folder whose own name matches stays visible, and its children follow the usual rules: they show if the folder is open or if they match. This is the same as before the fix.

## Cost

Each binding scans its subtree: O(items × depth) per keystroke. For a fully expanded tree of a few thousand entries, that is well under a frame.

## Testing

- Component tests (`file_tree_sidebar_test.go`) assert the new bindings and the absence of the old inline expression.
- Browser check on a seeded server:
  - `inner` reveals `lib/util/deep/inner.js` with its ancestors.
  - Clearing restores the earlier state.
  - `cz_tree_open` is unchanged.
  - A lazily loaded match appears.
  - Esc clears the filter.
  - A name containing quotes is harmless.
