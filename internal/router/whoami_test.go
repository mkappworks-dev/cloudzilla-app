package router_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/testutil"
)

func whoami(h http.Handler, authorization string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, "/api/user", nil)
	if authorization != "" {
		req.Header.Set("Authorization", authorization)
	}
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	return rr
}

func TestWhoami_ReturnsOnlyIDAndUsername(t *testing.T) {
	h, svc, db := newTestRouter(t)
	suffix := testutil.UniqueSuffix(t)
	userID := testutil.SeedUser(t, db, suffix)

	for _, scope := range []string{model.ScopeRepoRead, model.ScopeRepoWrite, model.ScopeIssuesWrite, model.ScopePullsWrite} {
		t.Run(scope, func(t *testing.T) {
			rr := whoami(h, "Bearer "+mintPAT(t, svc, userID, scope))
			if rr.Code != http.StatusOK {
				t.Fatalf("want 200, got %d: %s", rr.Code, rr.Body.String())
			}
			var got map[string]any
			if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
				t.Fatalf("decode: %v", err)
			}
			if len(got) != 2 || got["id"] != float64(userID) || got["username"] != "testuser_"+suffix {
				t.Errorf("body = %v, want exactly id %d and username testuser_%s", got, userID, suffix)
			}
		})
	}
}

func TestWhoami_SessionJWT(t *testing.T) {
	h, _, db := newTestRouter(t)
	suffix := testutil.UniqueSuffix(t)
	userID := testutil.SeedUser(t, db, suffix)

	rr := whoami(h, "Bearer "+superadminJWT(t, userID, "testuser_"+suffix))
	if rr.Code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", rr.Code, rr.Body.String())
	}
}

func TestWhoami_BadCredentialsAre401(t *testing.T) {
	h, svc, db := newTestRouter(t)
	userID := testutil.SeedUser(t, db, testutil.UniqueSuffix(t))
	ctx := context.Background()

	past := time.Now().Add(-time.Hour)
	expired, _, err := svc.AccessToken.Generate(ctx, userID, "expired", []string{model.ScopeRepoRead}, &past)
	if err != nil {
		t.Fatalf("Generate expired: %v", err)
	}
	revokedRaw, revoked, err := svc.AccessToken.Generate(ctx, userID, "revoked", []string{model.ScopeRepoRead}, nil)
	if err != nil {
		t.Fatalf("Generate revoked: %v", err)
	}
	if err := svc.AccessToken.Delete(ctx, revoked.ID, userID); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	for name, authorization := range map[string]string{
		"missing":   "",
		"malformed": "Bearer not-a-token",
		"unknown":   "Bearer czp_0000000000000000000000000000000000000000",
		"expired":   "Bearer " + expired,
		"revoked":   "Bearer " + revokedRaw,
	} {
		t.Run(name, func(t *testing.T) {
			if rr := whoami(h, authorization); rr.Code != http.StatusUnauthorized {
				t.Fatalf("want 401, got %d: %s", rr.Code, rr.Body.String())
			}
		})
	}
}

func TestWhoami_SuspendedAccountRefused(t *testing.T) {
	h, svc, db := newTestRouter(t)
	userID := testutil.SeedUser(t, db, testutil.UniqueSuffix(t))
	tok := mintPAT(t, svc, userID, model.ScopeRepoRead)
	testutil.Exec(t, db, `UPDATE users SET suspended_at = NOW() WHERE id = $1`, userID)

	if rr := whoami(h, "Bearer "+tok); rr.Code != http.StatusForbidden {
		t.Fatalf("want 403, got %d: %s", rr.Code, rr.Body.String())
	}
}

func TestUserSubroutesStayClosedToTokens(t *testing.T) {
	h, svc, db := newTestRouter(t)
	userID := testutil.SeedUser(t, db, testutil.UniqueSuffix(t))
	tok := mintPAT(t, svc, userID, model.ScopeRepoRead, model.ScopeRepoWrite, model.ScopeIssuesWrite, model.ScopePullsWrite)

	for _, path := range []string{"/api/user/tokens", "/api/user/keys", "/api/user/replies", "/api/user/transfers"} {
		t.Run(path, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, path, nil)
			req.Header.Set("Authorization", "Bearer "+tok)
			rr := httptest.NewRecorder()
			h.ServeHTTP(rr, req)
			if rr.Code != http.StatusForbidden || rr.Header().Get("WWW-Authenticate") != `Bearer error="insufficient_scope"` {
				t.Fatalf("want 403 insufficient_scope, got %d %q", rr.Code, rr.Header().Get("WWW-Authenticate"))
			}
		})
	}
}
