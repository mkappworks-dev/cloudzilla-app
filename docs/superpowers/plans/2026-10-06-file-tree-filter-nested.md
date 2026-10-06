# File tree filter shows nested matches — Implementation Plan

**Goal:** The code browser's file-tree filter shows a matching item along with all of its ancestor folders, and it never touches the folders' `open` state or the `cz_tree_open` cookie.

**Architecture:** Two pure methods on the `fileTree` Alpine scope derive visibility from the DOM. A `rev` counter, bumped on `htmx:after:swap`, re-runs them when items arrive.

**Tech Stack:** templ v0.3, htmx 4, Alpine.js 3.

**Spec:** [`docs/superpowers/specs/2026-10-06-file-tree-filter-nested-design.md`](../specs/2026-10-06-file-tree-filter-nested-design.md)

## Global Constraints

- Edit `.templ` files, then run `make generate-templ`. Commit each regenerated `_templ.go` file together with its `.templ` source.
- No name or path may be spliced into an Alpine expression.
- Filtering must never assign `open`.
- Comments state only a *why*, in one line by default.
- Stage files by explicit path. Never stage `.claude/`.

## Task 1 — `file_tree.js`

- [ ] Add `rev: 0` to `fileTree`.
- [ ] In `init()`, add a listener: `this.$el.addEventListener('htmx:after:swap', () => this.rev++)`.
- [ ] Add `matches(li)`, `shows(li)` and `revealsChildren(el)` as specified. Both `shows` and `revealsChildren` read `this.rev`.

## Task 2 — `file_tree_sidebar.templ`

- [ ] `<li>` gets `x-show="shows($el)"`.
- [ ] Folder `<ol>` gets `x-show="open || revealsChildren($el)"`.
- [ ] The chevron's `:class` becomes `{ 'rotate-90': open || revealsChildren($el.closest('li')) }`.
- [ ] Run `make generate-templ`.

## Task 3 — tests and docs

- [ ] `file_tree_sidebar_test.go` asserts the three new bindings and that the old `$el.dataset.name.toLowerCase()` expression is gone. Update the chevron test.
- [ ] `docs/code-browser.md` gets one sentence on the filter.
- [ ] `go test ./internal/view/... ./internal/handler/...` and `go vet` pass.

## Task 4 — browser check

Seed a scratch DB and run the server. Then check each case from the spec's Testing section with Playwright (Chromium at `/opt/pw-browsers`).
