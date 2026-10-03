# Repository Import Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make `/repos/import` work: a user gives an HTTP(S) Git URL (plus optional username + token) and Cloudzilla copies every branch and tag into a new repository, in the background.

**Architecture:** `ImportService` keeps jobs in memory and runs each clone in a goroutine. The clone goes into `ReposRoot/.import-tmp/<job id>` through a process-wide go-git HTTP client whose dialer refuses private addresses when the request context carries an import guard. On success `RepoService.CreateFromImport` claims the name, inserts the row and renames the clone into place. Handlers expose a JSON API (`/api/imports`) and two pages (`/repos/import`, `/repos/import/{id}`); the status page polls itself with htmx and redirects when the import finishes.

**Tech Stack:** Go 1.27, go-git v5.19.2, chi, Templ, htmx 4, Alpine.js, Tailwind v4, PostgreSQL.

**Spec:** `docs/superpowers/specs/2026-10-02-repo-import-design.md`

## Global Constraints

- No new Go module dependencies.
- Layering: stores → services → handlers. Handlers call services only.
- Comments record a *why* the code can't show, one line by default. No narration, no history.
- Edit `.templ` files, then `make generate-templ`. Never edit `*_templ.go` by hand. Text right after an element must not start with `for`, `if` or `switch` (templ parses it as Go).
- `git add` explicit paths only; never stage `.claude/`.
- Integration tests: `TEST_DATABASE_DSN='postgres://cloudzilla:cloudzilla@localhost:5432/cloudzilla_test?sslmode=disable'`. Without it they skip, so always run with it.
- Sources: `http` and `https` only; URLs with userinfo are rejected.
- Limits: 3 clones at once server-wide (`importConcurrency`), 5 queued or running per user (`importPerUserLimit`), finished jobs kept 1h (`importJobRetention`), timeout `import.timeout` (default `30m`), response bytes capped by `git.max_pack_bytes`.
- Config: `import.allow_local_networks` (default `false`, `CZ_IMPORT_ALLOW_LOCAL_NETWORKS`), `import.timeout` (default `30m`, `CZ_IMPORT_TIMEOUT`).
- Routes: `GET /repos/import`, `GET /repos/import/{id}`, `POST /api/imports`, `GET /api/imports/{id}`, all behind `authMW`.
- Status-page messages, verbatim:
  - `Repository not found, or it needs a username and token.`
  - `<host> resolves to a private network address. An administrator can allow this with import.allow_local_networks.`
  - `The source repository is empty — create a new repository instead.`
  - `The repository is larger than this instance's limit of <size>.`
  - `The import took longer than <timeout> and was stopped.`
  - `<owner>/<name> was created while the import ran.`
  - `You can no longer create repositories under <owner>.`
  - `The import failed.`

## File Map

| File | Responsibility |
| --- | --- |
| `internal/config/config.go` | `ImportConfig`, defaults |
| `internal/service/import_guard.go` | Per-import network guard and the process-wide go-git HTTP client |
| `internal/service/import_source.go` | URL validation, credentials, progress capture |
| `internal/service/import_clone.go` | Default-branch choice and the clone itself |
| `internal/service/repo_import.go` | `RepoService`: resolve the owner, check the name, publish the clone |
| `internal/service/import_service.go` | Jobs: start, run, status, failure messages |
| `internal/testutil/gitserver.go` | Read-only smart-HTTP git server and a seeded source repo, for tests |
| `internal/handler/repo_import_handler.go` | JSON API and page handlers |
| `internal/view/pages/repo_import.templ` | Import form, status page, status panel |
| `internal/view/pages/repo_new.templ` | Shares owner options and the visibility fieldset; fixes the import link |
| `docs/repo-import.md` | Subsystem doc |

---

### Task 1: Import config and network guard

**Files:**
- Modify: `internal/config/config.go`
- Modify: `internal/config/config_test.go`
- Create: `internal/service/import_guard.go`
- Test: `internal/service/import_guard_test.go` (package `service`)

**Interfaces:**
- Produces: `config.ImportConfig{AllowLocalNetworks bool; Timeout time.Duration}`, `config.Config.Import`.
- Produces (package `service`): `type importGuard struct{ allowLocal bool; maxBytes int64; ... }`, `withImportGuard(ctx, *importGuard) context.Context`, `(*importGuard).failure() error`, `type ImportBlockedError struct{ Host string }`, `var ErrImportTooLarge`, `installImportTransport()`, `newImportHTTPClient() *http.Client`, `blockedImportIP(net.IP) bool`.

- [ ] **Step 1: Write the failing config test**

Append to `internal/config/config_test.go` (add `"time"` to its imports):

```go
func TestLoad_ImportDefaultsAndEnv(t *testing.T) {
	cfg, err := config.Load("")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Import.AllowLocalNetworks || cfg.Import.Timeout != 30*time.Minute {
		t.Errorf("defaults: want allow_local_networks=false timeout=30m, got %+v", cfg.Import)
	}

	t.Setenv("CZ_IMPORT_ALLOW_LOCAL_NETWORKS", "true")
	t.Setenv("CZ_IMPORT_TIMEOUT", "5m")
	cfg, err = config.Load("")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !cfg.Import.AllowLocalNetworks || cfg.Import.Timeout != 5*time.Minute {
		t.Errorf("env: want allow_local_networks=true timeout=5m, got %+v", cfg.Import)
	}
}
```

- [ ] **Step 2: Run it to see it fail**

Run: `go test ./internal/config -run TestLoad_ImportDefaultsAndEnv`
Expected: FAIL, `cfg.Import undefined`.

- [ ] **Step 3: Add the config**

In `internal/config/config.go`, add the field to `Config` after `SMTP`:

```go
	Import   ImportConfig   `mapstructure:"import"`
```

Add the type after `GitConfig`:

```go
// ImportConfig holds repository import settings.
type ImportConfig struct {
	// Off by default, so a user can't make the server probe its own network.
	AllowLocalNetworks bool          `mapstructure:"allow_local_networks"`
	Timeout            time.Duration `mapstructure:"timeout"`
}
```

Add the defaults after `v.SetDefault("smtp.tls", false)`:

```go
	v.SetDefault("import.allow_local_networks", false)
	v.SetDefault("import.timeout", "30m")
```

- [ ] **Step 4: Run the config test**

Run: `go test ./internal/config`
Expected: PASS.

- [ ] **Step 5: Write the failing guard tests**

Create `internal/service/import_guard_test.go`:

```go
package service

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/mkappworks-dev/cloudzilla-app/internal/gittransport"
)

func TestBlockedImportIP(t *testing.T) {
	for _, tc := range []struct {
		ip      string
		blocked bool
	}{
		{"127.0.0.1", true}, {"10.1.2.3", true}, {"172.16.0.1", true}, {"192.168.1.1", true},
		{"169.254.169.254", true}, {"100.100.100.200", true}, {"0.1.2.3", true}, {"0.0.0.0", true},
		{"224.0.0.1", true}, {"::1", true}, {"fc00::1", true}, {"fe80::1", true}, {"::ffff:127.0.0.1", true},
		{"8.8.8.8", false}, {"140.82.112.3", false}, {"2606:4700:4700::1111", false},
	} {
		if got := blockedImportIP(net.ParseIP(tc.ip)); got != tc.blocked {
			t.Errorf("blockedImportIP(%s) = %v, want %v", tc.ip, got, tc.blocked)
		}
	}
}

func importTestServer(t *testing.T, body string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, body)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func importGet(ctx context.Context, url string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	return newImportHTTPClient().Do(req)
}

func TestImportClient_RefusesLoopbackUnderGuard(t *testing.T) {
	srv := importTestServer(t, "ok")
	g := &importGuard{}
	if resp, err := importGet(withImportGuard(context.Background(), g), srv.URL); err == nil {
		resp.Body.Close()
		t.Fatal("request to 127.0.0.1 succeeded under a guard")
	}
	var blocked *ImportBlockedError
	if !errors.As(g.failure(), &blocked) || blocked.Host != "127.0.0.1" {
		t.Errorf("failure() = %v, want ImportBlockedError for 127.0.0.1", g.failure())
	}
}

func TestImportClient_LeavesUnguardedRequestsAlone(t *testing.T) {
	srv := importTestServer(t, "ok")
	resp, err := importGet(context.Background(), srv.URL)
	if err != nil {
		t.Fatalf("unguarded request: %v", err)
	}
	resp.Body.Close()
}

func TestImportClient_AllowLocalNetworks(t *testing.T) {
	srv := importTestServer(t, "ok")
	resp, err := importGet(withImportGuard(context.Background(), &importGuard{allowLocal: true}), srv.URL)
	if err != nil {
		t.Fatalf("request with allow_local_networks: %v", err)
	}
	resp.Body.Close()
}

func TestImportClient_CapsResponseBytes(t *testing.T) {
	srv := importTestServer(t, strings.Repeat("x", 1000))
	g := &importGuard{allowLocal: true, maxBytes: 100}
	resp, err := importGet(withImportGuard(context.Background(), g), srv.URL)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer resp.Body.Close()
	if _, err := io.ReadAll(resp.Body); !errors.Is(err, gittransport.ErrPackTooLarge) {
		t.Errorf("read error = %v, want ErrPackTooLarge", err)
	}
	if !errors.Is(g.failure(), ErrImportTooLarge) {
		t.Errorf("failure() = %v, want ErrImportTooLarge", g.failure())
	}
}
```

- [ ] **Step 6: Run them to see them fail**

Run: `go test ./internal/service -run 'TestBlockedImportIP|TestImportClient'`
Expected: FAIL to compile, `undefined: blockedImportIP`.

- [ ] **Step 7: Implement the guard**

Create `internal/service/import_guard.go`:

```go
package service

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/url"
	"sync"
	"time"

	gitclient "github.com/go-git/go-git/v5/plumbing/transport/client"
	githttp "github.com/go-git/go-git/v5/plumbing/transport/http"
	"github.com/mkappworks-dev/cloudzilla-app/internal/gittransport"
)

var ErrImportTooLarge = errors.New("import exceeds the instance's size limit")

type ImportBlockedError struct{ Host string }

func (e *ImportBlockedError) Error() string {
	return e.Host + " resolves to a private network address"
}

// importGuard travels in the request context, so the process-wide go-git
// client applies it to one import alone. It records why it stopped a request
// because go-git wraps transport errors in ways errors.As can't always see through.
type importGuard struct {
	allowLocal bool
	maxBytes   int64

	mu          sync.Mutex
	blockedHost string
	tooLarge    bool
}

type importGuardKey struct{}

func withImportGuard(ctx context.Context, g *importGuard) context.Context {
	return context.WithValue(ctx, importGuardKey{}, g)
}

func importGuardFrom(ctx context.Context) *importGuard {
	g, _ := ctx.Value(importGuardKey{}).(*importGuard)
	return g
}

func (g *importGuard) failure() error {
	g.mu.Lock()
	defer g.mu.Unlock()
	switch {
	case g.blockedHost != "":
		return &ImportBlockedError{Host: g.blockedHost}
	case g.tooLarge:
		return ErrImportTooLarge
	}
	return nil
}

var blockedImportNets = []*net.IPNet{
	mustCIDR("0.0.0.0/8"),
	mustCIDR("100.64.0.0/10"), // CGNAT; some cloud metadata services live here
}

func mustCIDR(s string) *net.IPNet {
	_, n, err := net.ParseCIDR(s)
	if err != nil {
		panic(err)
	}
	return n
}

func blockedImportIP(ip net.IP) bool {
	if ip.IsLoopback() || ip.IsPrivate() || ip.IsUnspecified() || ip.IsLinkLocalUnicast() || ip.IsMulticast() {
		return true
	}
	for _, n := range blockedImportNets {
		if n.Contains(ip) {
			return true
		}
	}
	return false
}

// dial connects to the addresses it vetted, so a second DNS answer can't
// swap in a private one between the check and the connect.
func (g *importGuard) dial(ctx context.Context, d *net.Dialer, network, addr string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return nil, err
	}
	ips, err := net.DefaultResolver.LookupIPAddr(ctx, host)
	if err != nil {
		return nil, err
	}
	if len(ips) == 0 {
		return nil, &net.DNSError{Err: "no addresses", Name: host, IsNotFound: true}
	}
	for _, ip := range ips {
		if blockedImportIP(ip.IP) {
			g.mu.Lock()
			g.blockedHost = host
			g.mu.Unlock()
			return nil, &ImportBlockedError{Host: host}
		}
	}
	var lastErr error
	for _, ip := range ips {
		conn, err := d.DialContext(ctx, network, net.JoinHostPort(ip.IP.String(), port))
		if err == nil {
			return conn, nil
		}
		lastErr = err
	}
	return nil, lastErr
}

func newImportHTTPClient() *http.Client {
	dialer := &net.Dialer{Timeout: 30 * time.Second, KeepAlive: 30 * time.Second}
	tr := http.DefaultTransport.(*http.Transport).Clone()
	tr.DialContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
		if g := importGuardFrom(ctx); g != nil && !g.allowLocal {
			return g.dial(ctx, dialer, network, addr)
		}
		return dialer.DialContext(ctx, network, addr)
	}
	// Through a proxy the dial check would vet the proxy, not the source.
	tr.Proxy = func(req *http.Request) (*url.URL, error) {
		if importGuardFrom(req.Context()) != nil {
			return nil, nil
		}
		return http.ProxyFromEnvironment(req)
	}
	// A pooled connection skips the dial, and with it the check.
	tr.DisableKeepAlives = true
	return &http.Client{Transport: importRoundTripper{base: tr}}
}

type importRoundTripper struct{ base http.RoundTripper }

func (t importRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	resp, err := t.base.RoundTrip(req)
	if err != nil {
		return nil, err
	}
	if g := importGuardFrom(req.Context()); g != nil && g.maxBytes > 0 {
		resp.Body = &importBody{LimitedReadCloser: gittransport.NewLimitedReadCloser(resp.Body, g.maxBytes), guard: g}
	}
	return resp, nil
}

type importBody struct {
	*gittransport.LimitedReadCloser
	guard *importGuard
}

func (b *importBody) Read(p []byte) (int, error) {
	n, err := b.LimitedReadCloser.Read(p)
	if errors.Is(err, gittransport.ErrPackTooLarge) {
		b.guard.mu.Lock()
		b.guard.tooLarge = true
		b.guard.mu.Unlock()
	}
	return n, err
}

var importTransportOnce sync.Once

// go-git looks transports up in a process-wide table, so this client serves
// every go-git HTTP fetch; without a guard in the context it checks nothing.
func installImportTransport() {
	importTransportOnce.Do(func() {
		c := githttp.NewClient(newImportHTTPClient())
		gitclient.InstallProtocol("http", c)
		gitclient.InstallProtocol("https", c)
	})
}
```

- [ ] **Step 8: Run the guard tests**

Run: `go test ./internal/service -run 'TestBlockedImportIP|TestImportClient' -race -v`
Expected: PASS, 5 tests.

- [ ] **Step 9: Commit**

```bash
git add internal/config/config.go internal/config/config_test.go internal/service/import_guard.go internal/service/import_guard_test.go
git commit -m "feat(import): add import config and network guard

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 2: Source validation and the clone

**Files:**
- Create: `internal/testutil/gitserver.go`
- Create: `internal/service/import_source.go`
- Create: `internal/service/import_clone.go`
- Test: `internal/service/import_source_test.go` (package `service`)
- Test: `internal/service/import_clone_test.go` (package `service`)

**Interfaces:**
- Produces (testutil): `GitHTTPHandler(t, gitDir, user, pass string) http.Handler`, `ServeGitHTTP(t, gitDir, user, pass string) string` (returns a clone URL), `type SourceRepo struct{ Dir string; First, Second plumbing.Hash }`, `SeedSourceRepo(t) SourceRepo`.
- Produces (service): `ParseImportURL(string) (string, error)`, `ErrImportURL`, `ErrImportURLUserinfo`, `ErrImportCredentials`, `ErrImportEmptySource`, `importAuth(username, token string) transport.AuthMethod`, `type importProgress` (`io.Writer` + `String() string`), `maxProgressLine = 200`, `defaultImportBranch([]*plumbing.Reference) (string, error)`, `cloneForImport(ctx, dir, src string, auth transport.AuthMethod, progress io.Writer) (string, error)`.

- [ ] **Step 1: Add the test git server**

Create `internal/testutil/gitserver.go`:

```go
package testutil

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	gogit "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/format/pktline"
	"github.com/go-git/go-git/v5/plumbing/protocol/packp"
	"github.com/go-git/go-git/v5/plumbing/transport"
	"github.com/go-git/go-git/v5/plumbing/transport/server"
)

// GitHTTPHandler serves the bare repo at gitDir read-only over smart HTTP at
// any path. A non-empty user requires those basic-auth credentials.
func GitHTTPHandler(t *testing.T, gitDir, user, pass string) http.Handler {
	t.Helper()
	repo, err := gogit.PlainOpen(gitDir)
	if err != nil {
		t.Fatalf("open source repo: %v", err)
	}
	ep, err := transport.NewEndpoint("/")
	if err != nil {
		t.Fatalf("endpoint: %v", err)
	}
	// MapLoader is keyed on ep.String() (e.g. "file:///"), not the input to NewEndpoint.
	srv := server.NewServer(server.MapLoader{ep.String(): repo.Storer})
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if user != "" {
			u, p, ok := r.BasicAuth()
			if !ok || u != user || p != pass {
				w.Header().Set("WWW-Authenticate", `Basic realm="test"`)
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}
		}
		sess, err := srv.NewUploadPackSession(ep, nil)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		ar, err := sess.AdvertisedReferencesContext(r.Context())
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		switch {
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/info/refs"):
			w.Header().Set("Content-Type", "application/x-git-upload-pack-advertisement")
			pe := pktline.NewEncoder(w)
			_ = pe.Encodef("# service=git-upload-pack\n")
			_ = pe.Flush()
			_ = ar.Encode(w)
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/git-upload-pack"):
			req := packp.NewUploadPackRequest()
			if err := req.Decode(r.Body); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			resp, err := sess.UploadPack(r.Context(), req)
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			w.Header().Set("Content-Type", "application/x-git-upload-pack-result")
			_ = resp.Encode(w)
		default:
			http.NotFound(w, r)
		}
	})
}

// ServeGitHTTP serves GitHTTPHandler on a test server and returns a clone URL.
func ServeGitHTTP(t *testing.T, gitDir, user, pass string) string {
	t.Helper()
	srv := httptest.NewServer(GitHTTPHandler(t, gitDir, user, pass))
	t.Cleanup(srv.Close)
	return srv.URL + "/source.git"
}

// SourceRepo is a bare repo for import tests: main → First, develop → Second
// (a child of First), tag v1 → First, HEAD → develop, and refs/pull/1/head →
// Second, which an import must leave behind.
type SourceRepo struct {
	Dir           string
	First, Second plumbing.Hash
}

func SeedSourceRepo(t *testing.T) SourceRepo {
	t.Helper()
	dir := t.TempDir()
	repo, err := gogit.PlainInit(dir, true)
	if err != nil {
		t.Fatalf("init source repo: %v", err)
	}
	first := WriteCommit(t, repo.Storer, "first")
	second := WriteCommit(t, repo.Storer, "second", first)
	refs := map[plumbing.ReferenceName]plumbing.Hash{
		plumbing.NewBranchReferenceName("main"):    first,
		plumbing.NewBranchReferenceName("develop"): second,
		plumbing.NewTagReferenceName("v1"):         first,
		"refs/pull/1/head":                         second,
	}
	for name, hash := range refs {
		if err := repo.Storer.SetReference(plumbing.NewHashReference(name, hash)); err != nil {
			t.Fatalf("set %s: %v", name, err)
		}
	}
	head := plumbing.NewSymbolicReference(plumbing.HEAD, plumbing.NewBranchReferenceName("develop"))
	if err := repo.Storer.SetReference(head); err != nil {
		t.Fatalf("set HEAD: %v", err)
	}
	return SourceRepo{Dir: dir, First: first, Second: second}
}
```

Run: `go build ./internal/testutil`
Expected: no output.

- [ ] **Step 2: Write the failing source tests**

Create `internal/service/import_source_test.go`:

```go
package service

import (
	"errors"
	"strings"
	"testing"

	"github.com/go-git/go-git/v5/plumbing"
)

func TestParseImportURL(t *testing.T) {
	for _, tc := range []struct {
		in, want string
		err      error
	}{
		{"https://github.com/go-git/go-git.git", "https://github.com/go-git/go-git.git", nil},
		{"  http://git.example.com/a/b  ", "http://git.example.com/a/b", nil},
		{"https://github.com/a/b#readme", "https://github.com/a/b", nil},
		{"https://user:tok@github.com/a/b", "", ErrImportURLUserinfo},
		{"https://tok@github.com/a/b", "", ErrImportURLUserinfo},
		{"/srv/repos/alice/secret.git", "", ErrImportURL},
		{"file:///srv/repos/alice/secret.git", "", ErrImportURL},
		{"git@github.com:a/b.git", "", ErrImportURL},
		{"ssh://git@github.com/a/b.git", "", ErrImportURL},
		{"github.com/a/b", "", ErrImportURL},
		{"https:///a/b", "", ErrImportURL},
		{"", "", ErrImportURL},
	} {
		got, err := ParseImportURL(tc.in)
		if !errors.Is(err, tc.err) || got != tc.want {
			t.Errorf("ParseImportURL(%q) = %q, %v; want %q, %v", tc.in, got, err, tc.want, tc.err)
		}
	}
}

func TestImportAuth(t *testing.T) {
	if importAuth("", "") != nil {
		t.Error("importAuth without a token must be a nil interface")
	}
	if importAuth("alice", "tok") == nil {
		t.Error("importAuth with a token returned nil")
	}
}

func TestImportProgress_KeepsLastLine(t *testing.T) {
	var p importProgress
	_, _ = p.Write([]byte("Enumerating objects: 5, done.\nCounting objects:  50% (1/2)\r"))
	_, _ = p.Write([]byte("Counting objects: 100% (2/2), done.\r\nCompress"))
	if got := p.String(); got != "Counting objects: 100% (2/2), done." {
		t.Errorf("String() = %q", got)
	}
	_, _ = p.Write([]byte(strings.Repeat("x", 500) + "\n"))
	if got := p.String(); len(got) != maxProgressLine {
		t.Errorf("len(String()) = %d, want %d", len(got), maxProgressLine)
	}
}

func TestDefaultImportBranch(t *testing.T) {
	h := plumbing.NewHash("1111111111111111111111111111111111111111")
	branch := func(n string) *plumbing.Reference {
		return plumbing.NewHashReference(plumbing.NewBranchReferenceName(n), h)
	}
	head := func(n string) *plumbing.Reference {
		return plumbing.NewSymbolicReference(plumbing.HEAD, plumbing.NewBranchReferenceName(n))
	}
	tag := plumbing.NewHashReference(plumbing.NewTagReferenceName("v1"), h)
	for _, tc := range []struct {
		name string
		refs []*plumbing.Reference
		want string
		err  error
	}{
		{"HEAD target", []*plumbing.Reference{head("develop"), branch("main"), branch("develop")}, "develop", nil},
		{"HEAD on a missing branch falls back to main", []*plumbing.Reference{head("gone"), branch("zeta"), branch("main")}, "main", nil},
		{"no HEAD and no main: first by name", []*plumbing.Reference{branch("zeta"), branch("alpha")}, "alpha", nil},
		{"nested branch name", []*plumbing.Reference{head("release/1.0"), branch("release/1.0")}, "release/1.0", nil},
		{"tags only", []*plumbing.Reference{tag}, "", ErrImportEmptySource},
	} {
		got, err := defaultImportBranch(tc.refs)
		if got != tc.want || !errors.Is(err, tc.err) {
			t.Errorf("%s: got %q, %v; want %q, %v", tc.name, got, err, tc.want, tc.err)
		}
	}
}
```

- [ ] **Step 3: Write the failing clone tests**

Create `internal/service/import_clone_test.go`:

```go
package service

import (
	"context"
	"errors"
	"io"
	"path/filepath"
	"testing"

	gogit "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/transport"
	"github.com/mkappworks-dev/cloudzilla-app/internal/testutil"
)

func TestCloneForImport_CopiesBranchesTagsAndHead(t *testing.T) {
	src := testutil.SeedSourceRepo(t)
	url := testutil.ServeGitHTTP(t, src.Dir, "", "")
	dir := filepath.Join(t.TempDir(), "clone")

	branch, err := cloneForImport(context.Background(), dir, url, nil, io.Discard)
	if err != nil {
		t.Fatalf("cloneForImport: %v", err)
	}
	if branch != "develop" {
		t.Errorf("default branch = %q, want develop", branch)
	}

	repo, err := gogit.PlainOpen(dir)
	if err != nil {
		t.Fatalf("open clone: %v", err)
	}
	want := map[plumbing.ReferenceName]plumbing.Hash{
		plumbing.NewBranchReferenceName("main"):    src.First,
		plumbing.NewBranchReferenceName("develop"): src.Second,
		plumbing.NewTagReferenceName("v1"):         src.First,
	}
	for name, hash := range want {
		ref, err := repo.Reference(name, false)
		if err != nil || ref.Hash() != hash {
			t.Errorf("%s = %v, %v; want %s", name, ref, err, hash)
		}
	}
	if _, err := repo.Reference("refs/pull/1/head", false); err == nil {
		t.Error("refs/pull/1/head was imported")
	}
	head, err := repo.Reference(plumbing.HEAD, false)
	if err != nil || head.Target() != plumbing.NewBranchReferenceName("develop") {
		t.Errorf("HEAD = %v, %v; want -> refs/heads/develop", head, err)
	}
	cfg, err := repo.Config()
	if err != nil || len(cfg.Remotes) != 0 {
		t.Errorf("remotes = %v, %v; want none", cfg.Remotes, err)
	}
	if _, err := repo.CommitObject(src.Second); err != nil {
		t.Errorf("develop's commit missing: %v", err)
	}
}

func TestCloneForImport_PrivateSourceNeedsCredentials(t *testing.T) {
	src := testutil.SeedSourceRepo(t)
	url := testutil.ServeGitHTTP(t, src.Dir, "alice", "s3cret")

	_, err := cloneForImport(context.Background(), filepath.Join(t.TempDir(), "anon"), url, nil, io.Discard)
	if !errors.Is(err, transport.ErrAuthenticationRequired) {
		t.Fatalf("without credentials: err = %v, want ErrAuthenticationRequired", err)
	}
	if _, err := cloneForImport(context.Background(), filepath.Join(t.TempDir(), "authed"), url, importAuth("alice", "s3cret"), io.Discard); err != nil {
		t.Fatalf("with credentials: %v", err)
	}
}

func TestCloneForImport_EmptySource(t *testing.T) {
	dir := t.TempDir()
	if _, err := gogit.PlainInit(dir, true); err != nil {
		t.Fatalf("init: %v", err)
	}
	url := testutil.ServeGitHTTP(t, dir, "", "")
	_, err := cloneForImport(context.Background(), filepath.Join(t.TempDir(), "clone"), url, nil, io.Discard)
	if !errors.Is(err, ErrImportEmptySource) {
		t.Errorf("err = %v, want ErrImportEmptySource", err)
	}
}
```

- [ ] **Step 4: Run them to see them fail**

Run: `go test ./internal/service -run 'TestParseImportURL|TestImportAuth|TestImportProgress|TestDefaultImportBranch|TestCloneForImport'`
Expected: FAIL to compile, `undefined: ParseImportURL`.

- [ ] **Step 5: Implement source handling**

Create `internal/service/import_source.go`:

```go
package service

import (
	"bytes"
	"errors"
	"net/url"
	"strings"
	"sync"

	"github.com/go-git/go-git/v5/plumbing/transport"
	githttp "github.com/go-git/go-git/v5/plumbing/transport/http"
)

var (
	ErrImportURL         = errors.New("enter the http:// or https:// URL of a Git repository")
	ErrImportURLUserinfo = errors.New("put credentials in the username and token fields, not in the URL")
	ErrImportCredentials = errors.New("enter both a username and a token, or neither")
	ErrImportEmptySource = errors.New("the source repository is empty")
)

// ParseImportURL accepts http(s) only: go-git reads a bare path or file://
// URL as a repository on this server's disk.
func ParseImportURL(raw string) (string, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" {
		return "", ErrImportURL
	}
	if u.User != nil {
		return "", ErrImportURLUserinfo
	}
	u.Fragment, u.RawFragment = "", ""
	return u.String(), nil
}

// importAuth returns a nil interface, not a nil *BasicAuth, which go-git
// would call methods on.
func importAuth(username, token string) transport.AuthMethod {
	if token == "" {
		return nil
	}
	return &githttp.BasicAuth{Username: username, Password: token}
}

const maxProgressLine = 200

// importProgress keeps the last line of the source's progress output, which
// git redraws in place with \r.
type importProgress struct {
	mu      sync.Mutex
	pending []byte
	last    string
}

func (p *importProgress) Write(b []byte) (int, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.pending = append(p.pending, b...)
	for {
		i := bytes.IndexAny(p.pending, "\r\n")
		if i < 0 {
			break
		}
		if line := strings.TrimSpace(string(p.pending[:i])); line != "" {
			p.last = truncateRunes(line, maxProgressLine)
		}
		p.pending = p.pending[i+1:]
	}
	if len(p.pending) > 4*maxProgressLine {
		p.pending = p.pending[len(p.pending)-4*maxProgressLine:]
	}
	return len(b), nil
}

func (p *importProgress) String() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.last
}

func truncateRunes(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n])
}
```

- [ ] **Step 6: Implement the clone**

Create `internal/service/import_clone.go`:

```go
package service

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sort"

	gogit "github.com/go-git/go-git/v5"
	gitconfig "github.com/go-git/go-git/v5/config"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/transport"
)

// defaultImportBranch prefers the source's HEAD target, then main, then the
// first branch by name.
func defaultImportBranch(refs []*plumbing.Reference) (string, error) {
	var head plumbing.ReferenceName
	branches := map[plumbing.ReferenceName]bool{}
	for _, ref := range refs {
		switch {
		case ref.Name() == plumbing.HEAD && ref.Type() == plumbing.SymbolicReference:
			head = ref.Target()
		case ref.Name().IsBranch():
			branches[ref.Name()] = true
		}
	}
	if len(branches) == 0 {
		return "", ErrImportEmptySource
	}
	if branches[head] {
		return head.Short(), nil
	}
	if main := plumbing.NewBranchReferenceName("main"); branches[main] {
		return main.Short(), nil
	}
	names := make([]string, 0, len(branches))
	for name := range branches {
		names = append(names, name.Short())
	}
	sort.Strings(names)
	return names[0], nil
}

// cloneForImport fetches src's branches and tags into a new bare repo at dir,
// points HEAD at the source's default branch and returns that branch.
func cloneForImport(ctx context.Context, dir, src string, auth transport.AuthMethod, progress io.Writer) (string, error) {
	repo, err := gogit.PlainInit(dir, true)
	if err != nil {
		return "", fmt.Errorf("init bare repo: %w", err)
	}
	remote, err := repo.CreateRemote(&gitconfig.RemoteConfig{
		Name: "origin",
		URLs: []string{src},
		// Not a mirror's +refs/*:refs/*, which would also copy GitHub's refs/pull/*.
		Fetch: []gitconfig.RefSpec{"+refs/heads/*:refs/heads/*", "+refs/tags/*:refs/tags/*"},
	})
	if err != nil {
		return "", fmt.Errorf("add remote: %w", err)
	}
	refs, err := remote.ListContext(ctx, &gogit.ListOptions{Auth: auth})
	if errors.Is(err, transport.ErrEmptyRemoteRepository) {
		return "", ErrImportEmptySource
	}
	if err != nil {
		return "", err
	}
	branch, err := defaultImportBranch(refs)
	if err != nil {
		return "", err
	}
	err = remote.FetchContext(ctx, &gogit.FetchOptions{Auth: auth, Tags: gogit.NoTags, Progress: progress})
	if err != nil && !errors.Is(err, gogit.NoErrAlreadyUpToDate) {
		return "", err
	}
	head := plumbing.NewSymbolicReference(plumbing.HEAD, plumbing.NewBranchReferenceName(branch))
	if err := repo.Storer.SetReference(head); err != nil {
		return "", fmt.Errorf("set HEAD: %w", err)
	}
	if err := repo.DeleteRemote("origin"); err != nil {
		return "", fmt.Errorf("remove remote: %w", err)
	}
	return branch, nil
}
```

- [ ] **Step 7: Run the tests**

Run: `go test ./internal/service -run 'TestParseImportURL|TestImportAuth|TestImportProgress|TestDefaultImportBranch|TestCloneForImport' -race -v`
Expected: PASS, 7 tests. If `TestCloneForImport_EmptySource` gets a different error, print it and map that error to `ErrImportEmptySource` in `cloneForImport` only if it means "no refs advertised".

- [ ] **Step 8: Commit**

```bash
git add internal/testutil/gitserver.go internal/service/import_source.go internal/service/import_clone.go internal/service/import_source_test.go internal/service/import_clone_test.go
git commit -m "feat(import): validate import sources and clone branches and tags

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 3: Resolve the owner and publish the clone

**Files:**
- Create: `internal/service/repo_import.go`
- Test: `internal/service/repo_import_test.go` (package `service_test`, needs `TEST_DATABASE_DSN`)

**Interfaces:**
- Consumes: `claimRepo`, `abandonNewRepo`, `repoNameErr`, `personalOwner`, `isOrgOwner`, `ValidateRepoName`, `ErrForbidden`, `ErrRepoNameTaken` (all existing in package `service`).
- Produces: `type ImportTarget struct{ ActorID int64; OwnerName string; OwnerID int64; OrgID int64 }`, `(*RepoService).ResolveImportTarget(ctx, actorID int64, actorUsername, ownerName string) (ImportTarget, error)`, `(*RepoService).CheckImportName(ctx, ownerName, name string) error`, `(*RepoService).CreateFromImport(ctx, t ImportTarget, name, description string, private bool, defaultBranch, srcDir string) (*model.Repository, error)`.
- Produces (test package `service_test`): `seedImportUser(t, db) (int64, string)`, `seedImportedClone(t) string`. Task 4 reuses `seedImportUser`.

- [ ] **Step 1: Write the failing tests**

Create `internal/service/repo_import_test.go`:

```go
package service_test

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"testing"

	gogit "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/mkappworks-dev/cloudzilla-app/internal/config"
	"github.com/mkappworks-dev/cloudzilla-app/internal/service"
	"github.com/mkappworks-dev/cloudzilla-app/internal/store"
	"github.com/mkappworks-dev/cloudzilla-app/internal/testutil"
)

func newImportRepoSvc(t *testing.T) (*service.RepoService, *service.OrgService, *sql.DB, string) {
	t.Helper()
	db := testutil.OpenTestDB(t)
	cfg := config.GitConfig{ReposRoot: t.TempDir()}
	repos, users, orgs := store.NewRepoStore(db), store.NewUserStore(db), store.NewOrgStore(db)
	return service.NewRepoService(repos, users, orgs, nil, nil, cfg), service.NewOrgService(orgs, repos, users, cfg), db, cfg.ReposRoot
}

// seedImportUser's repos are deleted before the user: cleanups run last-in, first-out.
func seedImportUser(t *testing.T, db *sql.DB) (int64, string) {
	t.Helper()
	suffix := testutil.UniqueSuffix(t)
	id := testutil.SeedUser(t, db, suffix)
	name := "testuser_" + suffix
	t.Cleanup(func() { testutil.Exec(t, db, `DELETE FROM repositories WHERE owner_name = $1`, name) })
	return id, name
}

func seedImportedClone(t *testing.T) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "clone")
	repo, err := gogit.PlainInit(dir, true)
	if err != nil {
		t.Fatalf("init clone: %v", err)
	}
	c := testutil.WriteCommit(t, repo.Storer, "first")
	if err := repo.Storer.SetReference(plumbing.NewHashReference(plumbing.NewBranchReferenceName("trunk"), c)); err != nil {
		t.Fatalf("set trunk: %v", err)
	}
	return dir
}

func seedImportOrg(t *testing.T, db *sql.DB, orgs *service.OrgService, ownerID int64) string {
	t.Helper()
	org, err := orgs.Create(context.Background(), ownerID, "imporg_"+testutil.UniqueSuffix(t), "", "")
	if err != nil {
		t.Fatalf("create org: %v", err)
	}
	testutil.DeleteOrgOnCleanup(t, db, org.ID)
	t.Cleanup(func() { testutil.Exec(t, db, `DELETE FROM repositories WHERE owner_name = $1`, org.Name) })
	return org.Name
}

func TestResolveImportTarget_OwnAccountAndOwnedOrg(t *testing.T) {
	svc, orgs, db, _ := newImportRepoSvc(t)
	ctx := context.Background()
	uid, uname := seedImportUser(t, db)

	got, err := svc.ResolveImportTarget(ctx, uid, uname, "")
	if want := (service.ImportTarget{ActorID: uid, OwnerName: uname, OwnerID: uid}); err != nil || got != want {
		t.Errorf("own account: got %+v, %v; want %+v", got, err, want)
	}

	orgName := seedImportOrg(t, db, orgs, uid)
	got, err = svc.ResolveImportTarget(ctx, uid, uname, orgName)
	if err != nil || got.OwnerName != orgName || got.OrgID == 0 || got.OwnerID != 0 || got.ActorID != uid {
		t.Errorf("owned org: got %+v, %v", got, err)
	}
}

func TestResolveImportTarget_RefusesOrgsTheActorDoesNotOwn(t *testing.T) {
	svc, orgs, db, _ := newImportRepoSvc(t)
	ctx := context.Background()
	ownerID, _ := seedImportUser(t, db)
	otherID, otherName := seedImportUser(t, db)
	orgName := seedImportOrg(t, db, orgs, ownerID)

	for _, owner := range []string{orgName, "no-such-org-" + testutil.UniqueSuffix(t)} {
		if _, err := svc.ResolveImportTarget(ctx, otherID, otherName, owner); !errors.Is(err, service.ErrForbidden) {
			t.Errorf("owner %q: err = %v, want ErrForbidden", owner, err)
		}
	}
}

func TestCheckImportName(t *testing.T) {
	svc, _, db, _ := newImportRepoSvc(t)
	ctx := context.Background()
	uid, uname := seedImportUser(t, db)
	if _, err := svc.Create(ctx, uid, uname, "taken", "", false, service.RepoInitOptions{}); err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := svc.CheckImportName(ctx, uname, "free"); err != nil {
		t.Errorf("free name: %v", err)
	}
	if err := svc.CheckImportName(ctx, uname, "taken"); !errors.Is(err, service.ErrRepoNameTaken) {
		t.Errorf("taken name: err = %v, want ErrRepoNameTaken", err)
	}
	if err := svc.CheckImportName(ctx, uname, "bad name"); !errors.Is(err, service.ErrInvalidRepoName) {
		t.Errorf("invalid name: err = %v, want ErrInvalidRepoName", err)
	}
}

func TestCreateFromImport_AdoptsTheClone(t *testing.T) {
	svc, _, db, root := newImportRepoSvc(t)
	ctx := context.Background()
	uid, uname := seedImportUser(t, db)
	src := seedImportedClone(t)
	target := service.ImportTarget{ActorID: uid, OwnerName: uname, OwnerID: uid}

	repo, err := svc.CreateFromImport(ctx, target, "imported", "from elsewhere", true, "trunk", src)
	if err != nil {
		t.Fatalf("CreateFromImport: %v", err)
	}
	got, err := svc.Get(ctx, uname, "imported")
	if err != nil || got.ID != repo.ID || got.DefaultBranch != "trunk" || !got.Private || got.Description != "from elsewhere" {
		t.Errorf("row = %+v, %v", got, err)
	}
	if _, err := os.Stat(src); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("clone dir still exists: %v", err)
	}
	moved, err := gogit.PlainOpen(filepath.Join(root, uname, "imported.git"))
	if err != nil {
		t.Fatalf("open published repo: %v", err)
	}
	if _, err := moved.Reference(plumbing.NewBranchReferenceName("trunk"), false); err != nil {
		t.Errorf("trunk missing from published repo: %v", err)
	}
}

func TestCreateFromImport_NameTakenLeavesCloneForTheCaller(t *testing.T) {
	svc, _, db, _ := newImportRepoSvc(t)
	ctx := context.Background()
	uid, uname := seedImportUser(t, db)
	if _, err := svc.Create(ctx, uid, uname, "taken", "", false, service.RepoInitOptions{}); err != nil {
		t.Fatalf("create: %v", err)
	}
	src := seedImportedClone(t)
	target := service.ImportTarget{ActorID: uid, OwnerName: uname, OwnerID: uid}

	if _, err := svc.CreateFromImport(ctx, target, "taken", "", false, "trunk", src); !errors.Is(err, service.ErrRepoNameTaken) {
		t.Errorf("err = %v, want ErrRepoNameTaken", err)
	}
	if _, err := os.Stat(src); err != nil {
		t.Errorf("clone dir removed: %v", err)
	}
}

func TestCreateFromImport_RechecksOrgOwnership(t *testing.T) {
	svc, orgs, db, _ := newImportRepoSvc(t)
	ctx := context.Background()
	ownerID, ownerName := seedImportUser(t, db)
	otherID, _ := seedImportUser(t, db)
	orgName := seedImportOrg(t, db, orgs, ownerID)
	target, err := svc.ResolveImportTarget(ctx, ownerID, ownerName, orgName)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	target.ActorID = otherID

	if _, err := svc.CreateFromImport(ctx, target, "imported", "", false, "trunk", seedImportedClone(t)); !errors.Is(err, service.ErrForbidden) {
		t.Errorf("err = %v, want ErrForbidden", err)
	}
}
```

- [ ] **Step 2: Run them to see them fail**

Run: `TEST_DATABASE_DSN='postgres://cloudzilla:cloudzilla@localhost:5432/cloudzilla_test?sslmode=disable' go test ./internal/service -run 'TestResolveImportTarget|TestCheckImportName|TestCreateFromImport'`
Expected: FAIL to compile, `svc.ResolveImportTarget undefined`.

- [ ] **Step 3: Implement**

Create `internal/service/repo_import.go`:

```go
package service

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"

	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
)

// ImportTarget is the namespace an import lands in: OwnerID for a personal
// repo, OrgID for an org repo.
type ImportTarget struct {
	ActorID   int64
	OwnerName string
	OwnerID   int64
	OrgID     int64
}

var errImportOwner = fmt.Errorf("you can import only into your account or an organization you own: %w", ErrForbidden)

// ResolveImportTarget maps ownerName to the actor's own account (also for "")
// or to an org the actor owns.
func (s *RepoService) ResolveImportTarget(ctx context.Context, actorID int64, actorUsername, ownerName string) (ImportTarget, error) {
	if ownerName == "" || ownerName == actorUsername {
		if _, err := s.personalOwner(ctx, actorID, actorUsername); err != nil {
			return ImportTarget{}, err
		}
		return ImportTarget{ActorID: actorID, OwnerName: actorUsername, OwnerID: actorID}, nil
	}
	org, err := s.orgs.GetByName(ctx, ownerName)
	if errors.Is(err, sql.ErrNoRows) {
		return ImportTarget{}, errImportOwner
	}
	if err != nil {
		return ImportTarget{}, err
	}
	if !s.isOrgOwner(ctx, org.ID, actorID) {
		return ImportTarget{}, errImportOwner
	}
	return ImportTarget{ActorID: actorID, OwnerName: org.Name, OrgID: org.ID}, nil
}

// CheckImportName fails fast before a long clone; claimRepo checks again at publish.
func (s *RepoService) CheckImportName(ctx context.Context, ownerName, name string) error {
	if err := ValidateRepoName(name); err != nil {
		return fmt.Errorf("invalid repository name: %w", err)
	}
	held, err := s.repos.NameHeld(ctx, ownerName, name)
	if err != nil {
		return err
	}
	if held {
		return ErrRepoNameTaken
	}
	return nil
}

// CreateFromImport adopts srcDir, a bare repo an import cloned, as t's repo
// name. srcDir is left in place on failure; the caller removes it.
func (s *RepoService) CreateFromImport(ctx context.Context, t ImportTarget, name, description string, private bool, defaultBranch, srcDir string) (*model.Repository, error) {
	if err := ValidateRepoName(name); err != nil {
		return nil, fmt.Errorf("invalid repository name: %w", err)
	}
	if t.OrgID != 0 {
		if !s.isOrgOwner(ctx, t.OrgID, t.ActorID) {
			return nil, fmt.Errorf("no longer an owner of %s: %w", t.OwnerName, ErrForbidden)
		}
	} else if _, err := s.personalOwner(ctx, t.OwnerID, t.OwnerName); err != nil {
		return nil, err
	}

	gitDir, err := claimRepo(ctx, s.repos, s.cfg.ReposRoot, t.OwnerName, name)
	if err != nil {
		return nil, err
	}
	r := &model.Repository{
		OwnerID:       t.OwnerID,
		OrgID:         t.OrgID,
		CreatedBy:     t.ActorID,
		OwnerName:     t.OwnerName,
		Name:          name,
		Description:   description,
		Private:       private,
		DefaultBranch: defaultBranch,
	}
	if err := s.repos.CreateWithOwnerName(ctx, r); err != nil {
		abandonNewRepo(ctx, s.repos, 0, gitDir)
		return nil, repoNameErr("create imported repo", err)
	}
	// os.Rename won't replace a directory, so the empty claim goes first. The
	// row already holds the name, so a concurrent create still sees it taken.
	if err := os.Remove(gitDir); err != nil {
		abandonNewRepo(ctx, s.repos, r.ID, gitDir)
		return nil, fmt.Errorf("remove claimed dir: %w", err)
	}
	if err := os.Rename(srcDir, gitDir); err != nil {
		abandonNewRepo(ctx, s.repos, r.ID, gitDir)
		return nil, fmt.Errorf("move imported repo into place: %w", err)
	}
	return r, nil
}
```

- [ ] **Step 4: Run the tests**

Run: `TEST_DATABASE_DSN='postgres://cloudzilla:cloudzilla@localhost:5432/cloudzilla_test?sslmode=disable' go test ./internal/service -run 'TestResolveImportTarget|TestCheckImportName|TestCreateFromImport' -race -v`
Expected: PASS, 6 tests (none skipped).

- [ ] **Step 5: Commit**

```bash
git add internal/service/repo_import.go internal/service/repo_import_test.go
git commit -m "feat(import): resolve import owners and publish clones

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 4: Import jobs

**Files:**
- Create: `internal/service/import_service.go`
- Modify: `internal/service/services.go`
- Modify: `cmd/server/main.go`
- Test: `internal/service/import_service_internal_test.go` (package `service`)
- Test: `internal/service/import_service_test.go` (package `service_test`, needs `TEST_DATABASE_DSN`)

**Interfaces:**
- Consumes: Tasks 1–3 (`installImportTransport`, `importGuard`, `withImportGuard`, `ImportBlockedError`, `ErrImportTooLarge`, `ParseImportURL`, `importAuth`, `importProgress`, `cloneForImport`, `ErrImportEmptySource`, `ErrImportCredentials`, `ImportTarget`, `ResolveImportTarget`, `CheckImportName`, `CreateFromImport`), `concurrency.Go`, and `seedImportUser` from `repo_import_test.go`.
- Produces: `type ImportStatus string` with `ImportQueued`, `ImportRunning`, `ImportDone`, `ImportFailed`; `type ImportRequest struct{ CloneURL, AuthUsername, AuthToken, Owner, Name, Description string; Private bool }`; `type ImportJob struct{ ID string; UserID int64; SourceURL, Owner, Name string; Status ImportStatus; Progress, Error string; FinishedAt time.Time }` with `Finished() bool`; `NewImportService(repo *RepoService, git config.GitConfig, cfg config.ImportConfig) *ImportService`; `(*ImportService).Start(ctx, actorID int64, actorUsername string, req ImportRequest) (ImportJob, error)`; `(*ImportService).Get(userID int64, id string) (ImportJob, error)`; `(*ImportService).RemoveStaleTemp() error`; `ErrTooManyImports`, `ErrImportNotFound`; `Services.Import`.

- [ ] **Step 1: Write the failing unit tests**

Create `internal/service/import_service_internal_test.go`:

```go
package service

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestImportService_StartRejectsBadInputBeforeAnyLookup(t *testing.T) {
	s := &ImportService{jobs: map[string]*importJob{}} // nil repo: a lookup would panic
	for _, tc := range []struct {
		req ImportRequest
		err error
	}{
		{ImportRequest{CloneURL: "/srv/repos/alice/secret.git", Name: "x"}, ErrImportURL},
		{ImportRequest{CloneURL: "https://user:tok@example.com/a.git", Name: "x"}, ErrImportURLUserinfo},
		{ImportRequest{CloneURL: "https://example.com/a.git", AuthToken: "tok", Name: "x"}, ErrImportCredentials},
		{ImportRequest{CloneURL: "https://example.com/a.git", AuthUsername: "me", Name: "x"}, ErrImportCredentials},
	} {
		if _, err := s.Start(context.Background(), 1, "me", tc.req); !errors.Is(err, tc.err) {
			t.Errorf("Start(%+v) = %v, want %v", tc.req, err, tc.err)
		}
	}
}

func TestImportService_SweepDropsOldFinishedJobs(t *testing.T) {
	now := time.Now()
	s := &ImportService{jobs: map[string]*importJob{
		"old":     {ImportJob: ImportJob{ID: "old", Status: ImportDone, FinishedAt: now.Add(-2 * time.Hour)}},
		"recent":  {ImportJob: ImportJob{ID: "recent", Status: ImportFailed, FinishedAt: now.Add(-time.Minute)}},
		"running": {ImportJob: ImportJob{ID: "running", Status: ImportRunning}},
	}}
	s.sweepLocked(now)
	if _, ok := s.jobs["old"]; ok {
		t.Error("old finished job kept")
	}
	for _, id := range []string{"recent", "running"} {
		if _, ok := s.jobs[id]; !ok {
			t.Errorf("%s job dropped", id)
		}
	}
}

func TestFormatImportLimits(t *testing.T) {
	for d, want := range map[time.Duration]string{
		30 * time.Minute: "30m", time.Hour: "1h", 90 * time.Minute: "1h30m", 45 * time.Second: "45s",
	} {
		if got := formatImportTimeout(d); got != want {
			t.Errorf("formatImportTimeout(%v) = %q, want %q", d, got, want)
		}
	}
	for n, want := range map[int64]string{2 << 30: "2 GiB", 100 << 20: "100 MiB", 1: "1 MiB"} {
		if got := formatImportBytes(n); got != want {
			t.Errorf("formatImportBytes(%d) = %q, want %q", n, got, want)
		}
	}
}
```

- [ ] **Step 2: Write the failing integration tests**

Create `internal/service/import_service_test.go`:

```go
package service_test

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/mkappworks-dev/cloudzilla-app/internal/config"
	"github.com/mkappworks-dev/cloudzilla-app/internal/service"
	"github.com/mkappworks-dev/cloudzilla-app/internal/store"
	"github.com/mkappworks-dev/cloudzilla-app/internal/testutil"
)

func newImportEnv(t *testing.T, allowLocal bool) (*service.ImportService, *service.RepoService, *sql.DB, string) {
	t.Helper()
	db := testutil.OpenTestDB(t)
	git := config.GitConfig{ReposRoot: t.TempDir()}
	repoSvc := service.NewRepoService(store.NewRepoStore(db), store.NewUserStore(db), store.NewOrgStore(db), nil, nil, git)
	imports := service.NewImportService(repoSvc, git, config.ImportConfig{AllowLocalNetworks: allowLocal, Timeout: time.Minute})
	return imports, repoSvc, db, git.ReposRoot
}

func waitImport(t *testing.T, imports *service.ImportService, userID int64, id string) service.ImportJob {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for {
		job, err := imports.Get(userID, id)
		if err != nil {
			t.Fatalf("Get(%s): %v", id, err)
		}
		if job.Finished() {
			return job
		}
		if time.Now().After(deadline) {
			t.Fatalf("import %s still %s", id, job.Status)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func TestImport_PublishesTheSource(t *testing.T) {
	imports, repoSvc, db, root := newImportEnv(t, true)
	ctx := context.Background()
	uid, uname := seedImportUser(t, db)
	src := testutil.SeedSourceRepo(t)

	job, err := imports.Start(ctx, uid, uname, service.ImportRequest{
		CloneURL: testutil.ServeGitHTTP(t, src.Dir, "", ""), Name: "imported", Description: "copied", Private: true,
	})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	if job.Status != service.ImportQueued || job.Owner != uname || job.Name != "imported" {
		t.Errorf("started job = %+v", job)
	}
	if done := waitImport(t, imports, uid, job.ID); done.Status != service.ImportDone {
		t.Fatalf("status %s: %s", done.Status, done.Error)
	}
	repo, err := repoSvc.Get(ctx, uname, "imported")
	if err != nil || repo.DefaultBranch != "develop" || !repo.Private || repo.Description != "copied" {
		t.Errorf("repo = %+v, %v", repo, err)
	}
	if _, err := os.Stat(filepath.Join(root, ".import-tmp", job.ID)); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("temp clone left behind: %v", err)
	}
}

func TestImport_PrivateSource(t *testing.T) {
	imports, _, db, _ := newImportEnv(t, true)
	ctx := context.Background()
	uid, uname := seedImportUser(t, db)
	url := testutil.ServeGitHTTP(t, testutil.SeedSourceRepo(t).Dir, "alice", "s3cret")

	anon, err := imports.Start(ctx, uid, uname, service.ImportRequest{CloneURL: url, Name: "anon"})
	if err != nil {
		t.Fatalf("Start anon: %v", err)
	}
	got := waitImport(t, imports, uid, anon.ID)
	if got.Status != service.ImportFailed || got.Error != "Repository not found, or it needs a username and token." {
		t.Errorf("without credentials: %s %q", got.Status, got.Error)
	}

	authed, err := imports.Start(ctx, uid, uname, service.ImportRequest{CloneURL: url, AuthUsername: "alice", AuthToken: "s3cret", Name: "authed"})
	if err != nil {
		t.Fatalf("Start authed: %v", err)
	}
	if got := waitImport(t, imports, uid, authed.ID); got.Status != service.ImportDone {
		t.Errorf("with credentials: %s %q", got.Status, got.Error)
	}
}

func TestImport_BlocksPrivateNetworksByDefault(t *testing.T) {
	imports, repoSvc, db, _ := newImportEnv(t, false)
	ctx := context.Background()
	uid, uname := seedImportUser(t, db)
	url := testutil.ServeGitHTTP(t, testutil.SeedSourceRepo(t).Dir, "", "")

	job, err := imports.Start(ctx, uid, uname, service.ImportRequest{CloneURL: url, Name: "blocked"})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	got := waitImport(t, imports, uid, job.ID)
	want := "127.0.0.1 resolves to a private network address. An administrator can allow this with import.allow_local_networks."
	if got.Status != service.ImportFailed || got.Error != want {
		t.Errorf("got %s %q, want failed %q", got.Status, got.Error, want)
	}
	if _, err := repoSvc.Get(ctx, uname, "blocked"); err == nil {
		t.Error("a refused import created a repository")
	}
}

func TestImport_RefusesATakenName(t *testing.T) {
	imports, repoSvc, db, _ := newImportEnv(t, true)
	ctx := context.Background()
	uid, uname := seedImportUser(t, db)
	if _, err := repoSvc.Create(ctx, uid, uname, "taken", "", false, service.RepoInitOptions{}); err != nil {
		t.Fatalf("create: %v", err)
	}
	_, err := imports.Start(ctx, uid, uname, service.ImportRequest{CloneURL: "https://example.com/a.git", Name: "taken"})
	if !errors.Is(err, service.ErrRepoNameTaken) {
		t.Errorf("err = %v, want ErrRepoNameTaken", err)
	}
}

func TestImport_JobsArePrivateToTheirUser(t *testing.T) {
	imports, _, db, _ := newImportEnv(t, false)
	ctx := context.Background()
	uid, uname := seedImportUser(t, db)
	otherID, _ := seedImportUser(t, db)

	job, err := imports.Start(ctx, uid, uname, service.ImportRequest{CloneURL: "http://127.0.0.1:1/x.git", Name: "mine"})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	waitImport(t, imports, uid, job.ID)
	if _, err := imports.Get(otherID, job.ID); !errors.Is(err, service.ErrImportNotFound) {
		t.Errorf("other user's Get: err = %v, want ErrImportNotFound", err)
	}
}

func TestImport_LimitsActiveImportsPerUser(t *testing.T) {
	imports, _, db, _ := newImportEnv(t, true)
	ctx := context.Background()
	uid, uname := seedImportUser(t, db)
	handler := testutil.GitHTTPHandler(t, testutil.SeedSourceRepo(t).Dir, "", "")
	gate := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-gate
		handler.ServeHTTP(w, r)
	}))
	t.Cleanup(srv.Close)
	release := sync.OnceFunc(func() { close(gate) })
	t.Cleanup(release) // before srv.Close, which waits for the held requests

	var ids []string
	for i := range 5 {
		job, err := imports.Start(ctx, uid, uname, service.ImportRequest{CloneURL: srv.URL + "/source.git", Name: fmt.Sprintf("held%d", i)})
		if err != nil {
			t.Fatalf("import %d: %v", i, err)
		}
		ids = append(ids, job.ID)
	}
	if _, err := imports.Start(ctx, uid, uname, service.ImportRequest{CloneURL: srv.URL + "/source.git", Name: "sixth"}); !errors.Is(err, service.ErrTooManyImports) {
		t.Errorf("sixth import: err = %v, want ErrTooManyImports", err)
	}
	release()
	for _, id := range ids {
		waitImport(t, imports, uid, id)
	}
}
```

- [ ] **Step 3: Run them to see them fail**

Run: `TEST_DATABASE_DSN='postgres://cloudzilla:cloudzilla@localhost:5432/cloudzilla_test?sslmode=disable' go test ./internal/service -run 'TestImportService_|TestFormatImportLimits|TestImport_'`
Expected: FAIL to compile, `undefined: ImportService`.

- [ ] **Step 4: Implement the service**

Create `internal/service/import_service.go`:

```go
package service

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/go-git/go-git/v5/plumbing/transport"
	"github.com/mkappworks-dev/cloudzilla-app/internal/concurrency"
	"github.com/mkappworks-dev/cloudzilla-app/internal/config"
)

const (
	importConcurrency  = 3
	importPerUserLimit = 5
	importJobRetention = time.Hour
	// Owner names start with a letter or digit, so this can't collide with an owner dir.
	importTmpDirName = ".import-tmp"
)

var (
	ErrTooManyImports = fmt.Errorf("you already have %d imports in progress; wait for one to finish", importPerUserLimit)
	ErrImportNotFound = errors.New("import not found")
)

type ImportStatus string

const (
	ImportQueued  ImportStatus = "queued"
	ImportRunning ImportStatus = "running"
	ImportDone    ImportStatus = "done"
	ImportFailed  ImportStatus = "failed"
)

type ImportRequest struct {
	CloneURL     string
	AuthUsername string
	AuthToken    string
	Owner        string
	Name         string
	Description  string
	Private      bool
}

// ImportJob is a snapshot of one import. It never holds credentials.
type ImportJob struct {
	ID         string
	UserID     int64
	SourceURL  string
	Owner      string
	Name       string
	Status     ImportStatus
	Progress   string
	Error      string
	FinishedAt time.Time
}

func (j ImportJob) Finished() bool {
	return j.Status == ImportDone || j.Status == ImportFailed
}

// Status, Error and FinishedAt change under ImportService.mu; the other
// fields are fixed when Start creates the job.
type importJob struct {
	ImportJob
	progress *importProgress
}

// ImportService runs imports in the background. Jobs live in memory: a
// restart drops imports in flight, and RemoveStaleTemp clears their clones.
type ImportService struct {
	repo     *RepoService
	root     string
	maxBytes int64
	cfg      config.ImportConfig
	slots    chan struct{}

	mu   sync.Mutex
	jobs map[string]*importJob
}

func NewImportService(repo *RepoService, git config.GitConfig, cfg config.ImportConfig) *ImportService {
	installImportTransport()
	return &ImportService{
		repo:     repo,
		root:     git.ReposRoot,
		maxBytes: git.MaxPackBytes,
		cfg:      cfg,
		slots:    make(chan struct{}, importConcurrency),
		jobs:     map[string]*importJob{},
	}
}

func (s *ImportService) Start(ctx context.Context, actorID int64, actorUsername string, req ImportRequest) (ImportJob, error) {
	src, err := ParseImportURL(req.CloneURL)
	if err != nil {
		return ImportJob{}, err
	}
	if (req.AuthUsername == "") != (req.AuthToken == "") {
		return ImportJob{}, ErrImportCredentials
	}
	target, err := s.repo.ResolveImportTarget(ctx, actorID, actorUsername, req.Owner)
	if err != nil {
		return ImportJob{}, err
	}
	if err := s.repo.CheckImportName(ctx, target.OwnerName, req.Name); err != nil {
		return ImportJob{}, err
	}
	id, err := newImportID()
	if err != nil {
		return ImportJob{}, err
	}
	job := &importJob{
		ImportJob: ImportJob{ID: id, UserID: actorID, SourceURL: src, Owner: target.OwnerName, Name: req.Name, Status: ImportQueued},
		progress:  &importProgress{},
	}

	s.mu.Lock()
	s.sweepLocked(time.Now())
	if s.activeLocked(actorID) >= importPerUserLimit {
		s.mu.Unlock()
		return ImportJob{}, ErrTooManyImports
	}
	s.jobs[id] = job
	snap := job.ImportJob
	s.mu.Unlock()

	auth := importAuth(req.AuthUsername, req.AuthToken)
	concurrency.Go("repo.import", func() { s.run(job, target, req.Description, req.Private, auth) })
	return snap, nil
}

// Get answers ErrImportNotFound for another user's job too, so a leaked ID reveals nothing.
func (s *ImportService) Get(userID int64, id string) (ImportJob, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sweepLocked(time.Now())
	job, ok := s.jobs[id]
	if !ok || job.UserID != userID {
		return ImportJob{}, ErrImportNotFound
	}
	snap := job.ImportJob
	snap.Progress = job.progress.String()
	return snap, nil
}

func (s *ImportService) RemoveStaleTemp() error {
	return os.RemoveAll(filepath.Join(s.root, importTmpDirName))
}

func (s *ImportService) run(job *importJob, target ImportTarget, description string, private bool, auth transport.AuthMethod) {
	s.slots <- struct{}{}
	defer func() { <-s.slots }()
	s.setStatus(job, ImportRunning)

	guard := &importGuard{allowLocal: s.cfg.AllowLocalNetworks, maxBytes: s.maxBytes}
	ctx, cancel := s.jobContext(withImportGuard(context.Background(), guard))
	defer cancel()

	dir := filepath.Join(s.root, importTmpDirName, job.ID)
	defer func() { _ = os.RemoveAll(dir) }()

	var failure string
	if err := s.cloneAndPublish(ctx, dir, job, target, description, private, auth); err != nil {
		failure = s.failureMessage(ctx, job, guard, err)
	}
	s.finish(job, failure)
}

func (s *ImportService) jobContext(parent context.Context) (context.Context, context.CancelFunc) {
	if s.cfg.Timeout > 0 {
		return context.WithTimeout(parent, s.cfg.Timeout)
	}
	return context.WithCancel(parent)
}

func (s *ImportService) cloneAndPublish(ctx context.Context, dir string, job *importJob, target ImportTarget, description string, private bool, auth transport.AuthMethod) error {
	if err := os.MkdirAll(filepath.Dir(dir), 0o755); err != nil {
		return fmt.Errorf("create import temp dir: %w", err)
	}
	branch, err := cloneForImport(ctx, dir, job.SourceURL, auth, job.progress)
	if err != nil {
		return err
	}
	_, err = s.repo.CreateFromImport(ctx, target, job.Name, description, private, branch, dir)
	return err
}

func (s *ImportService) failureMessage(ctx context.Context, job *importJob, guard *importGuard, err error) string {
	var blocked *ImportBlockedError
	stopped := guard.failure()
	switch {
	case errors.As(stopped, &blocked):
		return blocked.Host + " resolves to a private network address. An administrator can allow this with import.allow_local_networks."
	case errors.Is(stopped, ErrImportTooLarge):
		return "The repository is larger than this instance's limit of " + formatImportBytes(s.maxBytes) + "."
	case errors.Is(ctx.Err(), context.DeadlineExceeded):
		return "The import took longer than " + formatImportTimeout(s.cfg.Timeout) + " and was stopped."
	case errors.Is(err, transport.ErrAuthenticationRequired), errors.Is(err, transport.ErrAuthorizationFailed),
		errors.Is(err, transport.ErrRepositoryNotFound):
		return "Repository not found, or it needs a username and token."
	case errors.Is(err, ErrImportEmptySource):
		return "The source repository is empty — create a new repository instead."
	case errors.Is(err, ErrRepoNameTaken):
		return job.Owner + "/" + job.Name + " was created while the import ran."
	case errors.Is(err, ErrForbidden):
		return "You can no longer create repositories under " + job.Owner + "."
	}
	slog.Error("repo import failed", "job_id", job.ID, "owner", job.Owner, "name", job.Name,
		"source_host", importHost(job.SourceURL), "error", err)
	return "The import failed."
}

func (s *ImportService) setStatus(job *importJob, status ImportStatus) {
	s.mu.Lock()
	defer s.mu.Unlock()
	job.Status = status
}

func (s *ImportService) finish(job *importJob, failure string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	job.Status, job.FinishedAt = ImportDone, time.Now()
	if failure != "" {
		job.Status, job.Error = ImportFailed, failure
	}
}

func (s *ImportService) sweepLocked(now time.Time) {
	for id, job := range s.jobs {
		if job.Finished() && now.Sub(job.FinishedAt) > importJobRetention {
			delete(s.jobs, id)
		}
	}
}

func (s *ImportService) activeLocked(userID int64) int {
	n := 0
	for _, job := range s.jobs {
		if job.UserID == userID && !job.Finished() {
			n++
		}
	}
	return n
}

func newImportID() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

func importHost(src string) string {
	u, err := url.Parse(src)
	if err != nil {
		return ""
	}
	return u.Host
}

func formatImportBytes(n int64) string {
	const gib, mib = 1 << 30, 1 << 20
	if n >= gib && n%gib == 0 {
		return fmt.Sprintf("%d GiB", n/gib)
	}
	return fmt.Sprintf("%d MiB", (n+mib-1)/mib)
}

// formatImportTimeout drops Duration.String's zero units: 30m, not 30m0s.
func formatImportTimeout(d time.Duration) string {
	s := d.String()
	if strings.HasSuffix(s, "m0s") {
		s = s[:len(s)-2]
	}
	if strings.HasSuffix(s, "h0m") {
		s = s[:len(s)-2]
	}
	return s
}
```

- [ ] **Step 5: Wire the service**

In `internal/service/services.go`, add to the `Services` struct after `Language *LanguageService`:

```go
	Import           *ImportService
```

and to the returned literal after `Language: languageSvc,`:

```go
		Import:           NewImportService(repoSvc, cfg.Git, cfg.Import),
```

In `cmd/server/main.go`, right after `services := service.New(stores, cfg)`:

```go
	if err := services.Import.RemoveStaleTemp(); err != nil {
		slog.Warn("remove clones of interrupted imports failed", "error", err)
	}
```

- [ ] **Step 6: Run the tests**

Run: `TEST_DATABASE_DSN='postgres://cloudzilla:cloudzilla@localhost:5432/cloudzilla_test?sslmode=disable' go test ./internal/service -run 'TestImportService_|TestFormatImportLimits|TestImport_|TestCloneForImport|TestImportClient' -race -v`
Expected: PASS, none skipped. Then `go build ./...` with no output.

- [ ] **Step 7: Commit**

```bash
git add internal/service/import_service.go internal/service/import_service_internal_test.go internal/service/import_service_test.go internal/service/services.go cmd/server/main.go
git commit -m "feat(import): run repository imports as background jobs

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 5: JSON API

**Files:**
- Modify: `internal/model/audit_log.go`
- Create: `internal/handler/repo_import_handler.go`
- Modify: `internal/router/router.go`
- Test: `internal/router/repo_import_test.go` (package `router_test`, needs `TEST_DATABASE_DSN`)

**Interfaces:**
- Consumes: `service.ImportRequest`, `service.ImportJob`, `Services.Import.Start/Get`, the error values from Tasks 2–4; router-test helpers `serve`, `makeJWT`, `testCSRF`, `testJWTSecret` (existing in `internal/router`).
- Produces: `model.AuditActionRepoImport = "repo.import"`; handlers `StartImport`, `GetImportJob`; test helpers `newImportRouter(t, allowLocal bool) (http.Handler, *service.Services, *sql.DB)`, `importUser(t, db) (id int64, name, jwt string)`, `importJSONRequest(method, target, session, body string) *http.Request`, `waitRouterImport(t, svc, userID, id) service.ImportJob`, `postImport(t, h, jwt, body string) importJSON` (Task 6 reuses all of them).

- [ ] **Step 1: Write the failing router tests**

Create `internal/router/repo_import_test.go`:

```go
package router_test

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/mkappworks-dev/cloudzilla-app/internal/config"
	"github.com/mkappworks-dev/cloudzilla-app/internal/router"
	"github.com/mkappworks-dev/cloudzilla-app/internal/service"
	"github.com/mkappworks-dev/cloudzilla-app/internal/store"
	"github.com/mkappworks-dev/cloudzilla-app/internal/testutil"
)

func newImportRouter(t *testing.T, allowLocal bool) (http.Handler, *service.Services, *sql.DB) {
	t.Helper()
	db := testutil.OpenTestDB(t)
	// With no account left, every route redirects to /setup.
	testutil.SeedUser(t, db, testutil.UniqueSuffix(t))
	cfg := &config.Config{
		Server: config.ServerConfig{BaseURL: "http://localhost"},
		Auth:   config.AuthConfig{JWTSecret: testJWTSecret, JWTExpiry: time.Hour, CookieName: "cz_token"},
		Git:    config.GitConfig{ReposRoot: t.TempDir()},
		Import: config.ImportConfig{AllowLocalNetworks: allowLocal, Timeout: time.Minute},
	}
	svc := service.New(store.New(db), cfg)
	h, err := router.New(svc, cfg, fstest.MapFS{})
	if err != nil {
		t.Fatalf("router.New: %v", err)
	}
	return h, svc, db
}

func importUser(t *testing.T, db *sql.DB) (int64, string, string) {
	t.Helper()
	suffix := testutil.UniqueSuffix(t)
	id := testutil.SeedUser(t, db, suffix)
	name := "testuser_" + suffix
	t.Cleanup(func() { testutil.Exec(t, db, `DELETE FROM repositories WHERE owner_name = $1`, name) })
	return id, name, makeJWT(t, id, name)
}

func importJSONRequest(method, target, session, body string) *http.Request {
	req := httptest.NewRequest(method, target, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: "cz_token", Value: session})
	req.AddCookie(&http.Cookie{Name: "csrf_token", Value: testCSRF})
	req.Header.Set("X-CSRF-Token", testCSRF)
	return req
}

type importJSON struct {
	ID        string `json:"id"`
	Status    string `json:"status"`
	Owner     string `json:"owner"`
	Name      string `json:"name"`
	Error     string `json:"error"`
	StatusURL string `json:"status_url"`
}

func postImport(t *testing.T, h http.Handler, jwt, body string) importJSON {
	t.Helper()
	rr := serve(h, importJSONRequest(http.MethodPost, "/api/imports", jwt, body))
	if rr.Code != http.StatusAccepted {
		t.Fatalf("POST /api/imports = %d: %s", rr.Code, rr.Body)
	}
	var job importJSON
	if err := json.Unmarshal(rr.Body.Bytes(), &job); err != nil {
		t.Fatalf("decode: %v", err)
	}
	return job
}

func waitRouterImport(t *testing.T, svc *service.Services, userID int64, id string) service.ImportJob {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for {
		job, err := svc.Import.Get(userID, id)
		if err != nil {
			t.Fatalf("Get(%s): %v", id, err)
		}
		if job.Finished() {
			return job
		}
		if time.Now().After(deadline) {
			t.Fatalf("import %s still %s", id, job.Status)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// A loopback source is refused at connect time under the default config, so
// these jobs fail fast without touching the network.
const refusedImportBody = `{"clone_url":"http://127.0.0.1:1/x.git","name":"refused"}`

func TestStartImport_Accepted(t *testing.T) {
	h, svc, db := newImportRouter(t, false)
	uid, uname, jwt := importUser(t, db)

	job := postImport(t, h, jwt, refusedImportBody)
	if job.ID == "" || job.Status != "queued" || job.Owner != uname || job.StatusURL != "/repos/import/"+job.ID {
		t.Errorf("response = %+v", job)
	}
	done := waitRouterImport(t, svc, uid, job.ID)
	if done.Status != service.ImportFailed || !strings.HasPrefix(done.Error, "127.0.0.1 resolves to a private network address") {
		t.Errorf("job = %s %q", done.Status, done.Error)
	}
}

func TestStartImport_RejectsBadInput(t *testing.T) {
	h, _, db := newImportRouter(t, false)
	_, _, jwt := importUser(t, db)
	for _, tc := range []struct {
		body, want string
	}{
		{`{"clone_url":"/srv/repos/a/b.git","name":"x"}`, service.ErrImportURL.Error()},
		{`{"clone_url":"https://example.com/a.git","auth_token":"t","name":"x"}`, service.ErrImportCredentials.Error()},
		{`{"clone_url":"https://example.com/a.git","name":"bad name"}`, ""},
	} {
		rr := serve(h, importJSONRequest(http.MethodPost, "/api/imports", jwt, tc.body))
		if rr.Code != http.StatusUnprocessableEntity {
			t.Errorf("%s: status %d, want 422: %s", tc.body, rr.Code, rr.Body)
			continue
		}
		if tc.want != "" && !strings.Contains(rr.Body.String(), tc.want) {
			t.Errorf("%s: body %s, want %q", tc.body, rr.Body, tc.want)
		}
	}
}

func TestGetImportJob_OnlyForItsUser(t *testing.T) {
	h, svc, db := newImportRouter(t, false)
	uid, _, jwt := importUser(t, db)
	_, _, otherJWT := importUser(t, db)
	job := postImport(t, h, jwt, refusedImportBody)
	waitRouterImport(t, svc, uid, job.ID)

	rr := serve(h, importJSONRequest(http.MethodGet, "/api/imports/"+job.ID, jwt, ""))
	var got importJSON
	if rr.Code != http.StatusOK || json.Unmarshal(rr.Body.Bytes(), &got) != nil || got.Status != "failed" || got.Error == "" {
		t.Errorf("owner GET = %d %s", rr.Code, rr.Body)
	}
	if rr := serve(h, importJSONRequest(http.MethodGet, "/api/imports/"+job.ID, otherJWT, "")); rr.Code != http.StatusNotFound {
		t.Errorf("other user GET = %d, want 404", rr.Code)
	}
}
```

- [ ] **Step 2: Run them to see them fail**

Run: `TEST_DATABASE_DSN='postgres://cloudzilla:cloudzilla@localhost:5432/cloudzilla_test?sslmode=disable' go test ./internal/router -run 'TestStartImport|TestGetImportJob'`
Expected: FAIL. `POST /api/imports` answers 404 (or 405), not 202.

- [ ] **Step 3: Add the audit action**

In `internal/model/audit_log.go`, after `AuditActionRepoCreate`:

```go
	AuditActionRepoImport          = "repo.import"
```

(Keep the block's alignment: run `gofmt -w internal/model/audit_log.go`.)

- [ ] **Step 4: Implement the handlers**

Create `internal/handler/repo_import_handler.go`:

```go
package handler

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/mkappworks-dev/cloudzilla-app/internal/middleware"
	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/service"
)

type startImportRequest struct {
	CloneURL     string `json:"clone_url"`
	AuthUsername string `json:"auth_username"`
	AuthToken    string `json:"auth_token"`
	Owner        string `json:"owner"`
	Name         string `json:"name"`
	Description  string `json:"description"`
	Private      bool   `json:"private"`
}

type importJobResponse struct {
	ID        string `json:"id"`
	Status    string `json:"status"`
	Owner     string `json:"owner"`
	Name      string `json:"name"`
	Progress  string `json:"progress,omitempty"`
	Error     string `json:"error,omitempty"`
	StatusURL string `json:"status_url"`
}

func importJobJSON(j service.ImportJob) importJobResponse {
	return importJobResponse{
		ID: j.ID, Status: string(j.Status), Owner: j.Owner, Name: j.Name,
		Progress: j.Progress, Error: j.Error, StatusURL: "/repos/import/" + j.ID,
	}
}

func (h *Handler) StartImport(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.ClaimsFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	var req startImportRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	job, err := h.Services.Import.Start(r.Context(), claims.UserID, claims.Username, service.ImportRequest{
		CloneURL:     req.CloneURL,
		AuthUsername: req.AuthUsername,
		AuthToken:    req.AuthToken,
		Owner:        req.Owner,
		Name:         req.Name,
		Description:  req.Description,
		Private:      req.Private,
	})
	switch {
	case errors.Is(err, service.ErrImportURL), errors.Is(err, service.ErrImportURLUserinfo),
		errors.Is(err, service.ErrImportCredentials),
		errors.Is(err, service.ErrRepoNameTaken), errors.Is(err, service.ErrRepoNameReserved):
		writeError(w, http.StatusUnprocessableEntity, err.Error())
		return
	case errors.Is(err, service.ErrInvalidRepoName):
		writeError(w, http.StatusUnprocessableEntity, invalidRepoNameMessage)
		return
	case errors.Is(err, service.ErrForbidden):
		writeError(w, http.StatusForbidden, "you can import only into your account or an organization you own")
		return
	case errors.Is(err, service.ErrTooManyImports):
		writeError(w, http.StatusTooManyRequests, err.Error())
		return
	case err != nil:
		slog.Error("start repo import", "user_id", claims.UserID, "error", err)
		writeError(w, http.StatusInternalServerError, "failed to start the import")
		return
	}

	// Recorded at start, not on success: a refused request to a private address belongs in the log.
	h.Services.AuditLog.Record(r.Context(), r, claims.UserID, claims.Username, model.AuditActionRepoImport, model.AuditTargetRepo, 0, job.Name,
		map[string]any{"owner": job.Owner, "source_url": job.SourceURL, "job_id": job.ID})
	writeJSON(w, http.StatusAccepted, importJobJSON(job))
}

func (h *Handler) GetImportJob(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.ClaimsFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	job, err := h.Services.Import.Get(claims.UserID, chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusNotFound, "import not found")
		return
	}
	writeJSON(w, http.StatusOK, importJobJSON(job))
}
```

- [ ] **Step 5: Add the routes**

In `internal/router/router.go`, directly above the `// Repo routes` comment that precedes `r.Route("/api/repos", ...)`:

```go
	// Not under /api/repos: GET /api/repos/import/{id} would shadow /api/repos/{owner}/{repo} for a user named "import".
	r.With(authMW, apiBodyLimit).Post("/api/imports", h.StartImport)
	r.With(authMW).Get("/api/imports/{id}", h.GetImportJob)
```

- [ ] **Step 6: Run the tests**

Run: `TEST_DATABASE_DSN='postgres://cloudzilla:cloudzilla@localhost:5432/cloudzilla_test?sslmode=disable' go test ./internal/router -run 'TestStartImport|TestGetImportJob' -race -v`
Expected: PASS, 3 tests.

- [ ] **Step 7: Commit**

```bash
git add internal/model/audit_log.go internal/handler/repo_import_handler.go internal/router/router.go internal/router/repo_import_test.go
git commit -m "feat(api): start and inspect repository imports

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 6: Import pages

**Files:**
- Modify: `internal/view/viewmodels_repo.go`
- Modify: `internal/view/pages/repo_new.templ`
- Create: `internal/view/pages/repo_import.templ`
- Modify: `internal/handler/repo_import_handler.go`
- Modify: `internal/router/router.go`
- Test: `internal/router/repo_import_test.go`

**Interfaces:**
- Consumes: `service.ImportJob`, `service.ImportQueued/Done/Failed`; the Task 5 test helpers; `testutil.SeedSourceRepo`, `testutil.ServeGitHTTP`.
- Produces: `view.RepoImportData`, `view.RepoImportStatusData`; templ components `pages.RepoImport`, `pages.RepoImportStatus`, `pages.RepoImportStatusPanel`, helpers `repoOwnerOpts(me string, orgs []model.Organization)` and `repoVisibilityFieldset(private bool)`; handlers `PageImportRepo`, `PageImportStatus`.

- [ ] **Step 1: Write the failing page tests**

Append to `internal/router/repo_import_test.go` (add `"fmt"` to its imports):

```go
func TestImportPage_RendersWithPrefill(t *testing.T) {
	h, _, db := newImportRouter(t, false)
	_, _, jwt := importUser(t, db)

	rr := serve(h, browserRequest(http.MethodGet, "/repos/import?url=https://example.com/a.git&name=prefilled", jwt, nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("GET /repos/import = %d", rr.Code)
	}
	for _, want := range []string{`id="import-form"`, `value="https://example.com/a.git"`, `value="prefilled"`} {
		if !strings.Contains(rr.Body.String(), want) {
			t.Errorf("page lacks %s", want)
		}
	}
}

func TestImportStatusPage_FailedJob(t *testing.T) {
	h, svc, db := newImportRouter(t, false)
	uid, _, jwt := importUser(t, db)
	_, _, otherJWT := importUser(t, db)
	job := postImport(t, h, jwt, refusedImportBody)
	waitRouterImport(t, svc, uid, job.ID)

	rr := serve(h, browserRequest(http.MethodGet, "/repos/import/"+job.ID, jwt, nil))
	if rr.Code != http.StatusOK || !strings.Contains(rr.Body.String(), "127.0.0.1 resolves to a private network address") ||
		!strings.Contains(rr.Body.String(), "Try again") {
		t.Errorf("owner page = %d, want the failure and a retry link", rr.Code)
	}
	if rr := serve(h, browserRequest(http.MethodGet, "/repos/import/"+job.ID, otherJWT, nil)); rr.Code != http.StatusNotFound {
		t.Errorf("other user = %d, want 404", rr.Code)
	}
}

func TestImportStatus_DoneRedirectsToTheRepo(t *testing.T) {
	h, svc, db := newImportRouter(t, true)
	uid, uname, jwt := importUser(t, db)
	url := testutil.ServeGitHTTP(t, testutil.SeedSourceRepo(t).Dir, "", "")
	job := postImport(t, h, jwt, fmt.Sprintf(`{"clone_url":%q,"name":"imported"}`, url))
	if done := waitRouterImport(t, svc, uid, job.ID); done.Status != service.ImportDone {
		t.Fatalf("import %s: %s", done.Status, done.Error)
	}
	repoURL := "/" + uname + "/imported"

	frag := browserRequest(http.MethodGet, "/repos/import/"+job.ID, jwt, nil)
	frag.Header.Set("HX-Request", "true")
	rr := serve(h, frag)
	if rr.Code != http.StatusOK || rr.Header().Get("HX-Redirect") != repoURL || strings.Contains(rr.Body.String(), "<html") {
		t.Errorf("fragment = %d, HX-Redirect %q", rr.Code, rr.Header().Get("HX-Redirect"))
	}
	rr = serve(h, browserRequest(http.MethodGet, "/repos/import/"+job.ID, jwt, nil))
	if rr.Code != http.StatusSeeOther || rr.Header().Get("Location") != repoURL {
		t.Errorf("page = %d, Location %q; want 303 to %s", rr.Code, rr.Header().Get("Location"), repoURL)
	}
}
```

- [ ] **Step 2: Run them to see them fail**

Run: `TEST_DATABASE_DSN='postgres://cloudzilla:cloudzilla@localhost:5432/cloudzilla_test?sslmode=disable' go test ./internal/router -run 'TestImportPage|TestImportStatus'`
Expected: FAIL. The pages answer 404.

- [ ] **Step 3: Add the view-models**

Append to `internal/view/viewmodels_repo.go`:

```go
// RepoImportData holds template data for the import form; the Default*
// fields come from query params, so "Try again" can prefill it.
type RepoImportData struct {
	BasePage
	OwnedOrgs      []model.Organization
	DefaultOwner   string
	DefaultPrivate bool
	DefaultURL     string
	DefaultName    string
}

// RepoImportStatusData holds template data for an import's status page.
type RepoImportStatusData struct {
	BasePage
	Job service.ImportJob
}
```

- [ ] **Step 4: Share the owner options and visibility fieldset**

In `internal/view/pages/repo_new.templ`:

1. Add `"github.com/mkappworks-dev/cloudzilla-app/internal/model"` to the import block.
2. Replace `{{ ownerOpts := repoNewOwnerOpts(data) }}` with `{{ ownerOpts := repoOwnerOpts(me, data.OwnedOrgs) }}`.
3. Cut the whole `<fieldset>` … `</fieldset>` visibility block out of `RepoNew` and put `@repoVisibilityFieldset(data.DefaultPrivate)` in its place.
4. Add this component below `RepoNew`. Its body is the fieldset you cut, with the two `checked?=` expressions now reading `private`:

```templ
// repoVisibilityFieldset is the public/private choice shared by the new and import forms.
templ repoVisibilityFieldset(private bool) {
	<fieldset>
		<legend class="text-[12px] font-medium text-muted-foreground mb-2">Visibility</legend>
		<label class="border border-border rounded-md p-3 flex items-start gap-3 mb-2 cursor-pointer hover:bg-accent">
			<input type="radio" name="visibility" value="public" checked?={ !private } class="mt-0.5 accent-[hsl(var(--ring))]"/>
			<div>
				<p class="text-[13.5px] font-medium inline-flex items-center gap-2">
					<svg width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.75" class="text-muted-foreground" aria-hidden="true"><circle cx="12" cy="12" r="10"></circle><line x1="2" y1="12" x2="22" y2="12"></line><path d="M12 2a15.3 15.3 0 0 1 4 10 15.3 15.3 0 0 1-4 10 15.3 15.3 0 0 1-4-10 15.3 15.3 0 0 1 4-10z"></path></svg>
					Public
				</p>
				<p class="mt-0.5 text-[12.5px] text-muted-foreground/70">Anyone on the internet can see this repository. You choose who can commit.</p>
			</div>
		</label>
		<label class="border border-border rounded-md p-3 flex items-start gap-3 cursor-pointer hover:bg-accent">
			<input type="radio" name="visibility" value="private" checked?={ private } class="mt-0.5 accent-[hsl(var(--ring))]"/>
			<div>
				<p class="text-[13.5px] font-medium inline-flex items-center gap-2">
					<svg width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.75" class="text-muted-foreground" aria-hidden="true"><rect x="3" y="11" width="18" height="11" rx="2" ry="2"></rect><path d="M7 11V7a5 5 0 0110 0v4"></path></svg>
					Private
				</p>
				<p class="mt-0.5 text-[12.5px] text-muted-foreground/70">You choose who can see and commit to this repository.</p>
			</div>
		</label>
	</fieldset>
}
```

5. Replace the Go func `repoNewOwnerOpts` with:

```go
func repoOwnerOpts(me string, orgs []model.Organization) []repoNewSelectOption {
	var opts []repoNewSelectOption
	if me != "" {
		opts = append(opts, repoNewSelectOption{Value: me, Label: me, Visibility: "public"})
	}
	for _, o := range orgs {
		vis := "private"
		if o.DefaultRepoVisibility == "public" {
			vis = "public"
		}
		opts = append(opts, repoNewSelectOption{Value: o.Name, Label: o.Name, Visibility: vis})
	}
	return opts
}
```

6. Change the bottom link `<a href="#" class="text-primary hover:underline">Import a repository →</a>` to `<a href="/repos/import" class="text-primary hover:underline">Import a repository →</a>`.

Run: `make generate-templ && go build ./...`
Expected: no errors.

- [ ] **Step 5: Add the import templates**

Create `internal/view/pages/repo_import.templ`:

```templ
package pages

import (
	"net/url"

	"github.com/mkappworks-dev/cloudzilla-app/internal/service"
	"github.com/mkappworks-dev/cloudzilla-app/internal/view"
	"github.com/mkappworks-dev/cloudzilla-app/internal/view/components"
	"github.com/mkappworks-dev/cloudzilla-app/internal/view/layout"
)

templ RepoImport(data view.RepoImportData) {
	{{ var me string }}
	if data.CurrentUser != nil {
		{{ me = data.CurrentUser.Username }}
	}
	{{ initialOwner := data.DefaultOwner }}
	{{ if initialOwner == "" { initialOwner = me } }}
	@layout.Base(data.BasePage, "Import repository") {
		<section class="mb-6">
			<h1 class="text-2xl font-semibold tracking-tight">Import a repository</h1>
			<p class="mt-1 text-[13px] text-muted-foreground">Copy a Git repository from another host, with its full history, branches, and tags.</p>
		</section>

		<div class="grid lg:grid-cols-[1fr_280px] gap-8">
			<div class="min-w-0">
				<form id="import-form" class="border border-border rounded-lg bg-card p-6 space-y-6">
					<div>
						<label for="import-url" class="block text-[12px] font-medium text-muted-foreground mb-1.5">Source repository URL *</label>
						<input id="import-url" name="clone_url" type="url" required autofocus placeholder="https://github.com/owner/repo.git" value={ data.DefaultURL } class="w-full bg-background border border-border rounded-md px-2.5 py-2 text-[13px] text-foreground font-mono placeholder:text-muted-foreground/70 focus:outline-hidden focus:border-ring"/>
						<p class="mt-1 text-[12px] text-muted-foreground/70">An HTTP or HTTPS clone URL. SSH URLs aren't supported.</p>
					</div>

					<div x-data="{ open: false }" class="flex flex-col gap-3">
						<button type="button" @click="open = !open" :aria-expanded="open ? 'true' : 'false'" aria-controls="import-credentials" class="self-start inline-flex items-center gap-1.5 text-[13px] text-primary hover:underline">
							<svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" aria-hidden="true" focusable="false" class="transition-transform" :class="open && 'rotate-90'"><path d="M9 18l6-6-6-6"></path></svg>
							The source needs a username and token
						</button>
						<div id="import-credentials" x-show="open" x-cloak class="grid sm:grid-cols-2 gap-3">
							<div>
								<label for="import-username" class="block text-[12px] font-medium text-muted-foreground mb-1.5">Username</label>
								<input id="import-username" name="auth_username" type="text" autocomplete="off" spellcheck="false" class="w-full bg-background border border-border rounded-md px-2.5 py-2 text-[13px] text-foreground focus:outline-hidden focus:border-ring"/>
							</div>
							<div>
								<label for="import-token" class="block text-[12px] font-medium text-muted-foreground mb-1.5">Access token</label>
								<input id="import-token" name="auth_token" type="password" autocomplete="new-password" class="w-full bg-background border border-border rounded-md px-2.5 py-2 text-[13px] text-foreground focus:outline-hidden focus:border-ring"/>
							</div>
							<p class="sm:col-span-2 text-[12px] text-muted-foreground/70">Used once for this import and never stored. A token with read access to the repository is enough.</p>
						</div>
					</div>

					<div class="grid grid-cols-[1fr_auto_2fr] gap-3 items-end">
						<div>
							<label for="import-owner-btn" class="block text-[12px] font-medium text-muted-foreground mb-1.5">Owner *</label>
							@repoNewCustomSelect("import-owner", "owner", repoOwnerOpts(me, data.OwnedOrgs), repoNewSelectOption{Value: initialOwner, Label: initialOwner})
						</div>
						<span class="text-muted-foreground/70 pb-2.5" aria-hidden="true">/</span>
						<div>
							<label for="import-name" class="block text-[12px] font-medium text-muted-foreground mb-1.5">Repository name *</label>
							<input id="import-name" name="name" type="text" required pattern="[A-Za-z0-9._\-]+" placeholder="my-repo" value={ data.DefaultName } class="w-full bg-background border border-border rounded-md px-2.5 py-2 text-[13px] text-foreground font-mono placeholder:text-muted-foreground/70 focus:outline-hidden focus:border-ring"/>
						</div>
					</div>

					<div>
						<label for="import-description" class="block text-[12px] font-medium text-muted-foreground mb-1.5">
							Description <span class="text-muted-foreground/70 font-normal">(optional)</span>
						</label>
						<textarea id="import-description" name="description" rows="2" placeholder="A short description of your project" class="w-full bg-background border border-border rounded-md px-2.5 py-2 text-[13px] text-foreground placeholder:text-muted-foreground/70 focus:outline-hidden focus:border-ring"></textarea>
					</div>

					@repoVisibilityFieldset(data.DefaultPrivate)

					<div id="import-form-error" role="alert" class="hidden text-sm text-destructive bg-destructive/10 border border-destructive/40 rounded-md px-3 py-2"></div>

					<div class="flex items-center justify-end gap-2 pt-2 border-t border-border">
						@components.LinkButton("/", components.ButtonOutline, components.ButtonSizeDefault, nil) {
							Cancel
						}
						@components.Button(components.ButtonSuccess, components.ButtonSizeDefault, templ.Attributes{"type": "submit"}) {
							Begin import
						}
					</div>
				</form>
			</div>

			<aside class="space-y-6" aria-label="About importing">
				<section aria-labelledby="ri-what">
					<h2 id="ri-what" class="text-[12px] font-medium text-muted-foreground mb-2">What gets imported</h2>
					<p class="text-[13px] text-muted-foreground/70 leading-relaxed">Every branch and tag, with full history. The new repository's default branch matches the source's.</p>
				</section>
				<section aria-labelledby="ri-not" class="border-t border-border pt-5">
					<h2 id="ri-not" class="text-[12px] font-medium text-muted-foreground mb-2">Not imported</h2>
					<ul class="space-y-2 text-[13px] text-muted-foreground/70 leading-relaxed">
						<li>Issues, pull requests, wikis, and releases.</li>
						<li>Git LFS files: only their pointer files come across.</li>
						<li>Later changes to the source. An import is a one-time copy.</li>
					</ul>
				</section>
			</aside>
		</div>

		<script>
			(function() {
				const form = document.getElementById('import-form');
				const errBox = document.getElementById('import-form-error');
				const urlInput = document.getElementById('import-url');
				const nameInput = document.getElementById('import-name');
				let nameEdited = nameInput.value !== '';
				nameInput.addEventListener('input', function() { nameEdited = true; });
				urlInput.addEventListener('input', function() {
					if (nameEdited) return;
					const path = urlInput.value.trim().replace(/[?#].*$/, '').replace(/\/+$/, '');
					nameInput.value = path.slice(path.lastIndexOf('/') + 1).replace(/\.git$/, '').replace(/[^A-Za-z0-9._-]/g, '-');
				});
				form.addEventListener('submit', async function(e) {
					e.preventDefault();
					errBox.classList.add('hidden');
					const submit = form.querySelector('[type=submit]');
					submit.disabled = true;
					const csrf = (document.cookie.match(/csrf_token=([^;]+)/) || [])[1] || '';
					try {
						const res = await fetch('/api/imports', {
							method: 'POST',
							headers: {'Content-Type': 'application/json', 'X-CSRF-Token': csrf},
							body: JSON.stringify({
								clone_url: urlInput.value.trim(),
								auth_username: document.getElementById('import-username').value,
								auth_token: document.getElementById('import-token').value,
								owner: form.querySelector('[name=owner]').value,
								name: nameInput.value.trim(),
								description: form.querySelector('[name=description]').value,
								private: form.querySelector('[name=visibility]:checked').value === 'private'
							})
						});
						if (res.ok) {
							window.location.href = (await res.json()).status_url;
							return;
						}
						const text = await res.text();
						let msg = text;
						try { msg = JSON.parse(text).error || text; } catch (_) {}
						errBox.textContent = msg || ('Failed to start the import (HTTP ' + res.status + ')');
						errBox.classList.remove('hidden');
					} finally {
						submit.disabled = false;
					}
				});
			})();
		</script>
	}
}

templ RepoImportStatus(data view.RepoImportStatusData) {
	@layout.Base(data.BasePage, "Import "+data.Job.Owner+"/"+data.Job.Name) {
		<section class="mb-6">
			<h1 class="text-2xl font-semibold tracking-tight">Importing <span class="font-mono">{ data.Job.Owner }/{ data.Job.Name }</span></h1>
			<p class="mt-1 text-[13px] text-muted-foreground font-mono break-all">{ data.Job.SourceURL }</p>
		</section>
		@RepoImportStatusPanel(data.Job)
	}
}

// RepoImportStatusPanel polls its own URL until the import finishes; the
// handler then answers the poll with HX-Redirect to the new repository.
templ RepoImportStatusPanel(job service.ImportJob) {
	switch job.Status {
		case service.ImportDone:
			<div id="import-status" class="border border-border rounded-lg bg-card p-6" role="status">
				<p class="text-[13.5px] font-medium">Import complete.</p>
				<a href={ templ.SafeURL("/" + job.Owner + "/" + job.Name) } class="mt-2 inline-block text-[13px] text-primary hover:underline">Go to { job.Owner }/{ job.Name } →</a>
			</div>
		case service.ImportFailed:
			<div id="import-status" class="border border-destructive/40 bg-destructive/10 rounded-lg p-6" role="alert">
				<p class="text-[13.5px] font-medium text-destructive">The import failed</p>
				<p class="mt-1 text-[13px] text-foreground">{ job.Error }</p>
				<div class="mt-4">
					@components.LinkButton(repoImportRetryURL(job), components.ButtonOutline, components.ButtonSizeSM, nil) {
						Try again
					}
				</div>
			</div>
		default:
			<div id="import-status" hx-get={ "/repos/import/" + job.ID } hx-trigger="every 2s" hx-swap="outerHTML" class="border border-border rounded-lg bg-card p-6" role="status" aria-live="polite">
				<p class="text-[13.5px] font-medium inline-flex items-center gap-2">
					<svg class="animate-spin text-muted-foreground" width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" aria-hidden="true" focusable="false"><path d="M21 12a9 9 0 11-6.219-8.56"></path></svg>
					if job.Status == service.ImportQueued {
						Waiting for other imports to finish…
					} else {
						Importing…
					}
				</p>
				if job.Progress != "" {
					<p class="mt-2 text-[12.5px] text-muted-foreground font-mono break-all">{ job.Progress }</p>
				}
				<p class="mt-4 text-[12.5px] text-muted-foreground/70">You can leave this page: the import keeps running. Large repositories can take several minutes.</p>
			</div>
	}
}

func repoImportRetryURL(job service.ImportJob) string {
	q := url.Values{"url": {job.SourceURL}, "owner": {job.Owner}, "name": {job.Name}}
	return "/repos/import?" + q.Encode()
}
```

Run: `make generate-templ && go build ./...`
Expected: no errors. A templ "expected nodes" error pointing into prose means a text line began with `for`/`if`/`switch`; reword it.

- [ ] **Step 6: Add the page handlers**

Append to `internal/handler/repo_import_handler.go` (add `"github.com/mkappworks-dev/cloudzilla-app/internal/view"` and `"github.com/mkappworks-dev/cloudzilla-app/internal/view/pages"` to its imports):

```go
func (h *Handler) PageImportRepo(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.ClaimsFromContext(r.Context())
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	ctx := r.Context()
	orgs, err := h.Services.Org.ListOwnedByUser(ctx, claims.UserID)
	if err != nil {
		slog.Error("list owned orgs", "error", err)
		orgs = []model.Organization{}
	}
	q := r.URL.Query()
	// As on /repos/new, ?owner= is honored only for an org the viewer owns.
	owner, private := claims.Username, false
	for _, o := range orgs {
		if o.Name == q.Get("owner") {
			owner, private = o.Name, o.DefaultRepoVisibility != "public"
			break
		}
	}
	h.render(w, r, pages.RepoImport(view.RepoImportData{
		BasePage:       withAccountSubnav(basePage(r, h.Services), "repositories", h.accountCounts(ctx, claims.UserID)),
		OwnedOrgs:      orgs,
		DefaultOwner:   owner,
		DefaultPrivate: private,
		DefaultURL:     q.Get("url"),
		DefaultName:    q.Get("name"),
	}))
}

func (h *Handler) PageImportStatus(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.ClaimsFromContext(r.Context())
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	job, err := h.Services.Import.Get(claims.UserID, chi.URLParam(r, "id"))
	if err != nil {
		h.NotFound(w, r)
		return
	}
	repoURL := "/" + job.Owner + "/" + job.Name
	if r.Header.Get("HX-Request") == "true" {
		if job.Status == service.ImportDone {
			w.Header().Set("HX-Redirect", repoURL)
		}
		h.render(w, r, pages.RepoImportStatusPanel(job))
		return
	}
	if job.Status == service.ImportDone {
		http.Redirect(w, r, repoURL, http.StatusSeeOther)
		return
	}
	h.render(w, r, pages.RepoImportStatus(view.RepoImportStatusData{
		BasePage: withAccountSubnav(basePage(r, h.Services), "repositories", h.accountCounts(r.Context(), claims.UserID)),
		Job:      job,
	}))
}
```

- [ ] **Step 7: Add the page routes**

In `internal/router/router.go`, after `r.With(authMW).Get("/repos/new", h.PageNewRepo)`:

```go
	r.With(authMW).Get("/repos/import", h.PageImportRepo)
	r.With(authMW).Get("/repos/import/{id}", h.PageImportStatus)
```

- [ ] **Step 8: Run the tests**

Run: `TEST_DATABASE_DSN='postgres://cloudzilla:cloudzilla@localhost:5432/cloudzilla_test?sslmode=disable' go test ./internal/router -run 'TestStartImport|TestGetImportJob|TestImportPage|TestImportStatus|NewRepo' -race -v`
Expected: PASS. Then `TEST_DATABASE_DSN=… go test ./internal/handler ./internal/view/...` also PASS (the `repo_new.templ` refactor must not change the New Repository page).

- [ ] **Step 9: Commit**

```bash
git add internal/view/viewmodels_repo.go internal/view/pages/repo_new.templ internal/view/pages/repo_new_templ.go internal/view/pages/repo_import.templ internal/view/pages/repo_import_templ.go internal/handler/repo_import_handler.go internal/router/router.go internal/router/repo_import_test.go
git commit -m "feat(ui): import form and status pages

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

The generated `*_templ.go` files are tracked, so they go in the same commit.

---

### Task 7: Docs

**Files:**
- Create: `docs/repo-import.md`
- Modify: `docs/configuration.md`
- Modify: `docs/api-reference.md`
- Modify: `CLAUDE.md`

- [ ] **Step 1: Write the subsystem doc**

Create `docs/repo-import.md`:

```markdown
# Repository import

`/repos/import` copies a Git repository from another host into a new Cloudzilla repository: every branch and tag, with HEAD on the source's default branch. Issues, pull requests, wikis, releases and LFS objects are not imported, and the copy does not track the source afterwards.

## Flow

1. `POST /api/imports` validates the request and returns `202` with the job ID. The form then opens `/repos/import/{id}`.
2. `ImportService` runs the job in a goroutine, at most 3 at once server-wide; later jobs wait as `queued`. A user may have 5 jobs queued or running.
3. The clone goes into `<git.repos_root>/.import-tmp/<job id>`: only `refs/heads/*` and `refs/tags/*` are fetched, and the `origin` remote is removed afterwards.
4. `RepoService.CreateFromImport` re-checks the user's right to the owner, claims the name, inserts the row and renames the clone into place.
5. The status page polls every 2 s and redirects to the repository when the job is done.

Jobs live in memory and are dropped an hour after they finish. A restart loses imports in flight; startup deletes `.import-tmp`.

## Security

- Only `http` and `https` URLs are accepted. go-git treats a bare path or `file://` URL as a repository on the server's own disk.
- Credentials go in the username and token fields, never the URL. They are used for one clone as HTTP basic auth and are not stored or logged.
- The go-git HTTP client is process-wide. When a request's context carries an import guard, the dialer resolves the host itself and refuses loopback, private, link-local, multicast, unspecified, `0.0.0.0/8` and `100.64.0.0/10` addresses, then connects to the vetted IP. Redirects re-dial through the same check. `import.allow_local_networks: true` turns the check off, for importing from a server on your own network.
- Imports ignore `HTTP(S)_PROXY`, and response bytes are capped by `git.max_pack_bytes`.
- Each import request is written to the audit log as `repo.import`, with the source URL, before the clone starts.

## Configuration

| Key | Default | Meaning |
| --- | --- | --- |
| `import.allow_local_networks` | `false` | Allow sources on private networks |
| `import.timeout` | `30m` | Time limit for one import |
| `git.max_pack_bytes` | 2 GiB | Also caps the bytes one import downloads |
```

- [ ] **Step 2: Update the reference docs**

In `docs/configuration.md`, add two rows to the Configuration Reference table, after the `smtp.tls` row:

```markdown
| `import.allow_local_networks`| `false`                                      | `CZ_IMPORT_ALLOW_LOCAL_NETWORKS`| Let repository imports reach loopback, private and link-local addresses |
| `import.timeout`             | `30m`                                        | `CZ_IMPORT_TIMEOUT`             | Time limit for one repository import            |
```

In `docs/api-reference.md`, add this section directly before `## Repository Transfers`:

```markdown
## Repository Imports

| Method | Path                | Auth     | Description |
| ------ | ------------------- | -------- | ----------- |
| POST   | `/api/imports`      | Required | Start importing a Git repository (`clone_url`, `name`, optional `owner`, `description`, `private`, `auth_username` + `auth_token`). Returns `202 {id, status, owner, name, status_url}` |
| GET    | `/api/imports/:id`  | Required | The import's `status` (`queued`, `running`, `done`, `failed`), `progress` and `error`. 404 for an unknown, expired or someone else's import |

`clone_url` must be `http://` or `https://` without credentials in it (422). `auth_username` and `auth_token` go together (422). `owner` defaults to the caller; another owner must be an organization the caller owns (403). A name already used under the owner is 422 `a repository with that name already exists`. A sixth import while five are queued or running is 429. Imports run in the background; see [repo-import](./repo-import.md).
```

In `CLAUDE.md`, add to the Subsystem docs list after the `pr-merge` line:

```markdown
- [repo-import](./docs/repo-import.md) — background clone jobs, SSRF guard on go-git's HTTP client, `import.*` config
```

- [ ] **Step 3: Commit**

```bash
git add docs/repo-import.md docs/configuration.md docs/api-reference.md CLAUDE.md
git commit -m "docs: document repository import

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 8: Verify end to end

- [ ] **Step 1: Full checks**

Run, each from the worktree root:

```bash
make generate-templ
```
```bash
go build ./... && go vet ./...
```
```bash
make lint
```
```bash
TEST_DATABASE_DSN='postgres://cloudzilla:cloudzilla@localhost:5432/cloudzilla_test?sslmode=disable' go test -race ./...
```
Expected: all pass, `git status` clean apart from nothing (generated files already committed).

- [ ] **Step 2: Browser check on a throwaway server**

Follow the throwaway-server recipe in memory `worktree-location.md` ("Previewing a throwaway server"): build the worktree's binary, `make setup-tailwind download-htmx build-css`, run it in the background on free ports with its own database and `auth.cookie_name`, then open it in the built-in browser. Never point it at the shared dev DB.

Check, in order:
1. The dashboard's "Import" button and the "Import a repository →" link on `/repos/new` both open `/repos/import`.
2. Typing `https://github.com/octocat/Hello-World.git` fills the name with `Hello-World`.
3. "Begin import" lands on `/repos/import/<id>`, shows "Importing…", then redirects to `/<you>/Hello-World`, whose branches and default branch match GitHub's.
4. Importing `http://127.0.0.1:<the server's own port>/x.git` fails with the private-network message, and "Try again" reopens the form prefilled (URL, owner, name; no token).
5. A second browser session as another user gets a 404 for the first user's `/repos/import/<id>`.

- [ ] **Step 3: Before opening a PR**

Re-check `git log HEAD..origin/main` and open PRs for overlapping work (memory `check-origin-main-before-committing.md`), then finish with superpowers:finishing-a-development-branch.
