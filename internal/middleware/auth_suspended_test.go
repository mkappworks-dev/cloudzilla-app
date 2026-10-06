package middleware

import (
	"context"
	"database/sql"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
)

type stubSessions struct {
	state model.SessionState
	err   error
}

func (s stubSessions) SessionState(context.Context, int64) (model.SessionState, error) {
	return s.state, s.err
}

func TestAuth_SuspendedUsersTokens_Forbidden(t *testing.T) {
	cases := []struct {
		name  string
		token string
		pat   PATValidator
		oauth OAuthTokenResolver
	}{
		{"pat", "czp_suspended", &stubPAT{err: model.ErrAccountSuspended}, nil},
		{"oauth", "oauth_suspended", nil, &stubOAuth{err: model.ErrAccountSuspended}},
	}
	for _, tc := range cases {
		mws := map[string]func(http.Handler) http.Handler{
			"Auth":         Auth(testSecret, "cz_token", tc.pat, tc.oauth, testUnauthorized),
			"OptionalAuth": OptionalAuth(testSecret, "cz_token", tc.pat, tc.oauth),
		}
		for mwName, mw := range mws {
			t.Run(tc.name+"/"+mwName, func(t *testing.T) {
				req := httptest.NewRequest(http.MethodGet, "/api/repos/bob/proj", nil)
				req.Header.Set("Authorization", "Bearer "+tc.token)
				rr := runAuth(mw, req)
				if rr.Code != http.StatusForbidden || !strings.Contains(rr.Body.String(), "account_suspended") {
					t.Errorf("want 403 account_suspended, got %d %q", rr.Code, rr.Body.String())
				}
			})
		}
	}
}

func TestAuth_SessionRoleReadFresh(t *testing.T) {
	for _, tc := range []struct {
		name          string
		jwtSuperadmin bool
		dbSuperadmin  bool
	}{
		{"demoted", true, false},
		{"promoted", false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			opt := WithSessionStates(stubSessions{state: model.SessionState{IsSuperadmin: tc.dbSuperadmin}})
			for mwName, mw := range map[string]func(http.Handler) http.Handler{
				"Auth":         Auth(testSecret, "cz_token", nil, nil, testUnauthorized, opt),
				"OptionalAuth": OptionalAuth(testSecret, "cz_token", nil, nil, opt),
			} {
				var got Claims
				handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					got, _ = ClaimsFromContext(r.Context())
				})
				req := httptest.NewRequest(http.MethodGet, "/", nil)
				req.Header.Set("Authorization", "Bearer "+makeValidJWT(t, 7, "alice", tc.jwtSuperadmin))
				mw(handler).ServeHTTP(httptest.NewRecorder(), req)
				if got.UserID != 7 || got.IsSuperadmin != tc.dbSuperadmin {
					t.Errorf("%s: claims %+v, want IsSuperadmin %v", mwName, got, tc.dbSuperadmin)
				}
			}
		})
	}
}

func TestAuth_SuspendedSession_Unauthorized(t *testing.T) {
	opt := WithSessionStates(stubSessions{err: sql.ErrNoRows})
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer "+makeValidJWT(t, 7, "alice", false))
	if rr := runAuth(Auth(testSecret, "cz_token", nil, nil, testUnauthorized, opt), req); rr.Code != http.StatusUnauthorized {
		t.Errorf("want 401, got %d", rr.Code)
	}
}
