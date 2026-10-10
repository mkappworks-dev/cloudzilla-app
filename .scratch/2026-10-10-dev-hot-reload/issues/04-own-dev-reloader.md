# Reload the browser from the dev server itself, not templ's proxy

Created: 2026-10-11
Category: enhancement
Status: ready-for-agent
Blocked by: 01, 02

## Problem

Ticket 02 landed a working loop, but with templ's proxy as the reload channel it carries three
costs that all trace back to one constraint: templ's proxy only starts once its watcher has run
a generation pass, so the watcher cannot be silenced.

- A `.templ` edit refreshes twice. templ refreshes the moment it regenerates — before air has
  rebuilt — so the browser shows the previous build, and `scripts/dev-notify-proxy.sh` has to
  refresh again once `/healthz` answers.
- The refresh is gated on polling. The script spawns `curl` every second for the whole session
  to notice the restart.
- Browsing moves to `:7331`, away from the port everything else in the project talks about.

The gating is also only as good as the poll: it is a heuristic for "the new server is up",
rather than something that cannot be wrong.

## Approach

A dev build serves its own reload channel, and the browser reloads **on reconnect**. The
browser can only reconnect once the new binary is listening, so a refresh arriving early is not
unlikely — it is impossible. No polling, no proxy, no second port.

`internal/devreload`, a normal package so it is testable without the build tag:

```go
func New(instanceID string) *Hub
func (h *Hub) Events(w http.ResponseWriter, r *http.Request)  // SSE
func (h *Hub) Script(w http.ResponseWriter, r *http.Request)  // client JS
func (h *Hub) Reload()                                        // broadcast
func (h *Hub) Watch(ctx context.Context, dir string) error     // assets → Reload
```

On connect the stream sends `event: hello` carrying the server's instance id, then `retry: 500`
so a dropped stream reconnects in half a second, then keepalives. The client compares the id to
the one it saw last:

```js
es.addEventListener('hello', function (e) {
  var prev = sessionStorage.getItem(KEY);
  sessionStorage.setItem(KEY, e.data);
  if (prev && prev !== e.data) location.reload();
});
es.addEventListener('reload', function () { location.reload(); });
```

Comparing ids rather than counting connections is what makes a reload loop impossible: after the
reload the client reconnects to the *same* server, sees the same id, and stops. A restart changes
the id exactly once, so a restart reloads exactly once.

`Watch` covers the case that needs no rebuild at all — Tailwind rewriting `main.css`, or a `.js`
edit under `cmd/server/frontend` — and pushes `reload` directly. fsnotify is already in the
module graph as an indirect dependency of viper, so this promotes it to direct rather than adding
anything. Writes are debounced (~100ms), because a Tailwind build can emit several events.

**Wiring**, all of it tag-gated so a release build contains no route and no watcher:

- The two endpoints are registered in `probeMux`'s `probes` map in `internal/router/router.go`,
  which answers exact paths ahead of every middleware. That is how `/healthz` already bypasses
  the setup redirect, so no change to `internal/middleware/setup.go`, CSRF or auth is needed.
- `internal/view/layout/layout.templ` gains one line, `@devReload()`, before `</body>`.
  `devReload` is defined twice: `templ.NopComponent` under `//go:build !dev`, and the script tag
  under `//go:build dev`. The layout is the only place the script is needed, since HTMX
  fragments do not render it.

## Scope

Also removed, since the proxy is gone:

- `scripts/dev-notify-proxy.sh` and the `DEV_PROXY_PORT` variable.
- templ's `--watch`/`--proxy` process from `make dev`. air's `dev-build` regenerates instead, so
  air watches `.templ` and excludes `_templ.go` to avoid retriggering itself. `make dev` is then
  two processes, air and Tailwind, and the browser stays on `:8080`.
- Ticket 03, which exists only to make a CSS change refresh the browser. `Watch` does that.

## Acceptance criteria

- [ ] A `.templ` edit rebuilds, restarts and refreshes the browser **exactly once**
- [ ] A `.go` edit does the same
- [ ] A `tailwind/input.css` or `cmd/server/frontend/**/*.js` edit refreshes with **no rebuild**
- [ ] No refresh can arrive before the new binary is listening, by construction rather than by
      polling — demonstrated by a test over the instance-id rule
- [ ] A reload never loops: reconnecting to the same server does not refresh again
- [ ] `make dev` runs two processes and one Ctrl-C stops both; the browser URL is `:8080`
- [ ] `go build ./cmd/server/.` with no tags registers no `/_dev/` route and starts no watcher,
      and `grep -rn "_dev/reload" internal/view/layout` matches only the dev-tagged file
- [ ] Unit tests for the hub without the build tag: hello carries the id, a broadcast reaches
      every subscriber, a slow subscriber cannot block another, and the watcher debounces
- [ ] `make test`, `make lint`, and `go build -tags dev ./...` all pass
- [ ] CONTRIBUTING.md rewritten for the new loop; ticket 03 closed as superseded
