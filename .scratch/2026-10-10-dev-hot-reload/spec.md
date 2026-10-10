# Hot-reloading dev loop

Created: 2026-10-10
Category: enhancement
Status: ready-for-agent

## Problem

`make dev` (`Makefile:71`) watches two of the three layers and reloads none of them. It runs
`tailwindcss --watch` and `templ generate --watch` beside a single `go run ./cmd/server/.`, so
templ dutifully regenerates `_templ.go` files that nothing recompiles. Every change — a template,
a handler, or a line of CSS — needs a manual Ctrl-C and `make dev`.

Nothing is read from disk at request time, which is why even a CSS edit needs a restart:

- templ compiles templates into Go, so a `.templ` edit is a Go edit.
- `cmd/server/embed.go` embeds the whole `frontend/` tree with `//go:embed all:frontend`.
- `assets.New` (`internal/router/router.go:675`) hashes that tree once at startup, and the
  `?v=<hash>` already baked into rendered HTML is served `max-age=31536000, immutable`. A new
  `main.css` on disk is invisible to a browser holding the old URL.

Measured on an M-series mac, `go build ./cmd/server/.` after a one-line edit:

| edited                                 | compile | link  | total |
| -------------------------------------- | ------- | ----- | ----- |
| a page in `internal/view/pages`        | 3.9s    | 0.9s  | ~4.8s |
| a file in `internal/handler`            | 1.1s    | 0.9s  | ~2.0s |
| `cmd/server/main.go`                   | —       | 1.0s  | ~1.0s |

With the default `CGO_ENABLED=1` the same view-page edit costs **6.1s** instead of 4.2s, because
darwin links through clang rather than Go's internal linker.

## Goals

1. A `.css` or `.js` edit under `cmd/server/frontend` is visible on refresh with **no Go rebuild**.
2. A `.templ` or `.go` edit rebuilds and restarts the server by itself, and the browser refreshes
   only once the new binary is answering.
3. Release builds are unchanged: assets embedded, and no code path that can serve them from disk.

## Approach

- A `dev` build tag selects `os.DirFS` over the embedded FS, and `internal/assets` grows a mode
  that re-stats on demand instead of hashing once (ticket 01).
- `make dev` runs `air` for the rebuild, templ's `--proxy` for the browser refresh, and
  `CGO_ENABLED=0` for the 30% link saving (ticket 02).
- A Tailwind rebuild notifies the proxy, so CSS-only work needs no keypress (ticket 03).

## Non-goals

- Splitting `internal/view/pages` — 60,605 lines in one package, and the 3.9s term above. Worth
  doing on its own merits, not as a build-speed fix.
- In-process code swap (yaegi, goloader, Go `plugin`). Function-level reload excludes generics
  and is explicitly not production-grade; goloader is unmaintained; `plugin` leaks every old
  version and gives each one distinct types.
- Sub-second feedback for Go changes. The ~0.9s link is a floor, so the realistic target is ~4s
  today and ~2s if the view package is ever split.
- Docker and production config.

## Acceptance criteria

- [ ] Editing `tailwind/input.css` shows the new CSS after a refresh with no `go build` in air's log
- [ ] Editing a `.templ` or `.go` file rebuilds, restarts, and refreshes the browser once
- [ ] A restart never fails with "address already in use" on `:8080` or `:2222`
- [ ] `go build ./cmd/server/.` with no tags still embeds the frontend, and `os.DirFS` appears in
      no file that a tagless build compiles
- [ ] `make test` and `make lint` pass, and so does `go build -tags dev ./...`

## Comments
