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

func TestBlockedIP(t *testing.T) {
	for _, tc := range []struct {
		ip      string
		blocked bool
	}{
		{"127.0.0.1", true}, {"10.1.2.3", true}, {"172.16.0.1", true}, {"192.168.1.1", true},
		{"169.254.169.254", true}, {"100.100.100.200", true}, {"0.1.2.3", true}, {"0.0.0.0", true},
		{"224.0.0.1", true}, {"::1", true}, {"fc00::1", true}, {"fe80::1", true}, {"::ffff:127.0.0.1", true},
		{"192.0.0.8", true}, {"198.18.0.1", true}, {"240.0.0.1", true}, {"255.255.255.255", true},
		{"64:ff9b::a00:1", true}, {"64:ff9b:1::1", true}, {"2002:a00:1::1", true}, {"fec0::1", true},
		{"8.8.8.8", false}, {"140.82.112.3", false}, {"2606:4700:4700::1111", false},
	} {
		if got := blockedIP(net.ParseIP(tc.ip)); got != tc.blocked {
			t.Errorf("blockedIP(%s) = %v, want %v", tc.ip, got, tc.blocked)
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
		_ = resp.Body.Close()
		t.Fatal("request to 127.0.0.1 succeeded under a guard")
	}
	var blocked *PrivateNetworkError
	if !errors.As(g.failure(), &blocked) || blocked.Host != "127.0.0.1" {
		t.Errorf("failure() = %v, want PrivateNetworkError for 127.0.0.1", g.failure())
	}
}

func TestImportClient_RefusesAHostnameThatResolvesToLoopback(t *testing.T) {
	srv := importTestServer(t, "ok")
	_, port, err := net.SplitHostPort(srv.Listener.Addr().String())
	if err != nil {
		t.Fatalf("split listener address: %v", err)
	}
	g := &importGuard{}
	if resp, err := importGet(withImportGuard(context.Background(), g), "http://localhost:"+port); err == nil {
		_ = resp.Body.Close()
		t.Fatal("request to localhost succeeded under a guard")
	}
	var blocked *PrivateNetworkError
	if !errors.As(g.failure(), &blocked) || *blocked != (PrivateNetworkError{Host: "localhost"}) {
		t.Errorf("failure() = %v, want PrivateNetworkError{Host: localhost}", g.failure())
	}
}

func TestImportClient_LeavesUnguardedRequestsAlone(t *testing.T) {
	srv := importTestServer(t, "ok")
	resp, err := importGet(context.Background(), srv.URL)
	if err != nil {
		t.Fatalf("unguarded request: %v", err)
	}
	_ = resp.Body.Close()
}

func TestImportClient_AllowLocalNetworks(t *testing.T) {
	srv := importTestServer(t, "ok")
	resp, err := importGet(withImportGuard(context.Background(), &importGuard{allowLocal: true}), srv.URL)
	if err != nil {
		t.Fatalf("request with allow_local_networks: %v", err)
	}
	_ = resp.Body.Close()
}

func TestImportClient_CapsResponseBytes(t *testing.T) {
	srv := importTestServer(t, strings.Repeat("x", 1000))
	g := &importGuard{allowLocal: true, maxPackBytes: 100}
	resp, err := importGet(withImportGuard(context.Background(), g), srv.URL)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if _, err := io.ReadAll(resp.Body); !errors.Is(err, gittransport.ErrPackTooLarge) {
		t.Errorf("read error = %v, want ErrPackTooLarge", err)
	}
	if !errors.Is(g.failure(), ErrImportTooLarge) {
		t.Errorf("failure() = %v, want ErrImportTooLarge", g.failure())
	}
	var size *importSizeError
	if !errors.As(g.failure(), &size) || size.refs || size.limit != 100 {
		t.Errorf("failure() = %+v, want the 100-byte pack cap", size)
	}
}

func TestImportClient_CapsTheRefAdvertisementSeparately(t *testing.T) {
	srv := importTestServer(t, strings.Repeat("x", 1000))
	g := &importGuard{allowLocal: true, maxRefsBytes: 100}
	resp, err := importGet(withImportGuard(context.Background(), g), srv.URL+"/source.git/info/refs?service=git-upload-pack")
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if _, err := io.ReadAll(resp.Body); !errors.Is(err, gittransport.ErrPackTooLarge) {
		t.Errorf("read error = %v, want ErrPackTooLarge", err)
	}
	var size *importSizeError
	if !errors.Is(g.failure(), ErrImportTooLarge) || !errors.As(g.failure(), &size) || !size.refs || size.limit != 100 {
		t.Errorf("failure() = %+v, want the 100-byte refs cap", g.failure())
	}
}

func TestImportClient_RefsCapDoesNotLimitThePack(t *testing.T) {
	srv := importTestServer(t, strings.Repeat("x", 1000))
	g := &importGuard{allowLocal: true, maxRefsBytes: 100}
	resp, err := importGet(withImportGuard(context.Background(), g), srv.URL+"/source.git/git-upload-pack")
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if body, err := io.ReadAll(resp.Body); err != nil || len(body) != 1000 {
		t.Errorf("read %d bytes, %v; want 1000, nil", len(body), err)
	}
	if g.failure() != nil {
		t.Errorf("failure() = %v, want nil", g.failure())
	}
}

func TestImportClient_TruncatesErrorBodies(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = io.WriteString(w, strings.Repeat("x", 1000))
	}))
	t.Cleanup(srv.Close)
	// Caps below the error cap prove a non-2xx body answers to its own cap alone.
	g := &importGuard{allowLocal: true, maxPackBytes: 10, maxRefsBytes: 10, maxErrorBytes: 100}
	resp, err := importGet(withImportGuard(context.Background(), g), srv.URL+"/source.git/info/refs?service=git-upload-pack")
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	if err != nil || len(body) != 100 {
		t.Errorf("read %d bytes, %v; want 100, nil", len(body), err)
	}
	if g.failure() != nil {
		t.Errorf("failure() = %v, want nil: a long error page is not a size failure", g.failure())
	}
}

func TestImportSizeError_Message(t *testing.T) {
	for _, tc := range []struct {
		err  importSizeError
		want string
	}{
		{importSizeError{limit: 2 << 30}, "The repository is larger than this instance's limit of 2 GiB."},
		{importSizeError{limit: importMaxRefsBytes, refs: true}, "The source advertised more refs than this instance accepts (64 MiB)."},
	} {
		if got := tc.err.message(); got != tc.want {
			t.Errorf("message() = %q, want %q", got, tc.want)
		}
	}
}
