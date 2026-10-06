package middleware

import (
	"net/http"
	"strconv"
	"strings"
	"time"
)

// Rate-limited resources. Each has its own budget per subject.
const (
	ResourceCore    = "core"
	ResourceGit     = "git"
	ResourceArchive = "archive"
	ResourceSearch  = "search"
)

// RateBudget is a resource's budget per window for signed-in and anonymous
// subjects. Zero is unlimited.
type RateBudget struct {
	Authenticated int
	Anonymous     int
}

// APIRateLimitConfig configures APIRateLimit.
type APIRateLimitConfig struct {
	Window  time.Duration
	Budgets map[string]RateBudget // by resource; a missing resource is unlimited

	JWTSecret  string
	CookieName string
	PAT        PATValidator
	OAuth      OAuthTokenResolver

	// OnLimited writes the 429 and its body; the rate-limit headers are already set.
	OnLimited http.HandlerFunc
}

// exemptPaths, with everything under /static/, are never counted. Health and
// metrics endpoints belong here too, since probes poll them.
var exemptPaths = map[string]bool{
	"/htmx.min.js":   true,
	"/alpine.min.js": true,
	"/favicon.ico":   true,
}

// APIRateLimit charges every request that isn't exempt to one resource's
// budget for its subject: the signed-in user, split into session and token
// buckets, or else the client IPv4 address or IPv6 /64. It runs before routing,
// so it resolves credentials itself; a token it looks up is kept in the request
// context for Auth, OptionalAuth and ValidatePAT to reuse.
func APIRateLimit(cfg APIRateLimitConfig) func(http.Handler) http.Handler {
	return newAPIRateLimiter(cfg, time.Now).middleware
}

type apiRateLimiter struct {
	cfg     APIRateLimitConfig
	counter *fixedWindow
}

func newAPIRateLimiter(cfg APIRateLimitConfig, now func() time.Time) *apiRateLimiter {
	if cfg.OnLimited == nil {
		cfg.OnLimited = func(w http.ResponseWriter, _ *http.Request) {
			http.Error(w, "rate limit exceeded", http.StatusTooManyRequests)
		}
	}
	return &apiRateLimiter{cfg: cfg, counter: newFixedWindow(cfg.Window, now)}
}

func (l *apiRateLimiter) middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resource, counted := classifyRequest(r)
		if !counted {
			next.ServeHTTP(w, r)
			return
		}
		r, subject, signedIn := l.subject(r, resource)
		budget := l.cfg.Budgets[resource]
		limit := budget.Anonymous
		if signedIn {
			limit = budget.Authenticated
		}
		if limit == 0 {
			next.ServeHTTP(w, r)
			return
		}
		remaining, reset, ok := l.counter.take(resource+":"+subject, limit)
		h := w.Header()
		h.Set("X-RateLimit-Limit", strconv.Itoa(limit))
		h.Set("X-RateLimit-Remaining", strconv.Itoa(remaining))
		h.Set("X-RateLimit-Reset", strconv.FormatInt(reset.Unix(), 10))
		h.Set("X-RateLimit-Resource", resource)
		if !ok {
			setRetryAfter(w, reset.Sub(l.counter.now()))
			l.cfg.OnLimited(w, r)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// subject returns the bucket r counts against, and r carrying any token it
// looked up. A credential that doesn't verify counts against the IP, so random
// tokens can't buy fresh buckets. The bucket never grants access, which is why
// a session JWT's signature is enough here: auth still checks its version.
func (l *apiRateLimiter) subject(r *http.Request, resource string) (*http.Request, string, bool) {
	ipKey := "ip:" + rateLimitKey(r)
	raw := extractToken(r, l.cfg.CookieName)
	if raw == "" && resource == ResourceGit {
		// git sends a PAT as the Basic-auth password; see resolveGitUser.
		if _, password, ok := r.BasicAuth(); ok && strings.HasPrefix(password, "czp_") {
			raw = password
		}
	}
	if raw == "" {
		return r, ipKey, false
	}
	if strings.HasPrefix(raw, "czp_") {
		if l.cfg.PAT == nil {
			return r, ipKey, false
		}
		token, user, err := l.cfg.PAT.Validate(r.Context(), raw)
		r = withCredential(r, resolvedCredential{raw: raw, kind: credentialPAT, token: token, user: user, err: err})
		if err != nil {
			return r, ipKey, false
		}
		return r, tokenBucket(user.ID), true
	}
	if claims, ok := parseSessionJWT(l.cfg.JWTSecret, raw); ok {
		return withCredential(r, resolvedCredential{raw: raw, kind: credentialSession}), "user:" + strconv.FormatInt(claims.UserID, 10) + ":web", true
	}
	if l.cfg.OAuth == nil {
		return r, ipKey, false
	}
	user, scopes, err := l.cfg.OAuth.ResolveOAuthToken(r.Context(), raw)
	r = withCredential(r, resolvedCredential{raw: raw, kind: credentialOAuth, user: user, scopes: scopes, err: err})
	if err != nil {
		return r, ipKey, false
	}
	return r, tokenBucket(user.ID), true
}

// All of a user's tokens share one bucket, so minting more doesn't raise the budget.
func tokenBucket(userID int64) string {
	return "user:" + strconv.FormatInt(userID, 10) + ":token"
}

// classifyRequest names the resource r is charged to, or reports that r isn't
// counted. It runs before routing, so it matches the shapes of the routes'
// paths, split as chi splits them.
func classifyRequest(r *http.Request) (string, bool) {
	path := r.URL.EscapedPath()
	if exemptPaths[path] || strings.HasPrefix(path, "/static/") {
		return "", false
	}
	if path == "/search" || path == "/search/code" {
		return ResourceSearch, true
	}
	seg := strings.Split(strings.TrimPrefix(path, "/"), "/")
	if seg[0] == "api" {
		return ResourceCore, true
	}
	switch {
	case len(seg) == 4 && seg[2] == "info" && seg[3] == "refs",
		len(seg) == 3 && (seg[2] == "git-upload-pack" || seg[2] == "git-receive-pack"):
		return ResourceGit, true
	case len(seg) >= 4 && seg[2] == "archive":
		return ResourceArchive, true
	}
	return ResourceCore, true
}
