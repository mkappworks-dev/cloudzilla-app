package middleware

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/mkappworks-dev/cloudzilla-app/internal/adminmode"
	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
)

const adminModeCookie = "cz_token_admin"

type stubAdminModes struct {
	live    bool
	actions []recordedAction
}

type recordedAction struct {
	userID             int64
	targetType, target string
	method, path       string
	status             int
}

func (s *stubAdminModes) Resolve(_ context.Context, _ *http.Request, raw string, _ int64, _ string, _ int) (time.Time, bool) {
	return time.Now().Add(time.Hour), s.live && raw == "good"
}

func (s *stubAdminModes) RecordAction(_ context.Context, r *http.Request, userID int64, _, targetType, target string, status int) {
	s.actions = append(s.actions, recordedAction{userID, targetType, target, r.Method, r.URL.Path, status})
}

func adminModeRequest(t *testing.T, method, path string, superadmin bool, viaHeader bool) *http.Request {
	t.Helper()
	req := httptest.NewRequest(method, path, nil)
	jwt := makeValidJWT(t, 7, "root", superadmin)
	if viaHeader {
		req.Header.Set("Authorization", "Bearer "+jwt)
	} else {
		req.AddCookie(&http.Cookie{Name: "cz_token", Value: jwt})
	}
	req.AddCookie(&http.Cookie{Name: adminModeCookie, Value: "good"})
	return req
}

func serveAdminMode(mw func(http.Handler) http.Handler, req *http.Request, h http.HandlerFunc) {
	mw(h).ServeHTTP(httptest.NewRecorder(), req)
}

func TestAdminMode_SetOnlyForSuperadminCookieSessions(t *testing.T) {
	cases := []struct {
		name       string
		superadmin bool
		viaHeader  bool
		live       bool
		want       bool
	}{
		{"superadmin cookie session", true, false, true, true},
		{"admin mode not live", true, false, false, false},
		{"not a superadmin", false, false, true, false},
		{"session JWT sent as a bearer header", true, true, true, false},
	}
	for _, tc := range cases {
		for mwName, build := range map[string]func(AuthOption) func(http.Handler) http.Handler{
			"Auth": func(o AuthOption) func(http.Handler) http.Handler {
				return Auth(testSecret, "cz_token", nil, nil, testUnauthorized, o)
			},
			"OptionalAuth": func(o AuthOption) func(http.Handler) http.Handler {
				return OptionalAuth(testSecret, "cz_token", nil, nil, o)
			},
		} {
			t.Run(tc.name+"/"+mwName, func(t *testing.T) {
				mw := build(WithAdminMode(adminModeCookie, &stubAdminModes{live: tc.live}))
				var st *adminmode.State
				serveAdminMode(mw, adminModeRequest(t, http.MethodGet, "/", tc.superadmin, tc.viaHeader), func(_ http.ResponseWriter, r *http.Request) {
					st = adminmode.FromContext(r.Context())
				})
				if (st != nil) != tc.want {
					t.Fatalf("admin mode set = %v, want %v", st != nil, tc.want)
				}
				if st != nil && st.UserID() != 7 {
					t.Errorf("bound to user %d, want 7", st.UserID())
				}
			})
		}
	}
}

func TestAdminMode_NeverForTokens(t *testing.T) {
	pat := &stubPAT{
		token: &model.AccessToken{ID: 1, Scopes: []string{model.ScopeRepoWrite}},
		user:  &model.User{ID: 7, Username: "root", IsSuperadmin: true},
	}
	oauth := &stubOAuth{user: &model.User{ID: 7, Username: "root", IsSuperadmin: true}, scopes: []string{model.ScopeRepoWrite}}
	for name, tok := range map[string]string{"pat": "czp_x", "oauth": "oauth_x"} {
		t.Run(name, func(t *testing.T) {
			mw := Auth(testSecret, "cz_token", pat, oauth, testUnauthorized, WithAdminMode(adminModeCookie, &stubAdminModes{live: true}))
			req := httptest.NewRequest(http.MethodGet, "/api/repos/bob/proj", nil)
			req.Header.Set("Authorization", "Bearer "+tok)
			req.AddCookie(&http.Cookie{Name: adminModeCookie, Value: "good"})
			var st *adminmode.State
			serveAdminMode(mw, req, func(_ http.ResponseWriter, r *http.Request) { st = adminmode.FromContext(r.Context()) })
			if st != nil {
				t.Error("a token request must never get admin mode")
			}
		})
	}
}

func TestAdminMode_NoOptionMeansNoAdminMode(t *testing.T) {
	var st *adminmode.State
	serveAdminMode(Auth(testSecret, "cz_token", nil, nil, testUnauthorized),
		adminModeRequest(t, http.MethodGet, "/", true, false),
		func(_ http.ResponseWriter, r *http.Request) { st = adminmode.FromContext(r.Context()) })
	if st != nil {
		t.Error("an auth middleware without the option, as git routes use, must not set admin mode")
	}
}

func TestAdminMode_AuditsMutatingRequestsThatUsedTheOverride(t *testing.T) {
	cases := []struct {
		name     string
		method   string
		override bool
		want     int
	}{
		{"POST that needed it", http.MethodPost, true, 1},
		{"DELETE that needed it", http.MethodDelete, true, 1},
		{"GET that needed it", http.MethodGet, true, 0},
		{"POST that did not need it", http.MethodPost, false, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := &stubAdminModes{live: true}
			mw := Auth(testSecret, "cz_token", nil, nil, testUnauthorized, WithAdminMode(adminModeCookie, rec))
			r := chi.NewRouter()
			r.With(mw).MethodFunc(tc.method, "/{owner}/{repo}/settings", func(w http.ResponseWriter, r *http.Request) {
				if tc.override {
					adminmode.Override(r.Context(), 7)
				}
				w.WriteHeader(http.StatusAccepted)
			})
			r.ServeHTTP(httptest.NewRecorder(), adminModeRequest(t, tc.method, "/bob/proj/settings", true, false))

			if len(rec.actions) != tc.want {
				t.Fatalf("audited %d actions, want %d", len(rec.actions), tc.want)
			}
			if tc.want == 1 {
				a := rec.actions[0]
				if a.userID != 7 || a.targetType != "repo" || a.target != "bob/proj" || a.method != tc.method || a.path != "/bob/proj/settings" || a.status != http.StatusAccepted {
					t.Errorf("unexpected audit %+v", a)
				}
			}
		})
	}
}
