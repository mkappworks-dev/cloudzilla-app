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
	limit  int
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

func newRateLimiter(limit int, window time.Duration, now func() time.Time) *rateLimiter {
	return &rateLimiter{limit: limit, window: window, now: now, windows: map[string]*rateWindow{}}
}

func (l *rateLimiter) middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if wait, ok := l.allow(rateLimitKey(r)); !ok {
			w.Header().Set("Retry-After", strconv.Itoa(int(math.Ceil(wait.Seconds()))))
			http.Error(w, "Too many attempts. Try again later.", http.StatusTooManyRequests)
			return
		}
		next.ServeHTTP(w, r)
	})
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

// allow reports whether key may proceed, or how long until its window resets.
func (l *rateLimiter) allow(key string) (time.Duration, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	if now.Sub(l.swept) >= l.window {
		for k, w := range l.windows {
			if now.Sub(w.start) >= l.window {
				delete(l.windows, k)
			}
		}
		l.swept = now
	}
	w, ok := l.windows[key]
	if !ok || now.Sub(w.start) >= l.window {
		w = &rateWindow{start: now}
		l.windows[key] = w
	}
	if w.count >= l.limit {
		return w.start.Add(l.window).Sub(now), false
	}
	w.count++
	return 0, true
}
