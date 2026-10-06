package middleware

import (
	"math"
	"net/http"
	"net/netip"
	"strconv"
	"sync"
	"time"
)

// RateLimit allows each client IPv4 address or IPv6 /64 limit requests per
// fixed window on the routes it wraps. Counts live in this process, so each
// instance of a multi-instance deployment enforces its own budget.
func RateLimit(limit int, window time.Duration) func(http.Handler) http.Handler {
	return newRateLimiter(limit, window, time.Now).middleware
}

type rateLimiter struct {
	limit   int
	counter *fixedWindow
}

func newRateLimiter(limit int, window time.Duration, now func() time.Time) *rateLimiter {
	return &rateLimiter{limit: limit, counter: newFixedWindow(window, now)}
}

func (l *rateLimiter) middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, reset, ok := l.counter.take(rateLimitKey(r), l.limit); !ok {
			setRetryAfter(w, reset.Sub(l.counter.now()))
			http.Error(w, "Too many attempts. Try again later.", http.StatusTooManyRequests)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func setRetryAfter(w http.ResponseWriter, wait time.Duration) {
	w.Header().Set("Retry-After", strconv.Itoa(int(math.Ceil(wait.Seconds()))))
}

// An IPv6 /64 is one subscriber, who could otherwise rotate through its addresses.
func rateLimitKey(r *http.Request) string {
	ip := RemoteIP(r)
	addr, err := netip.ParseAddr(ip)
	if err != nil {
		return ip
	}
	if addr = addr.Unmap(); addr.Is4() {
		return addr.String()
	}
	return netip.PrefixFrom(addr, 64).Masked().String()
}

// fixedWindow counts requests per key in windows that start at a key's first request.
type fixedWindow struct {
	window time.Duration
	now    func() time.Time

	mu      sync.Mutex
	windows map[string]*rateWindow
	swept   time.Time
}

type rateWindow struct {
	start time.Time
	count int
}

func newFixedWindow(window time.Duration, now func() time.Time) *fixedWindow {
	return &fixedWindow{window: window, now: now, windows: map[string]*rateWindow{}}
}

// take charges one request to key, reporting how many of limit remain and
// when key's window resets. ok is false, and nothing is charged, once the
// window's limit is spent.
func (c *fixedWindow) take(key string, limit int) (remaining int, reset time.Time, ok bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	now := c.now()
	if now.Sub(c.swept) >= c.window {
		for k, w := range c.windows {
			if now.Sub(w.start) >= c.window {
				delete(c.windows, k)
			}
		}
		c.swept = now
	}
	w, found := c.windows[key]
	if !found || now.Sub(w.start) >= c.window {
		w = &rateWindow{start: now}
		c.windows[key] = w
	}
	reset = w.start.Add(c.window)
	if w.count >= limit {
		return 0, reset, false
	}
	w.count++
	return limit - w.count, reset, true
}
