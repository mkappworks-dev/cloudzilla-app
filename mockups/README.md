# UI mockups

Static HTML mocks of the shipped UI, served by a zero-dependency Node server. They use the app's own design tokens and
Tailwind v4 classes, so a class that works here works in a `.templ` file.

```sh
make mockups            # http://localhost:4000, PORT=4100 make mockups to change it
node mockups/server.js  # same, without the Tailwind CLI check
```

```sh
make mockups-generate   # re-render generated/ from the templ components (needs Go)
```

Start at [`index.html`](./index.html), which lists every mock with the route it mirrors. Pages are reachable without the
extension: `/pr`, `/blob`, `/settings`.

## How it fits together

- `server.js` serves this folder and compiles `/assets/tailwind.css` on demand with the Tailwind v4 standalone CLI
  (`bin/tailwindcss`; a linked worktree falls back to the main checkout's, or set `TAILWIND=`). It rebuilds when any
  mock or `tailwind/input.css` changes.
- `input.css` imports `tailwind/input.css`, so the tokens, `@theme` and custom utilities are the app's, not a copy.
- `shell.js` renders the header, repo or account subnav, footer and theme toggle that `layout.templ` renders, and wires
  the dropdowns. Mount it with `<div id="cz-header" data-repo-tab="issues"></div>`; the options are in the file's header
  comment.
- `markdown-editor.js` and `comment-editor.js` stand in for the real editor components.

## Generated or hand-written

A page whose `.templ` renders from a plain view-model is **generated**: `cmd/mockgen` renders the real component with Go
fixtures into `generated/<page>[--variant].html`, so the mock cannot drift from the template. Both the output and the
fixtures are committed, and CI fails when `generated/` is stale. Hand-write a mock only for a page `cmd/mockgen` cannot
render (it needs a live service or request) and list it in [`COVERAGE.md`](./COVERAGE.md).

To add a page or a variant, add an entry to `registry()` in `cmd/mockgen/registry.go`, with its fixtures in
`fixtures.go`, then run `make mockups-generate` and commit the result. A variant exists only where the template
branches: a role (`anon`, `viewer`, `writer`, `admin`), admin mode, `--empty` on list pages, `--error` where the
template has an error state. Never edit a file in `generated/`: the next run overwrites it.

Generated pages carry no htmx or Alpine; `x-*` and `hx-*` attributes stay in the markup and do nothing. Interaction
(theme toggle, and whatever else a page needs) lives in the one shared hand-written `mock-ui.js`. The structural check
against a live server is `tests/mockgen-diff`, see [testing](../docs/testing.md).

## Keeping a mock honest

A hand-written mock mirrors the `.templ` named in [`index.html`](./index.html). When a page changes, change its mock in
the same PR; copy classes from the templ rather than inventing them. Pages and states that exist in the app but have no
mock are tracked in [`COVERAGE.md`](./COVERAGE.md).

Mocks are static: interactions are plain inline JS, with no htmx or Alpine, and sample data stands in for Go template
expressions.
