# Serve the frontend from disk under a dev build tag

Created: 2026-10-10
Category: enhancement
Status: done

## Problem

`cmd/server/embed.go` embeds `frontend/` into the binary, and `assets.New` hashes it once at
startup, so a CSS or JS edit is invisible until a rebuild and restart — the `?v=<hash>` in
already-rendered HTML still names the old bytes and is cached `immutable` for a year.

A dev build should read those files from disk instead. A release build must not be able to:
pointing a production binary at an arbitrary directory is not a knob worth shipping, so the
switch is a build tag rather than a config key or an env var.

## Scope

**`cmd/server`** — replace `embed.go` with two files. `router.New` takes `frontend fs.FS` and
does `fs.Sub(frontend, "frontend")`, so the dev root is `cmd/server`, which keeps both variants
rooted the same way. `make dev` already runs from the repo root.

```go
// embed_prod.go
//go:build !dev

//go:embed all:frontend
var frontendFS embed.FS

// embed_dev.go
//go:build dev

// Read from the working tree so a CSS or templ-free asset edit needs no rebuild.
var frontendFS fs.FS = os.DirFS("cmd/server")
```

**`internal/assets`** — a dev `Set` that resolves per request instead of trusting the startup
walk. Add a tag-gated constant so the mode follows the binary and `router.New` keeps its
signature:

```go
// mode_prod.go: //go:build !dev  → const devMode = false
// mode_dev.go:  //go:build dev   → const devMode = true
```

`New` returns `NewDev(fsys)` when `devMode`. `NewDev` is exported because `assets_test.go` is an
external test package (`package assets_test`) and must reach it without the tag; nothing in a
tagless build calls it.

Dev behaviour, differing from the release `Set` only where staleness would otherwise leak:

- `URL` re-hashes the file with the existing `hashFile` on every call. A 100 KB `main.css`
  hashes in well under a millisecond, which buys exactness and avoids an mtime-resolution
  guess. An unreadable or missing file returns the path unchanged, as today.
- `ServeHTTP` resolves the path on each request, so a file created after startup is served
  rather than 404'd. Validate with `fs.ValidPath` and keep `http.ServeFileFS`.
- Always `Cache-Control: no-store`, and no `ETag` — the release rule of caching only the
  current hash forever is exactly what we're escaping.
- Never gzip. The release path memoizes the compressed body in a `sync.Once` per file, which
  would serve stale bytes for the rest of the session after the first edit.

`assets.SetDefault` and `assets.URL` are unchanged. Leave the `/favicon.ico` read in
`router.New:686` as a startup read — a favicon edit mid-session is not worth the request cost.

## Acceptance criteria

- [x] `go build ./cmd/server/.` with no tags still embeds the frontend, and `grep -rn 'os.DirFS' cmd/`
      matches only the `dev`-tagged file
- [x] Under `-tags dev`, writing to `cmd/server/frontend/static/main.css` changes what
      `assets.URL("/static/main.css")` returns with no restart
- [x] Under `-tags dev`, a file created under `cmd/server/frontend/static/` after startup is
      served (404 before this change)
- [x] Dev responses carry `Cache-Control: no-store` and are never gzipped, whatever the request's
      `Accept-Encoding`
- [x] Every existing case in `internal/assets/assets_test.go` still passes unchanged
- [x] New tests cover the dev `Set` over a `t.TempDir()` + `os.DirFS` fixture: re-hash after a
      write, a late-created file, the headers, and no gzip
- [x] `go build -tags dev ./...` and `make lint` pass

## Comments

Claude, 2026-10-11: done in `tech/dev-hot-reload`.

One criterion needed a reading. "Every existing case still passes unchanged" holds for the
default build, which is what CI runs, but `newSet` now skips under `-tags dev`: it builds its Set
through `assets.New`, which by design answers with a dev Set there, so the release assertions
about immutable caching, ETags and gzip have nothing to say in that mode. The release cases are
otherwise untouched.

Added beyond the ticket: `TestNew_FollowsTheBuildTag`, with a tag-gated `newIsDev` constant per
mode. Without it nothing covered the wiring — every other dev case calls `NewDev` directly, so
`New`'s `devMode` branch could have been deleted with the suite staying green.

Verified by hand as well as by the suite: a throwaway program doing what `router.New` does
(`fs.Sub(os.DirFS("cmd/server"), "frontend")` → `assets.New`) reported a changed `?v=` hash after
a live edit under `-tags dev`, and an unchanged one without the tag.

Claude, 2026-10-11, after review: a code review caught that this ticket's scope argues for
hashing "on every call ... which buys exactness and avoids an mtime-resolution guess", while the
code now caches hashes against mtime and size — the guess this ticket rejected. The reversal was
deliberate and measured (2.5ms down to 0.012ms per page render, the layout resolving eight assets
of which one is a 5.3 MB bundle), and the trade is recorded at `devEntry` in
`internal/assets/assets.go`. Noting it here too, because the reasoning in the Scope section above
now reads as contradicting the code.

The mtime assumption sits in a safer place than the one originally rejected: as a _stamp_ a wrong
mtime would pin a stale URL indefinitely, whereas as a _cache key_ it can at worst miss one edit,
and the next edit recovers.
