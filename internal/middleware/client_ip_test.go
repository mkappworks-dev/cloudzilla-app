package middleware_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/mkappworks-dev/cloudzilla-app/internal/middleware"
)

func TestParseTrustedProxies_AcceptsCIDRsAndBareIPs(t *testing.T) {
	prefixes, err := middleware.ParseTrustedProxies([]string{" 10.0.0.0/8 ", "192.168.1.7", "::1", ""})
	if err != nil {
		t.Fatalf("ParseTrustedProxies: %v", err)
	}
	if len(prefixes) != 3 {
		t.Fatalf("want 3 prefixes, got %v", prefixes)
	}
	if got := prefixes[1].String(); got != "192.168.1.7/32" {
		t.Errorf("bare IPv4 must become a /32, got %s", got)
	}
}

func TestParseTrustedProxies_RejectsGarbage(t *testing.T) {
	if _, err := middleware.ParseTrustedProxies([]string{"not-an-ip"}); err == nil {
		t.Error("want an error for an entry that is neither an IP nor a CIDR")
	}
}

func TestClientIP(t *testing.T) {
	trusted, err := middleware.ParseTrustedProxies([]string{"10.0.0.0/8"})
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name, remoteAddr, xff, want string
	}{
		{"direct client", "203.0.113.9:4321", "", "203.0.113.9"},
		{"untrusted peer's header is ignored", "203.0.113.9:4321", "198.51.100.1", "203.0.113.9"},
		{"trusted proxy forwards the client", "10.0.0.2:80", "198.51.100.1", "198.51.100.1"},
		{"spoofed leftmost hop is skipped", "10.0.0.2:80", "6.6.6.6, 198.51.100.1", "198.51.100.1"},
		{"chained trusted proxies", "10.0.0.2:80", "198.51.100.1, 10.0.0.3", "198.51.100.1"},
		{"garbage hop stops the walk", "10.0.0.2:80", "198.51.100.1, junk", "10.0.0.2"},
		{"IPv6 client", "[2001:db8::1]:443", "", "2001:db8::1"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var got string
			h := middleware.ClientIP(trusted)(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
				got = middleware.RemoteIP(r)
			}))
			req := httptest.NewRequest(http.MethodGet, "/", nil)
			req.RemoteAddr = tc.remoteAddr
			if tc.xff != "" {
				req.Header.Set("X-Forwarded-For", tc.xff)
			}
			h.ServeHTTP(httptest.NewRecorder(), req)
			if got != tc.want {
				t.Errorf("want %s, got %s", tc.want, got)
			}
		})
	}
}
