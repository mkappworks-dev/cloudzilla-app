# UI mockups

Static HTML mocks of the shipped UI, served by a zero-dependency Node server. They use the app's own design tokens and
Tailwind v4 classes, so a class that works here works in a `.templ` file.

```sh
make mockups            # http://localhost:4000, PORT=4100 make mockups to change it
node mockups/server.js  # same, without the Tailwind CLI check
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

## Keeping a mock honest

A mock mirrors the `.templ` named in [`index.html`](./index.html). When a page changes, change its mock in the same PR;
copy classes from the templ rather than inventing them. Pages and states that exist in the app but have no mock are
tracked in [`COVERAGE.md`](./COVERAGE.md).

Mocks are static: interactions are plain inline JS, with no htmx or Alpine, and sample data stands in for Go template
expressions.
