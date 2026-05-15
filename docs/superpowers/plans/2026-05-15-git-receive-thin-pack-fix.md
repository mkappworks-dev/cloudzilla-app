# Git receive-pack: thin-pack handling — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make cloudzilla's git receive-pack (HTTP + SSH) accept thin packs produced by native git clients, by routing around a gap in go-git's filesystem-storer fast path. Preserve the existing "no git binary required" invariant.

**Architecture:** New `internal/gittransport` package exporting `WrapForReceive(storer.Storer) storer.Storer`. The wrapper hides `PackfileWriter()` via interface-embedding, forcing go-git's `packfile.UpdateObjectStorage` onto its `NewParserWithStorage(...)` branch, which can resolve external delta references via the storer. Two transports (`internal/handler/git_http.go` and `internal/ssh/server.go`) call `WrapForReceive` at the point where they construct `server.MapLoader`. Add light observability (pack size + duration) to surface slow-path latency.

**Tech Stack:** Go 1.23+, `github.com/go-git/go-git/v5 v5.19.0`, `github.com/go-git/go-billy/v5`, standard library `log/slog`. Vanilla Go testing (no testify).

**Spec:** [`docs/superpowers/specs/2026-05-15-git-receive-thin-pack-fix-design.md`](../specs/2026-05-15-git-receive-thin-pack-fix-design.md)

---

## File structure

```
internal/
├── handler/
│   └── git_http.go               ← modify: wrap storer + observability + TODO comment
├── ssh/
│   └── server.go                 ← modify: wrap storer + observability + TODO comment
└── gittransport/                 ← NEW package
    ├── storer.go                 ← WrapForReceive + receivePackStorer
    ├── storer_test.go            ← unit test (PackfileWriter is hidden)
    ├── storer_integration_test.go ← end-to-end thin-pack receive test
    ├── observe.go                ← ByteCounter (io.ReadCloser counting wrapper)
    └── observe_test.go           ← ByteCounter test
docs/
└── git-transport.md              ← modify: add "Thin packs" subsection
```

Responsibilities:

- `gittransport/storer.go` — *only* the storer wrapping primitive. No transport logic, no observability.
- `gittransport/observe.go` — *only* byte counting. Could grow other transport-observability helpers later, but for now this is its single concern.
- `gittransport/*_test.go` — pin the load-bearing behaviors with tests independent of the HTTP/SSH wiring.
- `handler/git_http.go`, `ssh/server.go` — unchanged in shape; they gain a wrap call, a counting reader, a slog line, and a TODO comment each.

---

## Task 1: Create `gittransport` package with `WrapForReceive` + invariant unit test

**Files:**
- Create: `internal/gittransport/storer.go`
- Create: `internal/gittransport/storer_test.go`

- [ ] **Step 1: Write the failing test**

Create `internal/gittransport/storer_test.go`:

```go
package gittransport_test

import (
	"testing"

	"github.com/go-git/go-billy/v5/memfs"
	"github.com/go-git/go-git/v5/plumbing/cache"
	"github.com/go-git/go-git/v5/plumbing/storer"
	"github.com/go-git/go-git/v5/storage/filesystem"

	"github.com/mkappworks-dev/cloudzilla-app/internal/gittransport"
)

// TestWrapForReceive_HidesPackfileWriter is the load-bearing invariant
// for this package. If the wrapped value ever satisfies PackfileWriter,
// go-git's UpdateObjectStorage will take its broken fast path and thin
// packs will fail again. See
// docs/superpowers/specs/2026-05-15-git-receive-thin-pack-fix-design.md.
func TestWrapForReceive_HidesPackfileWriter(t *testing.T) {
	underlying := filesystem.NewStorage(memfs.New(), cache.NewObjectLRUDefault())

	// Precondition: filesystem.Storage MUST implement PackfileWriter,
	// otherwise the wrapping is unnecessary and this whole package
	// should be reconsidered.
	if _, ok := any(underlying).(storer.PackfileWriter); !ok {
		t.Fatal("precondition violated: filesystem.Storage no longer implements storer.PackfileWriter")
	}

	wrapped := gittransport.WrapForReceive(underlying)

	if _, ok := wrapped.(storer.PackfileWriter); ok {
		t.Fatal("wrapped storer must NOT satisfy storer.PackfileWriter; interface-embedding wrapper is broken")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/gittransport/...`
Expected: build failure with `package github.com/mkappworks-dev/cloudzilla-app/internal/gittransport: no Go files in ...`

- [ ] **Step 3: Implement the package**

Create `internal/gittransport/storer.go`:

```go
// Package gittransport contains transport-layer adapters around go-git's
// receive-pack implementation.
//
// Today it exists for one purpose: routing the receive-pack path off
// go-git's filesystem fast path, which cannot resolve REF_DELTA references
// whose base lives outside the incoming thin pack. See
// docs/git-transport.md → "Thin packs" and
// docs/superpowers/specs/2026-05-15-git-receive-thin-pack-fix-design.md.
package gittransport

import "github.com/go-git/go-git/v5/plumbing/storer"

// receivePackStorer hides any methods of the wrapped storer that aren't
// part of storer.Storer itself. Critically it hides PackfileWriter(),
// which is what triggers the broken fast path inside
// packfile.UpdateObjectStorage.
//
// Interface-embedding (not struct-embedding) is load-bearing here:
// struct-embedding *filesystem.Storage would re-promote PackfileWriter
// and defeat the wrapper.
type receivePackStorer struct {
	storer.Storer
}

// WrapForReceive returns a view of s suitable for go-git's
// NewReceivePackSession that forces the slow-but-correct
// parse-with-storage pack-ingestion path.
func WrapForReceive(s storer.Storer) storer.Storer {
	return receivePackStorer{s}
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/gittransport/... -run TestWrapForReceive_HidesPackfileWriter -v`
Expected: `--- PASS: TestWrapForReceive_HidesPackfileWriter`

- [ ] **Step 5: Run `go vet` and confirm clean**

Run: `go vet ./internal/gittransport/...`
Expected: no output (no warnings).

- [ ] **Step 6: Commit**

```bash
git add internal/gittransport/storer.go internal/gittransport/storer_test.go
git commit -m "feat(gittransport): add WrapForReceive storer wrapper

Routes go-git's receive-pack off the filesystem fast path onto
NewParserWithStorage so REF_DELTAs against existing server objects
can be resolved. See docs/superpowers/specs/2026-05-15-git-receive-
thin-pack-fix-design.md."
```

---

## Task 2: Add integration test that proves end-to-end thin-pack receive succeeds

**Files:**
- Create: `internal/gittransport/storer_integration_test.go`

The test stands up two on-disk repos (server bare, client working), seeds them with a shared initial commit, makes a modification in the client, then pushes via an in-process transport whose server-side storer is wrapped by `WrapForReceive`. If the wrapper is correct, the push succeeds even though it's a thin pack.

- [ ] **Step 1: Write the test**

Create `internal/gittransport/storer_integration_test.go`:

```go
package gittransport_test

import (
	"os"
	"testing"
	"time"

	gogit "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/config"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/go-git/go-git/v5/plumbing/transport/client"
	"github.com/go-git/go-git/v5/plumbing/transport/server"

	"github.com/mkappworks-dev/cloudzilla-app/internal/gittransport"
)

// TestWrapForReceive_ThinPackReceiveSucceeds drives a real git push
// through go-git's client/server transport pair, with the server-side
// storer wrapped by WrapForReceive. Without the wrapper, the push
// fails with ErrReferenceDeltaNotFound. With the wrapper, it succeeds.
func TestWrapForReceive_ThinPackReceiveSucceeds(t *testing.T) {
	// 1. Bare server repo that will receive the push.
	serverDir := t.TempDir()
	serverRepo, err := gogit.PlainInit(serverDir, true)
	if err != nil {
		t.Fatalf("PlainInit server: %v", err)
	}

	// 2. Seed the server with one commit. Easiest path: init a non-bare
	// "seed" working dir, commit, and push via local file transport.
	seedDir := t.TempDir()
	seedRepo, err := gogit.PlainInit(seedDir, false)
	if err != nil {
		t.Fatalf("PlainInit seed: %v", err)
	}
	commitFile(t, seedRepo, "README.md", "hello\n", "initial commit")
	if _, err := seedRepo.CreateRemote(&config.RemoteConfig{
		Name: "origin",
		URLs: []string{serverDir},
	}); err != nil {
		t.Fatalf("CreateRemote seed: %v", err)
	}
	if err := seedRepo.Push(&gogit.PushOptions{}); err != nil {
		t.Fatalf("seed push: %v", err)
	}

	// 3. Clone server → client; client now has the initial commit.
	clientDir := t.TempDir()
	clientRepo, err := gogit.PlainClone(clientDir, false, &gogit.CloneOptions{
		URL: serverDir,
	})
	if err != nil {
		t.Fatalf("PlainClone client: %v", err)
	}

	// 4. Modify the existing file in client — this produces a delta on push.
	commitFile(t, clientRepo, "README.md", "hello\nmodified\n", "modify readme")

	// 5. Install a custom protocol whose server-side storer is wrapped.
	//    The "wrapped" URL scheme is registered process-wide; remove it
	//    after the test to avoid leaking into other tests in the package.
	const scheme = "wrappedtest"
	wrappedTransport := server.NewServer(server.MapLoader{
		scheme + "://" + serverDir: gittransport.WrapForReceive(serverRepo.Storer),
	})
	client.InstallProtocol(scheme, wrappedTransport)
	t.Cleanup(func() { client.InstallProtocol(scheme, nil) })

	if _, err := clientRepo.CreateRemote(&config.RemoteConfig{
		Name: "wrapped",
		URLs: []string{scheme + "://" + serverDir},
	}); err != nil {
		t.Fatalf("CreateRemote wrapped: %v", err)
	}

	// 6. Push via the wrapped transport. This is the operation that
	//    previously failed with ErrReferenceDeltaNotFound on cloudzilla.
	if err := clientRepo.Push(&gogit.PushOptions{RemoteName: "wrapped"}); err != nil {
		t.Fatalf("push through wrapped transport: %v", err)
	}

	// 7. Verify the server's HEAD advanced and the second commit is present.
	head, err := serverRepo.Head()
	if err != nil {
		t.Fatalf("serverRepo.Head: %v", err)
	}
	commit, err := serverRepo.CommitObject(head.Hash())
	if err != nil {
		t.Fatalf("serverRepo.CommitObject: %v", err)
	}
	if commit.Message != "modify readme" {
		t.Fatalf("expected server HEAD to be 'modify readme', got %q", commit.Message)
	}
	// Parent must exist on the server (this object came from the initial seed,
	// so resolving it via the wrapped storer's slow path is required).
	if commit.NumParents() != 1 {
		t.Fatalf("expected 1 parent, got %d", commit.NumParents())
	}
	if _, err := commit.Parent(0); err != nil {
		t.Fatalf("parent commit not resolvable: %v", err)
	}
}

// commitFile writes content to repo's worktree at filename, stages it,
// and creates a commit. Returns the new commit's hash.
func commitFile(t *testing.T, repo *gogit.Repository, filename, content, message string) plumbing.Hash {
	t.Helper()

	wt, err := repo.Worktree()
	if err != nil {
		t.Fatalf("Worktree: %v", err)
	}
	fs := wt.Filesystem
	f, err := fs.OpenFile(filename, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		t.Fatalf("OpenFile: %v", err)
	}
	if _, err := f.Write([]byte(content)); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if err := f.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if _, err := wt.Add(filename); err != nil {
		t.Fatalf("Add: %v", err)
	}
	hash, err := wt.Commit(message, &gogit.CommitOptions{
		Author: &object.Signature{
			Name:  "Test",
			Email: "test@example.com",
			When:  time.Now(),
		},
	})
	if err != nil {
		t.Fatalf("Commit: %v", err)
	}
	return hash
}
```

- [ ] **Step 2: Run the integration test**

Run: `go test ./internal/gittransport/... -run TestWrapForReceive_ThinPackReceiveSucceeds -v`
Expected: `--- PASS: TestWrapForReceive_ThinPackReceiveSucceeds`

If the test FAILS with `reference delta not found` or similar, the wrapping isn't taking effect. Double-check that `gittransport.WrapForReceive(serverRepo.Storer)` is used in step 5 (not the unwrapped storer).

- [ ] **Step 3: Sanity-check the test reproduces the bug when the wrapper is removed**

Temporarily edit step 5 of the test to use `serverRepo.Storer` directly instead of `gittransport.WrapForReceive(serverRepo.Storer)`. Re-run:

Run: `go test ./internal/gittransport/... -run TestWrapForReceive_ThinPackReceiveSucceeds -v`
Expected: FAIL with an error containing `reference delta not found`.

This confirms the test actually exercises the bug path. Revert the test back to using `WrapForReceive` before continuing.

Run: `go test ./internal/gittransport/... -run TestWrapForReceive_ThinPackReceiveSucceeds -v`
Expected: PASS again.

- [ ] **Step 4: Run `go vet` and confirm clean**

Run: `go vet ./internal/gittransport/...`
Expected: no output.

- [ ] **Step 5: Commit**

```bash
git add internal/gittransport/storer_integration_test.go
git commit -m "test(gittransport): integration test for thin-pack receive

Drives go-git's client/server push pair against a wrapped server-side
storer. Without WrapForReceive this test reproduces ErrReferenceDeltaNotFound;
with WrapForReceive it passes."
```

---

## Task 3: Add `ByteCounter` helper for observability

**Files:**
- Create: `internal/gittransport/observe.go`
- Create: `internal/gittransport/observe_test.go`

`ByteCounter` wraps an `io.ReadCloser`, tracks bytes read atomically, and propagates `Close`. Used by HTTP and SSH receive-pack handlers to record `pack_bytes` for the observability log line.

- [ ] **Step 1: Write the failing test**

Create `internal/gittransport/observe_test.go`:

```go
package gittransport_test

import (
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/mkappworks-dev/cloudzilla-app/internal/gittransport"
)

func TestByteCounter_TracksReadsAndClose(t *testing.T) {
	const payload = "hello, thin pack receiver\n"
	src := io.NopCloser(strings.NewReader(payload))

	c := gittransport.NewByteCounter(src)

	if got := c.Bytes(); got != 0 {
		t.Fatalf("Bytes() before Read: want 0, got %d", got)
	}

	buf := make([]byte, 8)
	n, err := c.Read(buf)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if n != 8 {
		t.Fatalf("Read: want n=8, got %d", n)
	}
	if got := c.Bytes(); got != 8 {
		t.Fatalf("Bytes() after first Read: want 8, got %d", got)
	}

	rest, err := io.ReadAll(c)
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	if want := int64(len(payload)); c.Bytes() != want {
		t.Fatalf("Bytes() after ReadAll: want %d, got %d", want, c.Bytes())
	}
	if string(buf[:n])+string(rest) != payload {
		t.Fatalf("payload mismatch: want %q, got %q", payload, string(buf[:n])+string(rest))
	}

	if err := c.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
}

func TestByteCounter_ClosePropagatesError(t *testing.T) {
	sentinel := errors.New("close failed")
	src := &errCloser{Reader: strings.NewReader(""), closeErr: sentinel}

	c := gittransport.NewByteCounter(src)
	if err := c.Close(); !errors.Is(err, sentinel) {
		t.Fatalf("Close: want %v, got %v", sentinel, err)
	}
}

type errCloser struct {
	io.Reader
	closeErr error
}

func (e *errCloser) Close() error { return e.closeErr }
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/gittransport/... -run TestByteCounter -v`
Expected: build failure (`undefined: gittransport.NewByteCounter`, `undefined: gittransport.ByteCounter`).

- [ ] **Step 3: Implement `ByteCounter`**

Create `internal/gittransport/observe.go`:

```go
package gittransport

import (
	"io"
	"sync/atomic"
)

// ByteCounter wraps an io.ReadCloser and atomically tracks the total
// number of bytes successfully read so far. Close is delegated to the
// underlying reader.
//
// Used by the receive-pack handlers to surface pack size in the
// post-push observability log line.
type ByteCounter struct {
	r io.ReadCloser
	n int64
}

// NewByteCounter wraps r. The returned counter exposes a running byte
// total via Bytes() and forwards Close() to r.
func NewByteCounter(r io.ReadCloser) *ByteCounter {
	return &ByteCounter{r: r}
}

func (c *ByteCounter) Read(p []byte) (int, error) {
	n, err := c.r.Read(p)
	if n > 0 {
		atomic.AddInt64(&c.n, int64(n))
	}
	return n, err
}

func (c *ByteCounter) Close() error { return c.r.Close() }

// Bytes returns the total number of bytes read so far. Safe to call
// concurrently with Read.
func (c *ByteCounter) Bytes() int64 { return atomic.LoadInt64(&c.n) }
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/gittransport/... -run TestByteCounter -v`
Expected: both subtests pass.

- [ ] **Step 5: Run the full gittransport suite + vet**

Run: `go test ./internal/gittransport/... && go vet ./internal/gittransport/...`
Expected: all tests pass, no vet output.

- [ ] **Step 6: Commit**

```bash
git add internal/gittransport/observe.go internal/gittransport/observe_test.go
git commit -m "feat(gittransport): add ByteCounter for receive-pack observability

Tracks pack size via an atomic counter. Used by both HTTP and SSH
receive-pack handlers in a follow-up commit."
```

---

## Task 4: Wire `WrapForReceive` + observability into HTTP receive-pack

**Files:**
- Modify: `internal/handler/git_http.go` (around lines 282-330)

Three changes at this site:
1. Wrap `gitRepo.Storer` with `gittransport.WrapForReceive(...)` before constructing `MapLoader`.
2. Wrap the (possibly gzip-decoded) request body with `gittransport.NewByteCounter(...)` so we can report `pack_bytes`.
3. After a successful `sess.ReceivePack(...)`, emit a structured `slog.Info` line. Add a `TODO(thin-pack)` comment above the wrap call.

- [ ] **Step 1: Read the current handler around the receive-pack call**

Run: `sed -n '270,340p' internal/handler/git_http.go`
Expected: shows the gzip-handling block, `transport.NewEndpoint`, `server.NewServer`, and the `sess.ReceivePack(...)` call.

- [ ] **Step 2: Apply the edit**

Use Edit to replace the existing block (the exact `old_string` must match what's currently in the file — read first if unsure). The change has three discrete parts; show them together for review:

**Add the import** at the top of the file (in the existing import block, alphabetically):

```go
"github.com/mkappworks-dev/cloudzilla-app/internal/gittransport"
```

(Confirm `time` and `log/slog` are already imported by `git_http.go`; if not, add `"time"` and ensure `"log/slog"` is present.)

**Replace the body-decoding-through-receive-pack block.** Locate:

```go
	// Handle gzip-encoded bodies
	body := io.Reader(r.Body)
	if r.Header.Get("Content-Encoding") == "gzip" {
		gr, err := gzip.NewReader(body)
		if err != nil {
			http.Error(w, "failed to decompress", http.StatusBadRequest)
			return
		}
		defer gr.Close()
		body = gr
	}

	ep, err := transport.NewEndpoint("/")
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	srv := server.NewServer(server.MapLoader{ep.String(): gitRepo.Storer})
```

Replace with:

```go
	// Handle gzip-encoded bodies
	body := io.ReadCloser(r.Body)
	if r.Header.Get("Content-Encoding") == "gzip" {
		gr, err := gzip.NewReader(r.Body)
		if err != nil {
			http.Error(w, "failed to decompress", http.StatusBadRequest)
			return
		}
		defer gr.Close()
		body = io.NopCloser(gr)
	}

	// Count incoming pack bytes so we can report pack_bytes in the
	// post-receive observability log line. The counter wraps the
	// post-decompression body, so the figure reflects the actual pack
	// payload rather than the gzipped wire bytes.
	counter := gittransport.NewByteCounter(body)
	body = counter

	ep, err := transport.NewEndpoint("/")
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	// TODO(thin-pack): WrapForReceive routes receive-pack onto go-git's
	// parsed-storage path. Required because filesystem.Storage's
	// PackfileWriter fast path can't resolve REF_DELTAs whose base lives
	// outside the incoming pack. See docs/git-transport.md → "Thin packs"
	// and docs/superpowers/specs/2026-05-15-git-receive-thin-pack-fix-design.md.
	srv := server.NewServer(server.MapLoader{
		ep.String(): gittransport.WrapForReceive(gitRepo.Storer),
	})
```

Note that `body` changed type from `io.Reader` to `io.ReadCloser` — every subsequent consumer of `body` in this function (the `req.Decode(body)` call) requires only `io.Reader`, so the change is upward-compatible.

**Add timing + log line.** Locate the existing call site:

```go
	status, err := sess.ReceivePack(r.Context(), req)
	if err != nil {
		slog.Error("git-http: receive-pack failed", "owner", owner, "repo", repoName, "error", err)
		http.Error(w, "receive-pack failed: "+err.Error(), http.StatusInternalServerError)
		return
	}
```

Replace with:

```go
	start := time.Now()
	status, err := sess.ReceivePack(r.Context(), req)
	if err != nil {
		slog.Error("git-http: receive-pack failed", "owner", owner, "repo", repoName, "error", err)
		http.Error(w, "receive-pack failed: "+err.Error(), http.StatusInternalServerError)
		return
	}
	slog.Info("git-http: receive-pack complete",
		"owner", owner,
		"repo", repoName,
		"pusher", gu.Username,
		"commands", len(req.Commands),
		"pack_bytes", counter.Bytes(),
		"duration_ms", time.Since(start).Milliseconds(),
	)
```

- [ ] **Step 3: Build the package**

Run: `go build ./internal/handler/...`
Expected: no output (clean build).

- [ ] **Step 4: Run the handler test suite**

Run: `go test ./internal/handler/...`
Expected: existing tests still pass. (They don't exercise receive-pack end-to-end; this is a smoke check that the imports and call shape are still well-formed.)

- [ ] **Step 5: Run `go vet`**

Run: `go vet ./internal/handler/...`
Expected: no output.

- [ ] **Step 6: Manual verification via real git push**

Start the dev server in a separate terminal (`make dev`). In a clone whose pushes previously triggered the bug, **remove** the client-side workaround:

```bash
git config --local --unset pack.depth
git config --local --unset pack.window
```

Make any small change, commit, and push:

```bash
echo "test" >> README.md
git add README.md
git commit -m "test thin-pack fix"
git push
```

Expected on the client: push succeeds, no `RPC failed` or `HTTP 500` lines.
Expected in the server log: an `INFO git-http: receive-pack complete` line with non-zero `pack_bytes` and a small `duration_ms`.

- [ ] **Step 7: Commit**

```bash
git add internal/handler/git_http.go
git commit -m "feat(http): route receive-pack through gittransport.WrapForReceive

Pushes from native git clients producing thin packs now succeed
(previously failed with 'reference delta not found' from go-git's
filesystem-storer fast path). Also emit a structured slog.Info line
with pack_bytes and duration_ms for slow-path visibility."
```

---

## Task 5: Wire `WrapForReceive` + observability into SSH receive-pack

**Files:**
- Modify: `internal/ssh/server.go` (around line 322 for the wrap; the receive-pack block inside `execGitService` from line 359 for observability)

Same three changes as Task 4, adapted to the SSH handler's shape. The SSH session reader is the byte source we wrap; the `req.Packfile` field on `ReferenceUpdateRequest` would also work but wrapping the session is simpler and symmetric with HTTP.

- [ ] **Step 1: Read the current handler**

Run: `sed -n '313,400p' internal/ssh/server.go`
Expected: shows `execGitService`, including the `server.NewServer(...)` call at line ~322 and the receive-pack branch.

- [ ] **Step 2: Apply the edits**

**Add the import** alongside existing imports in `internal/ssh/server.go`:

```go
"github.com/mkappworks-dev/cloudzilla-app/internal/gittransport"
```

(Confirm `time` and `log/slog` are already imported in `ssh/server.go`; if not, add them.)

**Replace the storer-wiring block.** Locate (~line 320-322):

```go
	// MapLoader is keyed on ep.String() (e.g. "file:///"), not the input to NewEndpoint.
	srv := server.NewServer(server.MapLoader{ep.String(): gitRepo.Storer})
```

Replace with:

```go
	// MapLoader is keyed on ep.String() (e.g. "file:///"), not the input to NewEndpoint.
	//
	// TODO(thin-pack): WrapForReceive routes receive-pack onto go-git's
	// parsed-storage path. Required because filesystem.Storage's
	// PackfileWriter fast path can't resolve REF_DELTAs whose base lives
	// outside the incoming pack. See docs/git-transport.md → "Thin packs"
	// and docs/superpowers/specs/2026-05-15-git-receive-thin-pack-fix-design.md.
	srv := server.NewServer(server.MapLoader{
		ep.String(): gittransport.WrapForReceive(gitRepo.Storer),
	})
```

**Wrap the session reader in the receive-pack branch.** Locate (~line 378-394):

```go
		req := packp.NewReferenceUpdateRequest()
		if err := req.Decode(session); err != nil {
			return nil, fmt.Errorf("decode receive-pack request: %w", err)
		}

		status, err := sess.ReceivePack(context.Background(), req)
		if err != nil {
			return nil, fmt.Errorf("receive-pack: %w", err)
		}

		if status != nil {
			if err := status.Encode(session); err != nil {
				return nil, fmt.Errorf("encode receive-pack status: %w", err)
			}
		}

		return req.Commands, nil
```

Replace with:

```go
		// Count the bytes flowing in from the SSH session so we can
		// report pack_bytes in the post-receive observability log.
		counter := gittransport.NewByteCounter(io.NopCloser(session))

		req := packp.NewReferenceUpdateRequest()
		if err := req.Decode(counter); err != nil {
			return nil, fmt.Errorf("decode receive-pack request: %w", err)
		}

		start := time.Now()
		status, err := sess.ReceivePack(context.Background(), req)
		if err != nil {
			return nil, fmt.Errorf("receive-pack: %w", err)
		}
		slog.Info("ssh: receive-pack complete",
			"owner", ownerName,
			"repo", repoName,
			"pusher", pusherName,
			"commands", len(req.Commands),
			"pack_bytes", counter.Bytes(),
			"duration_ms", time.Since(start).Milliseconds(),
		)

		if status != nil {
			if err := status.Encode(session); err != nil {
				return nil, fmt.Errorf("encode receive-pack status: %w", err)
			}
		}

		return req.Commands, nil
```

The slog.Info fields above mirror the HTTP handler's log line (Task 4) exactly: `owner`, `repo`, `pusher`, `commands`, `pack_bytes`, `duration_ms`. Symmetry is intentional — operators should be able to grep one shape across both transports.

Note: `execGitService` does not currently take owner / repo / pusher identifiers as parameters — it only has `*gogit.Repository`. There are two viable shapes here:

(a) **Reach for context already available.** The calling site (`handleSSHGitService` / similar around line 240 in `server.go`) has `repo *model.Repository` and `pusherName string` (defined at line 188, set at line 209) in scope. Adding three parameters to `execGitService` is the minimal change and matches the HTTP handler's structure. **Use this approach.**

Replace the function signature at line 315:
```go
func (s *Server) execGitService(session ssh.Session, svc string, gitRepo *gogit.Repository) ([]*packp.Command, error) {
```
with:
```go
func (s *Server) execGitService(session ssh.Session, svc string, gitRepo *gogit.Repository, ownerName, repoName, pusherName string) ([]*packp.Command, error) {
```

Find the caller of `execGitService` (search: `s.execGitService(`; there is exactly one, around line 240) and add the three arguments using the in-scope locals:

```go
	commands, err := s.execGitService(session, gitCmd, gitRepo, repo.OwnerName, repo.Name, pusherName)
```

- [ ] **Step 3: Build the package**

Run: `go build ./internal/ssh/...`
Expected: no output.

- [ ] **Step 4: Run the SSH test suite (if present) and full vet**

Run: `go test ./internal/ssh/... && go vet ./internal/ssh/...`
Expected: all tests pass (or no tests in the package — that's also acceptable here), no vet output.

- [ ] **Step 5: Manual SSH verification**

If you have SSH key setup against the local dev server (cloudzilla SSH listens on `:2222`), repeat the push test from Task 4 step 6 via SSH:

```bash
git remote set-url origin ssh://git@localhost:2222/malithaccimt/<repo>.git
# ... make a small commit ...
git push
```

Expected on the client: push succeeds.
Expected in the server log: an `INFO ssh: receive-pack complete` line.

If SSH isn't currently set up in your environment, skip this step — Task 2's integration test covers the storer-wrap behavior; manual SSH verification is a nicety, not a gate.

- [ ] **Step 6: Commit**

```bash
git add internal/ssh/server.go
git commit -m "feat(ssh): route receive-pack through gittransport.WrapForReceive

Symmetric change to the HTTP handler: wraps the server-side storer
and emits the same six-field observability log line
(owner, repo, pusher, commands, pack_bytes, duration_ms)."
```

---

## Task 6: Document "Thin packs" handling

**Files:**
- Modify: `docs/git-transport.md`

Add a new subsection so future maintainers don't have to re-derive the rationale.

- [ ] **Step 1: Pick the insertion point**

Run: `grep -n '^## ' docs/git-transport.md`
Expected: a list of `##` headings. Insert "Thin packs" as a `##` subsection between "Git HTTP Smart Protocol" and "SSH Server" (or near the end of the receive-pack discussion — wherever flows best for the reader).

- [ ] **Step 2: Add the section**

Insert the following markdown block at the chosen location:

```markdown
## Thin packs

Native `git push` produces *thin packs* by default — packs whose objects may be encoded as deltas against base objects already on the server. The server is expected to "fix" the thin pack by appending the missing bases.

Cloudzilla's transport is pure-Go and uses `go-git`'s `server.ReceivePack`. go-git's filesystem-backed storer takes a fast path inside `packfile.UpdateObjectStorage` that runs the pack parser **without** access to the storage, so REF_DELTAs whose base is only on disk (not in the pack) cannot be resolved. The receive fails with `reference delta not found` and a 500 is returned to the client.

To work around this without giving up the "no git binary required" invariant, both transports route the storer through `gittransport.WrapForReceive` before handing it to `server.NewServer`. The wrapper hides the storer's `PackfileWriter` method via interface-embedding, which forces `UpdateObjectStorage` onto its slower `NewParserWithStorage` branch. That parser *can* see the storage, so external delta bases are resolved correctly.

**Trade-off:** received objects land loose under `objects/xx/yyy…` rather than packed. Native git treats this as routine and `git gc` reclaims them; cloudzilla currently has no equivalent. Loose-object GC is a tracked follow-up.

**Observability:** each successful receive-pack emits an `INFO ... receive-pack complete` log line with `pack_bytes` and `duration_ms`. Use this to spot pushes that take seconds rather than tens of milliseconds.

**See also:** [`docs/superpowers/specs/2026-05-15-git-receive-thin-pack-fix-design.md`](./superpowers/specs/2026-05-15-git-receive-thin-pack-fix-design.md).
```

- [ ] **Step 3: Verify the doc still renders**

Run: `head -100 docs/git-transport.md | head -30 && echo '...' && grep -c '^## ' docs/git-transport.md`
Expected: section headings are in a sensible order and the count went up by 1 from before.

- [ ] **Step 4: Commit**

```bash
git add docs/git-transport.md
git commit -m "docs(git-transport): document thin-pack handling

Captures why receive-pack routes the storer through
gittransport.WrapForReceive, the trade-off (loose objects vs.
correctness), and the observability surface."
```

---

## Final verification

After all six tasks land, run the full repo's checks:

- [ ] **Full test suite**

Run: `go test ./...`
Expected: all tests pass. Particular interest in `./internal/gittransport/...` and `./internal/handler/...`.

- [ ] **Full vet**

Run: `go vet ./...`
Expected: no output.

- [ ] **Lint**

Run: `make lint`
Expected: passes per cloudzilla's existing lint configuration.

- [ ] **Real-world push smoke test** (if not already done in Tasks 4 / 5)

Push to a non-empty repository via HTTP and via SSH (if available); confirm both succeed and produce the new `receive-pack complete` log lines.

---

## Notes for the implementer

- All snippets in this plan have been hand-checked against the current state of `internal/handler/git_http.go`, `internal/ssh/server.go`, and `go-git v5.19.0`. If `go-git` has been bumped before you start, re-verify the unit test's precondition (`filesystem.Storage` still implements `storer.PackfileWriter`) and the type assertion path in `packfile/common.go`. If those have changed, this plan needs a revisit before continuing.
- Do not delete the `pack.depth=0 / pack.window=0` workaround from any of your own local clones until Task 4 step 6 has succeeded against a clone with delta compression enabled. That's the test that proves the production change actually works.
- The `// TODO(thin-pack):` comments are intentional and *should remain* in the code permanently — they point future maintainers to the rationale. Do not strip them in a later "cleanup" pass.
