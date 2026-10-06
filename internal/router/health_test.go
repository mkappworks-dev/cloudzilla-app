package router_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/mkappworks-dev/cloudzilla-app/internal/config"
	"github.com/mkappworks-dev/cloudzilla-app/internal/router"
	"github.com/mkappworks-dev/cloudzilla-app/internal/service"
	"github.com/mkappworks-dev/cloudzilla-app/internal/store"
	"github.com/mkappworks-dev/cloudzilla-app/internal/testutil"
)

type probeStub struct{ pingErr error }

func (p probeStub) Ping(context.Context) error                          { return p.pingErr }
func (p probeStub) PendingMigrations(context.Context) ([]string, error) { return nil, nil }

// With no database, any request that reaches the global middleware fails in
// RequireSetup, so a clean probe answer shows the app handler never ran.
func newProbeRouter(t *testing.T, probe service.HealthProbe) http.Handler {
	t.Helper()
	cfg := &config.Config{
		Server: config.ServerConfig{BaseURL: "http://localhost"},
		Auth:   config.AuthConfig{JWTSecret: testJWTSecret, JWTExpiry: time.Hour, CookieName: "cz_token"},
		Git:    config.GitConfig{ReposRoot: t.TempDir()},
	}
	svcs := service.New(store.New(nil), cfg)
	svcs.Health = service.NewHealthService(probe, cfg.Git.ReposRoot)
	h, err := router.New(svcs, cfg, fstest.MapFS{})
	if err != nil {
		t.Fatalf("router.New: %v", err)
	}
	return h
}

func request(h http.Handler, method, path string) *httptest.ResponseRecorder {
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest(method, path, nil))
	return rr
}

func captureLogs(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, nil)))
	t.Cleanup(func() { slog.SetDefault(prev) })
	return &buf
}

func TestHealthz_AnswersWithoutDatabase(t *testing.T) {
	h := newProbeRouter(t, probeStub{pingErr: errors.New("down")})
	logs := captureLogs(t)

	rr := request(h, http.MethodGet, "/healthz")

	if rr.Code != http.StatusOK || rr.Body.String() != "ok\n" {
		t.Fatalf("want 200 ok, got %d %q", rr.Code, rr.Body.String())
	}
	if ct := rr.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/plain") {
		t.Errorf("Content-Type: want text/plain, got %q", ct)
	}
	if cc := rr.Header().Get("Cache-Control"); cc != "no-store" {
		t.Errorf("Cache-Control: want no-store, got %q", cc)
	}
	if sc := rr.Header().Values("Set-Cookie"); len(sc) != 0 {
		t.Errorf("want no cookies, got %v", sc)
	}
	if strings.Contains(logs.String(), "msg=request") {
		t.Errorf("want no request log line, got %q", logs.String())
	}
}

func TestReadyz_ReportsChecks(t *testing.T) {
	for _, tc := range []struct {
		name   string
		probe  probeStub
		code   int
		status string
		checks map[string]string
	}{
		{"ready", probeStub{}, http.StatusOK, "ok",
			map[string]string{"database": "ok", "migrations": "ok", "storage": "ok"}},
		{"database down", probeStub{pingErr: errors.New("dial tcp 10.0.0.5:5432: refused")}, http.StatusServiceUnavailable, "fail",
			map[string]string{"database": "fail", "migrations": "skipped", "storage": "ok"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newProbeRouter(t, tc.probe)
			captureLogs(t)

			rr := request(h, http.MethodGet, "/readyz")

			if rr.Code != tc.code {
				t.Fatalf("want %d, got %d: %s", tc.code, rr.Code, rr.Body.String())
			}
			var body service.Readiness
			if err := json.Unmarshal(rr.Body.Bytes(), &body); err != nil {
				t.Fatalf("decode body %q: %v", rr.Body.String(), err)
			}
			if body.Status != tc.status {
				t.Errorf("status: want %q, got %q", tc.status, body.Status)
			}
			for k, v := range tc.checks {
				if body.Checks[k] != v {
					t.Errorf("%s: want %q, got %q", k, v, body.Checks[k])
				}
			}
			if strings.Contains(rr.Body.String(), "10.0.0.5") {
				t.Errorf("body leaks error detail: %s", rr.Body.String())
			}
			if sc := rr.Header().Values("Set-Cookie"); len(sc) != 0 {
				t.Errorf("want no cookies, got %v", sc)
			}
			if cc := rr.Header().Get("Cache-Control"); cc != "no-store" {
				t.Errorf("Cache-Control: want no-store, got %q", cc)
			}
		})
	}
}

func TestProbes_HeadAllowed_OtherMethodsRejected(t *testing.T) {
	h := newProbeRouter(t, probeStub{})
	for _, path := range []string{"/healthz", "/readyz"} {
		if rr := request(h, http.MethodHead, path); rr.Code != http.StatusOK {
			t.Errorf("HEAD %s: want 200, got %d", path, rr.Code)
		}
		for _, method := range []string{http.MethodPost, http.MethodPut, http.MethodDelete} {
			rr := request(h, method, path)
			if rr.Code != http.StatusMethodNotAllowed {
				t.Errorf("%s %s: want 405, got %d", method, path, rr.Code)
			}
			if allow := rr.Header().Get("Allow"); allow != "GET, HEAD" {
				t.Errorf("%s %s: Allow = %q", method, path, allow)
			}
		}
	}
}

func TestProbes_NotRateLimited(t *testing.T) {
	h := newProbeRouter(t, probeStub{})
	captureLogs(t)
	for i := range 200 {
		for _, path := range []string{"/healthz", "/readyz"} {
			req := httptest.NewRequest(http.MethodGet, path, nil)
			req.RemoteAddr = "192.0.2.9:1234"
			rr := httptest.NewRecorder()
			h.ServeHTTP(rr, req)
			if rr.Code != http.StatusOK {
				t.Fatalf("request %d to %s: got %d", i+1, path, rr.Code)
			}
		}
	}
}

func TestProbes_ReservedOwnerNames(t *testing.T) {
	for _, name := range []string{"healthz", "readyz"} {
		if service.ValidateOwnerName(name) == nil {
			t.Errorf("%q is served by a probe; reserve it", name)
		}
	}
}

func newSetupPendingRouter(t *testing.T) http.Handler {
	t.Helper()
	db := testutil.OpenFreshTestDB(t)
	cfg := &config.Config{
		Server: config.ServerConfig{BaseURL: "http://localhost"},
		Auth:   config.AuthConfig{JWTSecret: testJWTSecret, JWTExpiry: time.Hour, CookieName: "cz_token"},
		Git:    config.GitConfig{ReposRoot: t.TempDir()},
	}
	h, err := router.New(service.New(store.New(db), cfg), cfg, fstest.MapFS{})
	if err != nil {
		t.Fatalf("router.New: %v", err)
	}
	return h
}

// Before setup every other page redirects to /setup; the probes answer.
func TestProbes_BeforeSetup_AnswerDirectly(t *testing.T) {
	h := newSetupPendingRouter(t)
	captureLogs(t)

	if rr := request(h, http.MethodGet, "/"); rr.Code != http.StatusSeeOther {
		t.Fatalf("precondition: want / to redirect before setup, got %d", rr.Code)
	}
	for _, path := range []string{"/healthz", "/readyz"} {
		if rr := request(h, http.MethodGet, path); rr.Code != http.StatusOK {
			t.Errorf("%s: want 200, got %d: %s", path, rr.Code, rr.Body.String())
		}
	}
}

// The probes match exact paths; anything below them is an ordinary route.
func TestProbes_SubpathReachesRouter(t *testing.T) {
	h := newSetupPendingRouter(t)
	captureLogs(t)

	rr := request(h, http.MethodGet, "/healthz/anything")

	if rr.Code != http.StatusSeeOther || rr.Header().Get("Location") != "/setup" {
		t.Errorf("want the router's setup redirect, got %d to %q", rr.Code, rr.Header().Get("Location"))
	}
}
