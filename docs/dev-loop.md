# Dev loop

`make dev` rebuilds, restarts and refreshes the browser on save. CONTRIBUTING.md covers running
it; this covers what it is made of and the traps in changing it.

Everything here is behind the `dev` build tag and absent from a release binary. `make build` is
unaffected: assets stay embedded, no `/_dev/` route exists, and no watcher runs.

## Two processes

| process             | watches                                | does                                       |
| ------------------- | -------------------------------------- | ------------------------------------------ |
| `air` (`.air.toml`) | `cmd`, `internal` — `.go` and `.templ` | runs `make dev-build`, restarts the server |
| `bin/tailwindcss`   | `tailwind/input.css`                   | rewrites `static/main.css`                 |

The browser refresh comes from the server itself, not from a watcher — see below.

`air`'s `include_dir` is an allowlist on purpose. The repo root holds ~52k files, two thirds of
them other branches' worktrees and bare repositories, against ~1.5k under `cmd` and `internal`.
On darwin fsnotify watches through kqueue, which holds a descriptor per watched directory, so
overshooting exhausts descriptors rather than merely wasting CPU.

Two `.air.toml` values are load-bearing and easy to get wrong:

- `stop_on_error = false`. air's `stop_on_error` stops the binary after a failed build, so `true`
  takes the site down on a typo. With `false` the last good server keeps serving and the error
  goes to the console.
- `send_interrupt` with `kill_delay = "2s"`. SIGINT is what frees `:8080` and `:2222`, because
  `cmd/server/main.go` closes the listeners before draining; the drain then waits up to 30s for
  in-flight requests, and a git clone over HTTP is one. The kill 2s later abandons a transfer
  that no longer matters in dev.

`dev-build` regenerates `_templ.go` and builds with `CGO_ENABLED=0`, which uses Go's internal
linker rather than clang and takes a view-package rebuild from 6.1s to 4.2s. Because it rewrites
the files air watches, `exclude_regex` drops `_templ.go` — otherwise every build would trigger
the next.

## Assets from disk

`assets.Set` is an interface. `New` returns the release implementation, which walks the embedded
tree once and caches a hash per file; under the `dev` tag it returns `NewDev`, which resolves each
file per request. `cmd/server/embed_dev.go` supplies `os.DirFS("cmd/server")` in place of the
embedded FS, so **`make dev` must run from the repo root**.

The dev implementation differs only where staleness would otherwise leak:

- Hashes are cached against mtime and size, not held forever. The layout resolves eight assets per
  render and one is a 5.3 MB bundle, so hashing every one on every render cost 2.5ms against
  0.012ms now. The trade is recorded at `devEntry`: a filesystem with one-second timestamps could
  miss a same-size edit inside one tick.
- Responses are `no-cache` with an `ETag`, so an unchanged file answers 304. `no-store` would be
  simpler but re-sends the whole tree on every refresh, and every save causes a refresh.
- Nothing is gzipped. The release path memoizes the compressed body per file in a `sync.Once`,
  which would serve one edit's bytes under a correct hash for the rest of the session.

## Browser refresh

`internal/devreload` serves an event stream at `/_dev/reload` and a client at `/_dev/reload.js`,
injected into the layout by `@devReload()` — defined as `templ.NopComponent` without the tag.
Both are registered in `probeMux`'s `probes` map, which answers exact paths ahead of every
middleware, which is why neither `middleware/setup.go` nor CSRF needs a dev-only exception.

The page refreshes when the stream **reconnects reporting a different instance id**. That is what
makes the timing exact rather than best-effort: the reconnect is only possible once the new server
is listening, so a refresh cannot arrive early, and reconnecting to the same instance does
nothing, so a refresh cannot loop. Asset edits are pushed as an explicit `reload` event by an
fsnotify watcher on `cmd/server/frontend`, debounced 100ms because one Tailwind build emits
several events.

`Hub.Close` runs on SIGINT. Without it the stream is a long-lived in-flight request that
`http.Server.Shutdown` waits for, which cost 2.45s of every restart until an open tab's stream was
closed; the restart window is 0.48s with it. `signal.Notify` fans out, so `main.go`'s own handler
is unaffected and needs no dev-only hook.

## Known gaps

- **A brand-new Tailwind class emits no CSS.** The Tailwind watcher reacts to its own entry file
  but never rescans the `@source` glob, so a utility the project has never used renders unstyled
  until `make build-css`. Pre-existing; tracked in `.scratch/2026-10-11-tailwind-source-watch/`.
  Fixing it will expose a second refresh on `.templ` edits, because an asset push would then fire
  against the still-running old server mid-rebuild; that needs coalescing with the restart path.
- **CI never compiles this.** `go test ./...` and `make lint` run without tags, so no `*_dev.go`
  file is built and `TestNew_FollowsTheBuildTag` only ever asserts the release case. Run
  `go build -tags dev ./...` and `go test -tags dev ./internal/assets/` by hand when changing any
  of it.
