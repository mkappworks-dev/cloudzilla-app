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
