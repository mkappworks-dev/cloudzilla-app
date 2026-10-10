# Reload the browser from the dev server itself, not templ's proxy

Created: 2026-10-11
Category: enhancement
Status: done
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
es.addEventListener("hello", function (e) {
  var prev = sessionStorage.getItem(KEY);
  sessionStorage.setItem(KEY, e.data);
  if (prev && prev !== e.data) location.reload();
});
es.addEventListener("reload", function () {
  location.reload();
});
```

Comparing ids rather than counting connections is what makes a reload loop impossible: after the
reload the client reconnects to the _same_ server, sees the same id, and stops. A restart changes
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

- [x] A `.templ` edit rebuilds, restarts and refreshes the browser **exactly once**
- [x] A `.go` edit does the same
- [x] A `tailwind/input.css` or `cmd/server/frontend/**/*.js` edit refreshes with **no rebuild**
- [x] No refresh can arrive before the new binary is listening, by construction rather than by
      polling — demonstrated by a test over the instance-id rule
- [x] A reload never loops: reconnecting to the same server does not refresh again
- [x] `make dev` runs two processes and one Ctrl-C stops both; the browser URL is `:8080`
- [x] `go build ./cmd/server/.` with no tags registers no `/_dev/` route and starts no watcher,
      and `grep -rn "_dev/reload" internal/view/layout` matches only the dev-tagged file
- [x] Unit tests for the hub without the build tag: hello carries the id, a broadcast reaches
      every subscriber, a slow subscriber cannot block another, and the watcher debounces
- [x] `make test`, `make lint`, and `go build -tags dev ./...` all pass
- [x] CONTRIBUTING.md rewritten for the new loop; ticket 03 closed as superseded

## Comments

Claude, 2026-10-11: done in `tech/dev-hot-reload`. Verified by holding the reload stream like a
browser tab would and counting the refreshes a client's own rule would perform.

| change                  | served in | refreshes | restarts |
| ----------------------- | --------: | --------: | -------: |
| `.templ` edit           |      4.3s |         1 |        1 |
| `.go` edit              |      1.2s |         1 |        1 |
| `main.css` edit         |     <0.5s |         1 |        0 |
| 5s idle after the above |         — |         0 |        0 |

Six changes over a run produced six refreshes: one each, none spurious, no loop. `make dev` is
two processes, and one SIGINT to the group stopped make, air, Tailwind **and** the server air had
started in its own process group.

**One thing the design did not anticipate.** The reload stream is itself a long-lived in-flight
request, and `http.Server.Shutdown` waits for those, so an open tab held every restart open until
air's `kill_delay` expired — measured at **2.45s of pure waiting per restart**. `Hub.Close`, wired
to SIGINT in `internal/router/devreload_dev.go`, ends the streams so the drain completes at once:
the restart window dropped to **0.48s**, and the whole edit-to-serving cycle for a `.go` edit from
4.5s to 1.2s. `signal.Notify` fans out, so `main.go`'s own handler is unaffected and needed no
dev-only hook. `kill_delay` stays at 2s as the backstop for a real in-flight git clone.

Two test-quality notes. The hub's tests were written as one batch, so I mutation-checked the two
load-bearing ones afterwards: removing the debounce fails the watcher test, and making the
broadcast blocking fails the slow-subscriber test. The second only started failing after I raised
that test from 50 events to 50,000 — below the socket and transport buffers nothing ever stalls,
so the test had been passing against a blocking implementation and proving nothing.

Also replaced ticket 02's criteria that this supersedes: its "refreshes once" and "never refreshes
before the new binary answers" are now true by construction rather than by polling, and the
single-Ctrl-C and no-30s-drain checks it left unverified are measured above against this loop.

Claude, 2026-10-11, after review: a code review argued that a `.templ` edit introducing a new
Tailwind class would refresh twice — Tailwind rewriting `main.css` inside air's build window, so
`Hub.Watch` broadcasts against the old, still-running server, and the client's `reload` listener
bypasses the instance-id rule. The reasoning is sound and the code has no coalescing between the
asset path and the restart path.

Measured, it does not happen today, for an unrelated reason: Tailwind's watcher never rebuilds on
a `.templ` change at all (it only watches its own entry file), so no asset event is emitted during
a rebuild. Verified with an arbitrary value absent from `main.css` — `rotate-[17deg]` produced no
rewrite in 30s — and tracked as its own bug in `.scratch/2026-10-11-tailwind-source-watch/`.

So "exactly once" holds as measured, but it rests on that bug. Whoever fixes the Tailwind watcher
should expect the double refresh to appear and needs to suppress an asset push while a source
rebuild is in flight, or coalesce the two paths in the client.
