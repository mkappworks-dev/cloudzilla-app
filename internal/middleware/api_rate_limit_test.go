package middleware

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
)

// tokenTable validates PATs and OAuth tokens from a map of raw token to user ID,
// counting lookups.
type tokenTable struct {
	users   map[string]int64
	lookups int
}

func (t *tokenTable) find(raw string) (*model.User, error) {
	t.lookups++
	id, ok := t.users[raw]
	if !ok {
		return nil, errors.New("no such token")
	}
	return &model.User{ID: id, Username: "u" + strconv.FormatInt(id, 10)}, nil
}

func (t *tokenTable) Validate(_ context.Context, raw string) (*model.AccessToken, *model.User, error) {
	u, err := t.find(raw)
	if err != nil {
		return nil, nil, err
	}
	return &model.AccessToken{ID: u.ID * 100, Name: "ci", Scopes: []string{"repo"}}, u, nil
}

func (t *tokenTable) UpdateLastUsed(context.Context, int64) error { return nil }

func (t *tokenTable) ResolveOAuthToken(_ context.Context, raw string) (*model.User, []string, error) {
	u, err := t.find(raw)
	if err != nil {
		return nil, nil, err
	}
	return u, []string{"repo"}, nil
}

func newTestAPILimiter(budgets map[string]RateBudget, tokens *tokenTable, clock *fakeClock) *apiRateLimiter {
	return newAPIRateLimiter(APIRateLimitConfig{
		Window:     time.Hour,
		Budgets:    budgets,
		JWTSecret:  testSecret,
		CookieName: "cz_token",
		PAT:        tokens,
		OAuth:      tokens,
	}, clock.now)
}

func request(method, target, ip string) *http.Request {
	req := httptest.NewRequest(method, target, nil)
	req.RemoteAddr = net.JoinHostPort(ip, "1234")
	return req
}

func serve(h http.Handler, req *http.Request) *httptest.ResponseRecorder {
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	return rr
}

func TestClassifyRequest(t *testing.T) {
	for _, tc := range []struct {
		method, path string
		resource     string
		counted      bool
	}{
		{"GET", "/acme/app/info/refs", ResourceGit, true},
		{"GET", "/acme/app.git/info/refs", ResourceGit, true},
		{"POST", "/acme/app/git-upload-pack", ResourceGit, true},
		{"POST", "/acme/app.git/git-receive-pack", ResourceGit, true},
		{"GET", "/acme/app/archive/main", ResourceArchive, true},
		{"GET", "/acme/app/archive/release/v1.0.zip", ResourceArchive, true},
		{"POST", "/api/repos/acme/app/archive", ResourceCore, true},
		{"GET", "/a%2Fb/app/archive/main", ResourceArchive, true},
		{"GET", "/search", ResourceSearch, true},
		{"GET", "/search/code", ResourceSearch, true},
		{"GET", "/acme/app/blame/main/README.md", ResourceCore, true},
		{"GET", "/acme/app/tree/main/info/refs", ResourceCore, true},
		{"GET", "/acme/app/raw/main/x.go", ResourceCore, true},
		{"GET", "/api/repos/acme/app", ResourceCore, true},
		{"POST", "/login", ResourceCore, true},
		{"GET", "/", ResourceCore, true},
		{"GET", "/static/app.css", "", false},
		{"GET", "/htmx.min.js", "", false},
		{"GET", "/alpine.min.js", "", false},
		{"GET", "/favicon.ico", "", false},
	} {
		t.Run(tc.method+" "+tc.path, func(t *testing.T) {
			resource, counted := classifyRequest(httptest.NewRequest(tc.method, tc.path, nil))
			if resource != tc.resource || counted != tc.counted {
				t.Errorf("want %q %v, got %q %v", tc.resource, tc.counted, resource, counted)
			}
		})
	}
}

func TestAPIRateLimit_Subject(t *testing.T) {
	tokens := &tokenTable{users: map[string]int64{"czp_alice": 7, "oauth_alice": 7}}
	l := newTestAPILimiter(nil, tokens, &fakeClock{})
	session := makeValidJWT(t, 7, "alice", false)

	for _, tc := range []struct {
		name     string
		path     string
		setup    func(*http.Request)
		want     string
		signedIn bool
	}{
		{"anonymous", "/", func(*http.Request) {}, "ip:203.0.113.9", false},
		{"session cookie", "/", func(r *http.Request) { r.AddCookie(&http.Cookie{Name: "cz_token", Value: session}) }, "user:7:web", true},
		{"session bearer", "/api/user", func(r *http.Request) { r.Header.Set("Authorization", "Bearer "+session) }, "user:7:web", true},
		{"PAT bearer", "/api/user", func(r *http.Request) { r.Header.Set("Authorization", "Bearer czp_alice") }, "user:7:token", true},
		{"OAuth bearer", "/api/user", func(r *http.Request) { r.Header.Set("Authorization", "Bearer oauth_alice") }, "user:7:token", true},
		{"git Basic PAT", "/acme/app/info/refs", func(r *http.Request) { r.SetBasicAuth("alice", "czp_alice") }, "user:7:token", true},
		{"Basic PAT off git", "/api/user", func(r *http.Request) { r.SetBasicAuth("alice", "czp_alice") }, "ip:203.0.113.9", false},
		{"unknown PAT", "/api/user", func(r *http.Request) { r.Header.Set("Authorization", "Bearer czp_nope") }, "ip:203.0.113.9", false},
		{"unknown OAuth token", "/api/user", func(r *http.Request) { r.Header.Set("Authorization", "Bearer garbage") }, "ip:203.0.113.9", false},
		{"expired session", "/", func(r *http.Request) { r.AddCookie(&http.Cookie{Name: "cz_token", Value: makeExpiredJWT(t)}) }, "ip:203.0.113.9", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := request(http.MethodGet, tc.path, "203.0.113.9")
			tc.setup(req)
			resource, _ := classifyRequest(req)
			_, got, signedIn := l.subject(req, resource)
			if got != tc.want || signedIn != tc.signedIn {
				t.Errorf("want %s (signed in %v), got %s (%v)", tc.want, tc.signedIn, got, signedIn)
			}
		})
	}
}

func TestAPIRateLimit_HeadersAndRefusal(t *testing.T) {
	clock := &fakeClock{t: time.Unix(1_000_000, 0)}
	l := newTestAPILimiter(map[string]RateBudget{ResourceCore: {Anonymous: 2}}, &tokenTable{}, clock)
	l.cfg.OnLimited = func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte("limited"))
	}
	h := l.middleware(http.HandlerFunc(okHandler))

	rr := serve(h, request(http.MethodGet, "/explore", "203.0.113.9"))
	want := map[string]string{
		"X-RateLimit-Limit":     "2",
		"X-RateLimit-Remaining": "1",
		"X-RateLimit-Reset":     "1003600",
		"X-RateLimit-Resource":  "core",
	}
	for k, v := range want {
		if got := rr.Header().Get(k); got != v {
			t.Errorf("%s: want %q, got %q", k, v, got)
		}
	}
	if rr.Header().Get("Retry-After") != "" {
		t.Error("an allowed request carries no Retry-After")
	}

	serve(h, request(http.MethodGet, "/explore", "203.0.113.9"))
	clock.t = clock.t.Add(10 * time.Minute)
	rr = serve(h, request(http.MethodGet, "/explore", "203.0.113.9"))
	if rr.Code != http.StatusTooManyRequests || rr.Body.String() != "limited" {
		t.Fatalf("want onLimited's 429, got %d %q", rr.Code, rr.Body.String())
	}
	if got := rr.Header().Get("Retry-After"); got != "3000" {
		t.Errorf("want Retry-After 3000, got %q", got)
	}
	if got := rr.Header().Get("X-RateLimit-Remaining"); got != "0" {
		t.Errorf("want 0 remaining, got %q", got)
	}

	if rr := serve(h, request(http.MethodGet, "/explore", "2001:db8::1")); rr.Code != http.StatusOK {
		t.Errorf("another client has its own budget; got %d", rr.Code)
	}
}

func TestAPIRateLimit_ExemptPathsAreNeverCountedOrRefused(t *testing.T) {
	l := newTestAPILimiter(map[string]RateBudget{ResourceCore: {Anonymous: 1}}, &tokenTable{}, &fakeClock{})
	h := l.middleware(http.HandlerFunc(okHandler))

	for range 3 {
		rr := serve(h, request(http.MethodGet, "/static/app.css", "203.0.113.9"))
		if rr.Code != http.StatusOK || rr.Header().Get("X-RateLimit-Limit") != "" {
			t.Fatalf("exempt path: want 200 without headers, got %d %v", rr.Code, rr.Header())
		}
	}
	if rr := serve(h, request(http.MethodGet, "/", "203.0.113.9")); rr.Code != http.StatusOK {
		t.Errorf("exempt requests must not spend core; got %d", rr.Code)
	}
}

func TestAPIRateLimit_ResourcesHaveSeparateBudgets(t *testing.T) {
	budgets := map[string]RateBudget{
		ResourceCore:    {Anonymous: 1},
		ResourceGit:     {Anonymous: 1},
		ResourceArchive: {Anonymous: 1},
		ResourceSearch:  {Anonymous: 1},
	}
	h := newTestAPILimiter(budgets, &tokenTable{}, &fakeClock{}).middleware(http.HandlerFunc(okHandler))

	for _, tc := range []struct{ method, path string }{
		{"GET", "/acme/app/info/refs"},
		{"GET", "/acme/app/archive/main"},
		{"GET", "/search"},
		{"POST", "/api/repos/acme/app/archive"},
	} {
		if rr := serve(h, request(tc.method, tc.path, "203.0.113.9")); rr.Code != http.StatusOK {
			t.Errorf("%s %s: first request of its resource; got %d", tc.method, tc.path, rr.Code)
		}
	}
	if rr := serve(h, request("GET", "/explore", "203.0.113.9")); rr.Code != http.StatusTooManyRequests {
		t.Errorf("POST …/archive spent core, so core is exhausted; got %d", rr.Code)
	}
	if rr := serve(h, request("POST", "/acme/app/git-upload-pack", "203.0.113.9")); rr.Code != http.StatusTooManyRequests {
		t.Errorf("git is exhausted; got %d", rr.Code)
	}
}

func TestAPIRateLimit_SessionAndTokenBucketsAreSeparate(t *testing.T) {
	tokens := &tokenTable{users: map[string]int64{"czp_one": 7, "czp_two": 7}}
	h := newTestAPILimiter(map[string]RateBudget{ResourceCore: {Authenticated: 1, Anonymous: 1}}, tokens, &fakeClock{}).
		middleware(http.HandlerFunc(okHandler))
	withBearer := func(tok string) *http.Request {
		req := request(http.MethodGet, "/api/user", "203.0.113.9")
		req.Header.Set("Authorization", "Bearer "+tok)
		return req
	}

	if rr := serve(h, withBearer("czp_one")); rr.Code != http.StatusOK {
		t.Fatalf("first PAT request: got %d", rr.Code)
	}
	if rr := serve(h, withBearer("czp_two")); rr.Code != http.StatusTooManyRequests {
		t.Errorf("a user's PATs share a bucket; got %d", rr.Code)
	}
	if rr := serve(h, withBearer(makeValidJWT(t, 7, "alice", false))); rr.Code != http.StatusOK {
		t.Errorf("the session bucket is separate from the token bucket; got %d", rr.Code)
	}
	if rr := serve(h, request(http.MethodGet, "/api/user", "203.0.113.9")); rr.Code != http.StatusOK {
		t.Errorf("the IP bucket is separate from the user's; got %d", rr.Code)
	}
	if rr := serve(h, withBearer("czp_forged")); rr.Code != http.StatusTooManyRequests {
		t.Errorf("an invalid token counts against the spent IP bucket; got %d", rr.Code)
	}
}

func TestAPIRateLimit_ZeroBudgetIsUnlimited(t *testing.T) {
	h := newTestAPILimiter(map[string]RateBudget{ResourceCore: {Anonymous: 0}}, &tokenTable{}, &fakeClock{}).
		middleware(http.HandlerFunc(okHandler))
	for range 5 {
		rr := serve(h, request(http.MethodGet, "/", "203.0.113.9"))
		if rr.Code != http.StatusOK || rr.Header().Get("X-RateLimit-Limit") != "" {
			t.Fatalf("unlimited: want 200 without headers, got %d %v", rr.Code, rr.Header())
		}
	}
}

func TestAPIRateLimit_AuthReusesTheTokenLookup(t *testing.T) {
	tokens := &tokenTable{users: map[string]int64{"czp_alice": 7, "oauth_alice": 7}}
	limiter := newTestAPILimiter(map[string]RateBudget{ResourceCore: {Authenticated: 10, Anonymous: 10}}, tokens, &fakeClock{})
	auth := Auth(testSecret, "cz_token", tokens, tokens, testUnauthorized)
	optAuth := OptionalAuth(testSecret, "cz_token", tokens, tokens)

	for _, tc := range []struct {
		name    string
		mw      func(http.Handler) http.Handler
		bearer  string
		lookups int
	}{
		{"Auth PAT", auth, "czp_alice", 1},
		{"Auth OAuth", auth, "oauth_alice", 1},
		{"Auth unknown token", auth, "garbage", 1},
		{"Auth session", auth, makeValidJWT(t, 7, "alice", false), 0},
		{"OptionalAuth PAT", optAuth, "czp_alice", 1},
		{"OptionalAuth session", optAuth, makeValidJWT(t, 7, "alice", false), 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tokens.lookups = 0
			req := request(http.MethodGet, "/api/user", "203.0.113.9")
			req.Header.Set("Authorization", "Bearer "+tc.bearer)
			serve(limiter.middleware(tc.mw(http.HandlerFunc(okHandler))), req)
			if tokens.lookups != tc.lookups {
				t.Errorf("want %d lookups, got %d", tc.lookups, tokens.lookups)
			}
		})
	}
}

func TestValidatePAT_ReusesTheLimitersResult(t *testing.T) {
	tokens := &tokenTable{users: map[string]int64{"czp_alice": 7}}
	limiter := newTestAPILimiter(map[string]RateBudget{ResourceGit: {Authenticated: 10, Anonymous: 10}}, tokens, &fakeClock{})
	var user *model.User
	h := limiter.middleware(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		_, password, _ := r.BasicAuth()
		_, user, _ = ValidatePAT(r, tokens, password)
	}))

	req := request(http.MethodGet, "/acme/app/info/refs", "203.0.113.9")
	req.SetBasicAuth("alice", "czp_alice")
	serve(h, req)
	if tokens.lookups != 1 || user == nil || user.ID != 7 {
		t.Errorf("want one lookup resolving user 7, got %d lookups and %+v", tokens.lookups, user)
	}
}
