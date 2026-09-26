package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

type fakeClock struct{ t time.Time }

func (c *fakeClock) now() time.Time { return c.t }

func postFrom(h http.Handler, ip string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/register", nil)
	req.RemoteAddr = ip + ":1234"
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

	if len(l.windows) != 1 {
		t.Errorf("expired windows must be dropped; have %d", len(l.windows))
	}
}
