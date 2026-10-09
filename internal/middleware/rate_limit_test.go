package middleware

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

type fakeClock struct{ t time.Time }

func (c *fakeClock) now() time.Time { return c.t }

func postFrom(h http.Handler, ip string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/register", nil)
	req.RemoteAddr = net.JoinHostPort(ip, "1234")
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	return rr
}

func TestRateLimit_BlocksAfterLimitPerIP(t *testing.T) {
	clock := &fakeClock{t: time.Unix(1_000_000, 0)}
	h := newRateLimiter(2, time.Minute, clock.now).middleware(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))

	for i := range 2 {
		if rr := postFrom(h, "203.0.113.9"); rr.Code != http.StatusOK {
			t.Fatalf("request %d: want 200, got %d", i+1, rr.Code)
		}
	}
	rr := postFrom(h, "203.0.113.9")
	if rr.Code != http.StatusTooManyRequests {
		t.Fatalf("want 429 over the limit, got %d", rr.Code)
	}
	if got := rr.Header().Get("Retry-After"); got != "60" {
		t.Errorf("want Retry-After 60, got %q", got)
	}
	if rr := postFrom(h, "198.51.100.1"); rr.Code != http.StatusOK {
		t.Errorf("another IP has its own budget; got %d", rr.Code)
	}
}

func TestRateLimit_IPv6SharesBudgetPerSlash64(t *testing.T) {
	clock := &fakeClock{t: time.Unix(1_000_000, 0)}
	h := newRateLimiter(1, time.Minute, clock.now).middleware(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))

	postFrom(h, "2001:db8:1:2::1")
	if rr := postFrom(h, "2001:db8:1:2:ffff:ffff:ffff:9"); rr.Code != http.StatusTooManyRequests {
		t.Errorf("addresses in one /64 share a budget; got %d", rr.Code)
	}
	if rr := postFrom(h, "2001:db8:1:3::1"); rr.Code != http.StatusOK {
		t.Errorf("another /64 has its own budget; got %d", rr.Code)
	}
}

func TestRateLimit_IPv4KeyedPerAddress(t *testing.T) {
	clock := &fakeClock{t: time.Unix(1_000_000, 0)}
	h := newRateLimiter(1, time.Minute, clock.now).middleware(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))

	postFrom(h, "203.0.113.9")
	if rr := postFrom(h, "203.0.113.9"); rr.Code != http.StatusTooManyRequests {
		t.Errorf("the same address shares its budget; got %d", rr.Code)
	}
	if rr := postFrom(h, "203.0.113.10"); rr.Code != http.StatusOK {
		t.Errorf("a neighbouring IPv4 address has its own budget; got %d", rr.Code)
	}
}

func TestRateLimit_WindowResets(t *testing.T) {
	clock := &fakeClock{t: time.Unix(1_000_000, 0)}
	l := newRateLimiter(1, time.Minute, clock.now)
	h := l.middleware(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))

	postFrom(h, "203.0.113.9")
	clock.t = clock.t.Add(30 * time.Second)
	if rr := postFrom(h, "203.0.113.9"); rr.Header().Get("Retry-After") != "30" {
		t.Errorf("want Retry-After 30 mid-window, got %q", rr.Header().Get("Retry-After"))
	}
	clock.t = clock.t.Add(30 * time.Second)
	if rr := postFrom(h, "203.0.113.9"); rr.Code != http.StatusOK {
		t.Errorf("want 200 once the window passes, got %d", rr.Code)
	}
}

func TestRateLimit_ForgetsExpiredWindows(t *testing.T) {
	clock := &fakeClock{t: time.Unix(1_000_000, 0)}
	l := newRateLimiter(1, time.Minute, clock.now)
	h := l.middleware(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))

	postFrom(h, "203.0.113.9")
	clock.t = clock.t.Add(time.Minute)
	postFrom(h, "198.51.100.1")

	if len(l.counter.windows) != 1 {
		t.Errorf("expired windows must be dropped; have %d", len(l.counter.windows))
	}
}

func TestFixedWindow_ReportsRemainingAndReset(t *testing.T) {
	clock := &fakeClock{t: time.Unix(1_000_000, 0)}
	c := newFixedWindow(time.Minute, clock.now)

	remaining, reset, ok := c.take("k", 2)
	if !ok || remaining != 1 || !reset.Equal(clock.t.Add(time.Minute)) {
		t.Fatalf("first take: want 1 left resetting in a minute, got %d %v %v", remaining, reset, ok)
	}
	clock.t = clock.t.Add(10 * time.Second)
	if remaining, _, ok = c.take("k", 2); !ok || remaining != 0 {
		t.Fatalf("second take: want 0 left, got %d %v", remaining, ok)
	}
	remaining, reset, ok = c.take("k", 2)
	if ok || remaining != 0 || !reset.Equal(time.Unix(1_000_060, 0)) {
		t.Errorf("third take: want a refusal with the window's reset, got %d %v %v", remaining, reset, ok)
	}
}

func TestRateLimit_RejectionFormatByPath(t *testing.T) {
	clock := &fakeClock{t: time.Unix(1_000_000, 0)}
	h := newRateLimiter(1, time.Minute, clock.now).middleware(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	over := func(path string) *httptest.ResponseRecorder {
		var rr *httptest.ResponseRecorder
		for range 2 {
			req := httptest.NewRequest(http.MethodPost, path, nil)
			req.RemoteAddr = net.JoinHostPort("203.0.113.50", "1234")
			rr = httptest.NewRecorder()
			h.ServeHTTP(rr, req)
		}
		return rr
	}

	api := over("/api/auth/device/code")
	if api.Code != http.StatusTooManyRequests || api.Header().Get("Content-Type") != "application/json" ||
		api.Header().Get("Retry-After") != "60" || strings.TrimSpace(api.Body.String()) != `{"error":"rate limit exceeded"}` {
		t.Errorf("API path = %d %q %q retry %q", api.Code, api.Header().Get("Content-Type"), api.Body, api.Header().Get("Retry-After"))
	}

	page := over("/register")
	if page.Code != http.StatusTooManyRequests || !strings.HasPrefix(page.Header().Get("Content-Type"), "text/plain") || page.Header().Get("Retry-After") != "60" {
		t.Errorf("page path = %d %q retry %q", page.Code, page.Header().Get("Content-Type"), page.Header().Get("Retry-After"))
	}
}

func TestUserLimiter_CountsPerUser(t *testing.T) {
	now := time.Now()
	l := newUserLimiter(2, time.Hour, func() time.Time { return now })
	limited := 0
	h := l.Middleware(func(w http.ResponseWriter, r *http.Request) { limited++; w.WriteHeader(http.StatusTooManyRequests) })(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) }))
	call := func(userID int64) int {
		req := httptest.NewRequest("POST", "/x", nil)
		req = req.WithContext(context.WithValue(req.Context(), claimsKey, Claims{UserID: userID}))
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code == http.StatusTooManyRequests && rec.Header().Get("Retry-After") == "" {
			t.Error("429 without Retry-After")
		}
		return rec.Code
	}
	for i, want := range []int{200, 200, 429} {
		if got := call(1); got != want {
			t.Fatalf("user 1 request %d = %d; want %d", i+1, got, want)
		}
	}
	if call(2) != 200 {
		t.Error("user 2 has a budget of its own")
	}
	now = now.Add(time.Hour + time.Second)
	if call(1) != 200 {
		t.Error("the window should reset after an hour")
	}
	if limited != 1 {
		t.Errorf("onLimited ran %d times; want 1", limited)
	}
}

func TestUserLimiter_LetsRequestsWithoutClaimsThrough(t *testing.T) {
	l := newUserLimiter(1, time.Hour, time.Now)
	h := l.Middleware(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusTooManyRequests) })(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) }))
	for i := range 3 {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest("POST", "/x", nil))
		if rec.Code != http.StatusOK {
			t.Fatalf("request %d without claims = %d; want 200, leaving the auth middleware to refuse it", i+1, rec.Code)
		}
	}
}
