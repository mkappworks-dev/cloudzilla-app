# Refresh the browser on a CSS or JS change, with no Go rebuild

Created: 2026-10-10
Category: enhancement
Status: ready-for-agent
Blocked by: 01, 02

## Problem

After ticket 01 a CSS edit needs no Go rebuild, and after ticket 02 a Go edit refreshes the
browser by itself. The gap left is the case that should be the fastest of the three: editing
`tailwind/input.css` or a file under `cmd/server/frontend/static/` rebuilds the stylesheet in
well under a second, and then nothing tells the browser. Air is watching `.go` only — correctly,
since a rebuild would be pure waste — so the fastest change in the loop is the one that still
needs a keypress.

## Scope

A second watcher whose only job is to notify the proxy, which is how templ's own docs wire this
(an air instance with `bin = "true"`, watching the asset extensions). Either a second air config
or `wgo` is fine; prefer whichever keeps `make dev` legible.

Watch `cmd/server/frontend/static/` for `css,js` — that is Tailwind's **output** directory, so
the notify fires after `main.css` is rewritten, never before. Watching `tailwind/input.css`
instead would race the Tailwind build. The action is `templ generate --notify-proxy`.

One wrinkle to decide while implementing: a `.templ` edit that introduces a new Tailwind
class fires both notifies — the healthz transition from ticket 02 and this watcher — so the
browser can reload twice. Either coalesce notifies within a short window (~500ms) or accept
the double reload and say so in CONTRIBUTING.md.

The refresh is what makes the new hash visible: `assets.URL` re-hashes on render (ticket 01), so
a full page reload is enough — no Go rebuild and no restart.

## Acceptance criteria

- [ ] Editing `tailwind/input.css` refreshes the browser and shows the new styles
- [ ] Air's log shows no rebuild for that change, and the server's PID is unchanged
- [ ] Editing `cmd/server/frontend/static/kanban.js` does the same
- [ ] A `.templ` edit that changes both markup and classes either refreshes once or is
      documented as refreshing twice — not left as an unexplained flicker
- [ ] CONTRIBUTING.md notes which changes skip the Go rebuild
