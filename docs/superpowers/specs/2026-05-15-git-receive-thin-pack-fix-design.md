# 2026-05-15 — Git receive-pack: thin-pack handling

**Status:** Design approved, implementation pending
**Branch convention:** `tech/git-thin-pack-fix`
**Affected subsystem:** Git transport (HTTP + SSH)

---

## Problem

Pushing to a non-empty repository on a cloudzilla instance fails with HTTP 500 whenever the client's send-pack produces a thin pack — which is git's default behavior for any push that can delta-compress against objects the server already has. The native git client output looks like:

```
Total 3 (delta 2), reused 0 (delta 0), pack-reused 0 (from 0)
error: RPC failed; HTTP 500
fatal: the remote end hung up unexpectedly
```

Server log shows:

```
ERROR git-http: receive-pack failed owner=<owner> repo=<repo> error="reference delta not found"
```

The pack and the ref both fail to land on the server, so the push is effectively dropped.

### Root cause

go-git's filesystem-backed storer (`*filesystem.Storage`) implements `storer.PackfileWriter`. When `rpSession.ReceivePack` calls `packfile.UpdateObjectStorage(s.storer, r)`, the type assertion `s.(storer.PackfileWriter)` succeeds and the call takes a fast path that ends in `dotgit.PackWriter`. That writer spawns a goroutine running:

```go
w.parser, err = packfile.NewParser(s, w.writer)
```

Note `NewParser`, not `NewParserWithStorage` — the parser building the `.idx` sidecar has **no storage backend**. When it encounters an `OBJ_REF_DELTA` whose base SHA isn't found inside the same pack, it creates an `ExternalRef` placeholder and later errors with `ErrReferenceDeltaNotFound` from `parser.go:402`. The error propagates through `waitBuildIndex` → `PackWriter.Close` → `WritePackfileToObjectStorage` → `ReceivePack` → HTTP 500. The temp pack file is cleaned up.

The irony: the storer **could** resolve the base (it sits in `objects/`), but the pack-indexing pass is structurally decoupled from object lookup inside the fast path.

### Native-git equivalent

Native `git-receive-pack` accepts thin packs because it shells out to `git index-pack --fix-thin`, which appends missing delta bases to the pack before writing the `.idx`. cloudzilla cannot use this approach without violating the "no git binary required" invariant in `CLAUDE.md`.

### Current workaround (client-side)

```bash
git config --local pack.depth 0
git config --local pack.window 0
```

This disables delta compression in pack-objects so no delta references appear in the pushed pack. Confirmed working but every consumer of cloudzilla must apply it per-clone, which is unacceptable for production.

---

## Constraints

- **Pure-Go:** No native `git` binary at runtime. This is an architectural property documented in `CLAUDE.md` and must be preserved.
- **Both transports affected:** HTTP (`internal/handler/git_http.go`) and SSH (`internal/ssh/server.go`) both call `sess.ReceivePack` and both fail identically. The fix must apply to both.
- **No upstream fork:** Do not maintain a forked go-git or a `replace` directive in `go.mod`. Maintenance burden exceeds the value of a route-around.

---

## Decision

**Approach A — Interface-embedding storer wrapper.**

Introduce a new package `internal/gittransport` exporting `WrapForReceive(storer.Storer) storer.Storer`. The wrapper hides `PackfileWriter()` via interface-embedding, causing go-git's type assertion to fail and routing `UpdateObjectStorage` onto its `NewParserWithStorage(scanner, s)` branch. That parser **does** receive the storage and can resolve external delta references through `p.storage.EncodedObject(...)`.

### Alternatives considered

| Approach | Verdict |
|---|---|
| Custom `server.Loader` instead of `MapLoader` | Same idea, more code. Rejected unless we add more receive-pack glue later. |
| Fork/vendor go-git to fix the parser at the source | Real fix but ongoing maintenance tax. Rejected. |
| Shell to `git index-pack --fix-thin` | Robust, but breaks the pure-Go invariant. Rejected per constraint above. |
| Continue requiring client-side `pack.window=0` | Not viable for production. Rejected. |

---

## Design

### Package layout

```
internal/
├── handler/
│   └── git_http.go               ← imports gittransport.WrapForReceive
├── ssh/
│   └── server.go                 ← imports gittransport.WrapForReceive
└── gittransport/                 ← NEW
    ├── storer.go                 ← WrapForReceive + receivePackStorer
    └── storer_test.go            ← thin-pack receive test
```

### Wrapper API

```go
// Package gittransport contains transport-layer adapters around go-git's
// receive-pack implementation. Today it exists for one purpose: routing
// the receive path off go-git's filesystem fast path, which cannot resolve
// REF_DELTA references whose base lives outside the incoming thin pack.
package gittransport

import "github.com/go-git/go-git/v5/plumbing/storer"

// receivePackStorer hides any methods of the wrapped storer that aren't
// part of storer.Storer itself. Critically, it hides PackfileWriter(),
// which is what triggers the broken fast path inside
// packfile.UpdateObjectStorage. Interface-embedding (not struct-embedding)
// is load-bearing: struct-embedding *filesystem.Storage would re-promote
// PackfileWriter and defeat the wrapper.
type receivePackStorer struct {
    storer.Storer
}

// WrapForReceive returns a view of s suitable for go-git's
// NewReceivePackSession that forces the slow-but-correct
// parse-with-storage pack-ingestion path. See docs/git-transport.md →
// "Thin packs" for the full rationale.
func WrapForReceive(s storer.Storer) storer.Storer {
    return receivePackStorer{s}
}
```

### Call-site changes

```go
// internal/handler/git_http.go — around line 299
- srv := server.NewServer(server.MapLoader{ep.String(): gitRepo.Storer})
+ srv := server.NewServer(server.MapLoader{
+     ep.String(): gittransport.WrapForReceive(gitRepo.Storer),
+ })

// internal/ssh/server.go — around line 360, identical change
```

Both sites grow an `// TODO(thin-pack)` reference comment pointing to this spec and the `docs/git-transport.md` subsection (see Documentation below).

### Data flow

**Before (broken fast path):**

```
client send-pack → POST /git-receive-pack
  → sess.ReceivePack → writePackfile → UpdateObjectStorage
    → s.(PackfileWriter) ✓ → filesystem.PackWriter
      ├─ io.Copy → temp pack file
      └─ buildIndex: NewParser(s, w.writer)        ← no storage
                     └─> ErrReferenceDeltaNotFound  ✗
```

**After (slow-but-correct parsed-storage path):**

```
client send-pack → POST /git-receive-pack
  → WrapForReceive(gitRepo.Storer)                ← NEW
  → sess.ReceivePack → writePackfile → UpdateObjectStorage
    → s.(PackfileWriter) ✗ → fall through
      → NewParserWithStorage(scanner, s)          ← has storage
        ├─ indexObjects: builds objectInfo map
        ├─ for OBJ_REF_DELTA with external base:
        │    p.get(parent) → storage.EncodedObject(SHA1)  ✓
        └─ resolveDeltas → SetEncodedObject(obj)
```

### Observable side-effects of the slow path

1. **Objects land loose** (`objects/xx/yyy…`) instead of inside a pack. Native git treats this as routine and `git gc` rolls them up. cloudzilla does not currently run any GC equivalent — see *Follow-ups* below.
2. **Push latency increases** for delta-heavy pushes. Order-of-magnitude estimate: ~10–50ms additional time for a typical 100-object push. Captured in the observability log so outliers are visible.

### Error handling

No new error surface. The slow path can still fail (corrupt pack, disk error during loose-object write, etc.), but those propagate through the existing `http.Error(w, "receive-pack failed: "+err.Error(), 500)` at `git_http.go:329` and the SSH equivalent. The wrapper does not introduce new error types or recovery logic — it only changes which go-git code path runs.

---

## Observability

Add a structured log line at the end of each successful receive-pack handler in both `git_http.go` and `ssh/server.go`. The fields are identical across transports; the message prefix differs (`git-http:` vs `ssh:`) so operators can grep for one transport without losing the other:

```go
// In git_http.go (HTTP receive-pack)
slog.Info("git-http: receive-pack complete",
    "owner", owner,
    "repo", repoName,
    "pusher", gu.Username,
    "commands", len(req.Commands),
    "pack_bytes", counter.Bytes(),     // tracked via gittransport.ByteCounter
    "duration_ms", time.Since(start).Milliseconds(),
)

// In ssh/server.go (SSH receive-pack) — same fields, different message prefix
slog.Info("ssh: receive-pack complete",
    "owner", ownerName,
    "repo", repoName,
    "pusher", pusherName,
    "commands", len(req.Commands),
    "pack_bytes", counter.Bytes(),
    "duration_ms", time.Since(start).Milliseconds(),
)
```

`pack_bytes` is captured by wrapping the post-decompression body (HTTP) or session reader (SSH) in `gittransport.ByteCounter` before handing it to go-git. The SSH handler threads `ownerName`, `repoName`, and `pusherName` into `execGitService` as new parameters so the log fields match what HTTP already has in scope.

This is the minimal instrumentation needed to spot a slow-path perf regression without standing up Prometheus or similar. If the field shows pushes taking seconds in production, that's our trigger to revisit the GC follow-up or consider a smarter wrapper.

The existing `ERROR git-http: receive-pack failed` log already captures the failure case, so no new error logging is needed.

---

## Testing

### Unit test (load-bearing invariant)

`internal/gittransport/storer_test.go` — assert that `WrapForReceive` produces a value that does **not** satisfy `storer.PackfileWriter`:

```go
func TestWrapForReceive_HidesPackfileWriter(t *testing.T) {
    fs := memfs.New()
    underlying := filesystem.NewStorage(fs, cache.NewObjectLRUDefault())

    if _, ok := any(underlying).(storer.PackfileWriter); !ok {
        t.Fatal("precondition: filesystem.Storage must implement PackfileWriter")
    }

    wrapped := gittransport.WrapForReceive(underlying)
    if _, ok := wrapped.(storer.PackfileWriter); ok {
        t.Fatal("wrapped storer must NOT satisfy PackfileWriter")
    }
}
```

If this test ever fails after a go-git bump, we know the wrapping primitive needs revisiting (e.g. go-git introduced a new method on `storer.Storer` itself that pulls `PackfileWriter` into the interface — unlikely but possible).

### Integration test (end-to-end thin-pack receive)

`internal/gittransport/storer_test.go` — set up an on-disk (filesystem-backed) repo with one initial commit, drive a full `NewReceivePackSession` against the wrapped storer using a `ReferenceUpdateRequest` whose packfile contains a `REF_DELTA` whose base is an object already in the repository (i.e. an explicitly thin pack), and assert no error plus that the ref moved.

The exact pack-construction mechanism is left to the implementation plan; the simplest reliable shape is likely to use go-git's own client `Push` against a process-local `NewServer(...)` so the wire-format matches what real clients produce.

### Manual verification

After implementation:

1. Revert the `pack.depth=0 / pack.window=0` config on a clone with existing history.
2. Make a small commit, `git push`.
3. Expect HTTP 200 and a successful contribution-heatmap update on next page load.

---

## Documentation

### `docs/git-transport.md`

Add a new subsection between "Authentication" and "Webhooks" titled **"Thin packs"**. Content covers:

- What thin packs are and why git sends them by default.
- That cloudzilla wraps the storer (via `gittransport.WrapForReceive`) to route around a gap in go-git's `filesystem.Storage.PackfileWriter` fast path.
- Trade-off: pushes land objects loose rather than packed; see the *Follow-ups* section below for the planned GC story.
- Link back to this spec.

### Inline TODO comments

At `git_http.go:~299` and `ssh/server.go:~360`:

```go
// TODO(thin-pack): WrapForReceive routes receive-pack onto go-git's
// parsed-storage path. Required because filesystem.Storage's PackfileWriter
// fast path can't resolve REF_DELTAs whose base lives outside the
// incoming pack. See docs/git-transport.md → "Thin packs" and
// docs/superpowers/specs/2026-05-15-git-receive-thin-pack-fix-design.md.
```

---

## Out of scope / follow-ups

These are intentionally **not** part of this fix:

1. **Loose-object garbage collection.** Loose objects accumulate over time. Native git solves this with periodic `git gc`. cloudzilla needs an equivalent — either an in-process goroutine that periodically calls go-git's `Repository.RepackObjects`, or shelling to native git in a worker (which would have to be explicitly carved out from the "no git binary" invariant). Tracked as a separate tech-debt item.
2. **Upstream fix in go-git.** Filing an issue (and possibly a PR) against `github.com/go-git/go-git` to make `dotgit.PackWriter.buildIndex` use `NewParserWithStorage` would benefit the entire go-git ecosystem. Not blocking this fix; should be opened as a follow-up.
3. **Performance optimization of the slow path.** If observability shows pushes routinely take seconds, we have options (custom pack-writer that preserves the fast-write but uses parsed-storage indexing; periodic background repack into packs). Defer until data justifies the work.

---

## Risks

| Risk | Likelihood | Mitigation |
|---|---|---|
| go-git changes `storer.Storer` to include `PackfileWriter` | Low | Unit test fails immediately; we adjust wrapping. |
| go-git slow path has a latent bug we haven't hit | Low–Medium | Integration test exercises it on every CI run. |
| Slow-path latency unacceptable for some workloads | Medium | Observability log surfaces it; follow-up perf work is scoped. |
| Loose objects fill disk | Medium (long-term) | Documented as known follow-up; not introduced by this fix (just made more frequent). |

---

## Rollout

No feature flag. The wrapper is correctness-preserving — every push that succeeded with the fast path will also succeed via the slow path (the fast path is a strict optimization of the slow path's input expectations). Pushes that *failed* with the fast path will now succeed.

Steps:

1. Land the `gittransport` package + tests.
2. Switch call sites in `git_http.go` and `ssh/server.go`.
3. Add `docs/git-transport.md` subsection and inline TODOs.
4. Manual verification per *Testing → Manual verification* above.
5. Merge to `main`.

No data migration. No config change. No client-side change required.
