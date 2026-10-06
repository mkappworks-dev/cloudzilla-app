package service

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	gitclient "github.com/go-git/go-git/v5/plumbing/transport/client"
	githttp "github.com/go-git/go-git/v5/plumbing/transport/http"
	"github.com/mkappworks-dev/cloudzilla-app/internal/gittransport"
)

var ErrImportTooLarge = errors.New("import exceeds the instance's size limit")

const (
	// go-git decodes the whole ref advertisement in memory, whatever git.max_pack_bytes says.
	importMaxRefsBytes = 64 << 20
	// go-git reads a whole non-2xx body into memory, only to use it as error text.
	importMaxErrorBodyBytes = 64 << 10
)

// importSizeError names the cap a response crossed.
type importSizeError struct {
	limit int64
	refs  bool
}

func (e *importSizeError) Error() string { return ErrImportTooLarge.Error() }
func (e *importSizeError) Unwrap() error { return ErrImportTooLarge }

func (e *importSizeError) message() string {
	if e.refs {
		return "The source advertised more refs than this instance accepts (" + formatImportBytes(e.limit) + ")."
	}
	return "The repository is larger than this instance's limit of " + formatImportBytes(e.limit) + "."
}

// importGuard travels in the request context, so the process-wide go-git
// client applies it to one import alone. It records why it stopped a request
// because go-git wraps transport errors in ways errors.As can't always see through.
// A non-positive cap is off.
type importGuard struct {
	allowLocal    bool
	maxPackBytes  int64
	maxRefsBytes  int64
	maxErrorBytes int64

	mu          sync.Mutex
	blockedHost string
	tooLarge    *importSizeError
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
		return &PrivateNetworkError{Host: g.blockedHost}
	case g.tooLarge != nil:
		return g.tooLarge
	}
	return nil
}

func (g *importGuard) dial(ctx context.Context, d *net.Dialer, network, addr string) (net.Conn, error) {
	conn, err := dialPublic(ctx, d, network, addr)
	var blocked *PrivateNetworkError
	if errors.As(err, &blocked) {
		g.mu.Lock()
		g.blockedHost = blocked.Host
		g.mu.Unlock()
	}
	return conn, err
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
	g := importGuardFrom(req.Context())
	if g == nil {
		return resp, nil
	}
	switch {
	case resp.StatusCode < 200 || resp.StatusCode > 299:
		if g.maxErrorBytes > 0 {
			resp.Body = truncatedBody{Reader: io.LimitReader(resp.Body, g.maxErrorBytes), Closer: resp.Body}
		}
	case req.Method == http.MethodGet && strings.HasSuffix(req.URL.Path, "/info/refs"):
		resp.Body = g.capBody(resp.Body, g.maxRefsBytes, true)
	default:
		resp.Body = g.capBody(resp.Body, g.maxPackBytes, false)
	}
	return resp, nil
}

type truncatedBody struct {
	io.Reader
	io.Closer
}

func (g *importGuard) capBody(body io.ReadCloser, limit int64, refs bool) io.ReadCloser {
	if limit <= 0 {
		return body
	}
	return &importBody{
		LimitedReadCloser: gittransport.NewLimitedReadCloser(body, limit),
		guard:             g,
		crossed:           &importSizeError{limit: limit, refs: refs},
	}
}

type importBody struct {
	*gittransport.LimitedReadCloser
	guard   *importGuard
	crossed *importSizeError
}

func (b *importBody) Read(p []byte) (int, error) {
	n, err := b.LimitedReadCloser.Read(p)
	if errors.Is(err, gittransport.ErrPackTooLarge) {
		b.guard.mu.Lock()
		b.guard.tooLarge = b.crossed
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
