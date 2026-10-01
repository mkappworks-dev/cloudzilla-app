package middleware

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
)

func TestScopeAllows(t *testing.T) {
	const (
		read   = model.ScopeRepoRead
		write  = model.ScopeRepoWrite
		issues = model.ScopeIssuesWrite
		pulls  = model.ScopePullsWrite
		admin  = model.ScopeRepoAdmin
	)
	tests := []struct {
		name   string
		method string
		target string
		scopes []string
		want   bool
	}{
		{"repo read with repo:read", "GET", "/api/repos/alice/proj", []string{read}, true},
		{"repo list with repo:read", "GET", "/api/repos", []string{read}, true},
		{"issue list with issues:write", "GET", "/api/repos/alice/proj/issues", []string{issues}, true},
		{"repo read with no scopes", "GET", "/api/repos/alice/proj", nil, false},
		{"create issue with repo:read", "POST", "/api/repos/alice/proj/issues", []string{read}, false},
		{"create issue with issues:write", "POST", "/api/repos/alice/proj/issues", []string{issues}, true},
		{"create issue with repo:write", "POST", "/api/repos/alice/proj/issues", []string{write}, true},
		{"create issue with pulls:write", "POST", "/api/repos/alice/proj/issues", []string{pulls}, false},
		{"review PR with pulls:write", "POST", "/api/repos/alice/proj/pulls/3/reviews", []string{pulls}, true},
		{"review PR with issues:write", "POST", "/api/repos/alice/proj/pulls/3/reviews", []string{issues}, false},
		{"apply suggestion with pulls:write", "POST", "/api/repos/alice/proj/pulls/3/line_comments/9/apply", []string{pulls}, false},
		{"apply suggestion with repo:write", "POST", "/api/repos/alice/proj/pulls/3/line_comments/9/apply", []string{write}, true},
		{"edit line comment with pulls:write", "PATCH", "/api/repos/alice/proj/pulls/3/line_comments/9", []string{pulls}, true},
		{"create release with issues:write", "POST", "/api/repos/alice/proj/releases", []string{issues}, false},
		{"create release with repo:write", "POST", "/api/repos/alice/proj/releases", []string{write}, true},
		{"create repo with repo:write", "POST", "/api/repos", []string{write}, true},
		{"update repo settings with repo:write", "PATCH", "/api/repos/alice/proj", []string{write}, false},
		{"add webhook with repo:write", "POST", "/api/repos/alice/proj/hooks", []string{write}, false},
		{"list webhooks with repo:write", "GET", "/api/repos/alice/proj/hooks", []string{write}, false},
		{"add collaborator with repo:write", "POST", "/api/repos/alice/proj/collaborators", []string{write}, false},
		{"add deploy key with repo:write", "POST", "/api/repos/alice/proj/keys", []string{write}, false},
		{"branch protection with repo:write", "POST", "/api/repos/alice/proj/branches/protections", []string{write}, false},
		{"create branch with repo:write", "POST", "/api/repos/alice/proj/branches", []string{write}, true},
		{"transfer with repo:write", "POST", "/api/repos/alice/proj/transfer", []string{write}, false},
		{"delete with repo:write", "POST", "/api/repos/alice/proj/delete", []string{write}, false},
		{"unarchive with repo:write", "POST", "/api/repos/alice/proj/unarchive", []string{write}, false},
		{"set topics with repo:write", "PUT", "/api/repos/alice/proj/topics", []string{write}, false},
		{"read topics with repo:read", "GET", "/api/repos/alice/proj/topics", []string{read}, false},
		{"unlisted repo sub-resource read", "GET", "/api/repos/alice/proj/mirror", []string{read, write}, false},
		{"unlisted repo sub-resource write", "POST", "/api/repos/alice/proj/mirror", []string{read, write}, false},
		{"create from template with repo:write", "POST", "/api/repos/from-template", []string{write}, true},
		{"read from-template path", "GET", "/api/repos/from-template", []string{read}, false},
		{"encoded slash cannot hide an admin path", "POST", "/api/repos/alice%2Fx/proj/hooks", []string{write}, false},
		{"user profile with repo:read", "GET", "/api/users/alice", []string{read}, true},
		{"user repos with repo:read", "GET", "/api/users/alice/repos", []string{read}, true},
		{"unlisted user sub-resource", "GET", "/api/users/alice/keys", []string{read, write}, false},
		{"org with repo:read", "GET", "/api/orgs/acme", []string{read}, true},
		{"org members with repo:read", "GET", "/api/orgs/acme/members", []string{read}, true},
		{"unlisted org sub-resource", "GET", "/api/orgs/acme/settings", []string{read, write}, false},
		{"create org repo with repo:write", "POST", "/api/orgs/acme/repos", []string{write}, true},
		{"add org member with repo:write", "POST", "/api/orgs/acme/members", []string{write}, false},
		{"create org with repo:write", "POST", "/api/orgs", []string{write}, false},
		{"clone with repo:read", "GET", "/alice/proj/info/refs?service=git-upload-pack", []string{read}, true},
		{"upload-pack with repo:read", "POST", "/alice/proj.git/git-upload-pack", []string{read}, true},
		{"push advert with repo:read", "GET", "/alice/proj/info/refs?service=git-receive-pack", []string{read}, false},
		{"push with repo:read", "POST", "/alice/proj/git-receive-pack", []string{read}, false},
		{"push with repo:write", "POST", "/alice/proj/git-receive-pack", []string{write}, true},
		{"profile page", "GET", "/alice", []string{read, write}, false},
		{"repo page", "GET", "/alice/proj", []string{read}, false},
		{"notification settings page", "GET", "/settings/notifications", []string{read, write}, false},
		{"create PAT", "POST", "/api/user/tokens", []string{read, write}, false},
		{"list SSH keys", "GET", "/api/user/keys", []string{read, write}, false},
		{"register OAuth app", "POST", "/api/oauth/apps", []string{read, write}, false},
		{"notifications API", "GET", "/api/notifications/unread-count", []string{read, write}, false},
		{"gists API", "POST", "/api/gists", []string{read, write}, false},
		{"admin API", "POST", "/api/admin/settings", []string{read, write}, false},
		{"consent screen", "POST", "/oauth/authorize", []string{read, write}, false},
		{"resend verification email", "POST", "/settings/email/resend-verification", []string{read, write, issues, pulls}, false},
		{"verify email page", "GET", "/verify-email?token=x", []string{read, write, issues, pulls}, false},
		{"verify email submit", "POST", "/verify-email", []string{read, write, issues, pulls}, false},
		{"admin verify email", "POST", "/api/admin/users/verify-email", []string{read, write, issues, pulls}, false},
		{"sign out other sessions", "POST", "/settings/sessions/revoke", []string{read, write, issues, pulls}, false},
		{"change password", "POST", "/settings/password", []string{read, write, issues, pulls}, false},
		{"email a confirmation code", "POST", "/settings/confirm-code", []string{read, write, issues, pulls}, false},
		{"sign in again to confirm", "POST", "/settings/reauth/google", []string{read, write, issues, pulls, admin}, false},
		{"add collaborator with repo:admin", "POST", "/api/repos/alice/proj/collaborators", []string{admin}, true},
		{"add webhook with repo:admin", "POST", "/api/repos/alice/proj/hooks", []string{admin}, true},
		{"delete repo with repo:admin", "POST", "/api/repos/alice/proj/delete", []string{admin}, true},
		{"add org owner with repo:admin", "POST", "/api/orgs/acme/members", []string{admin}, true},
		{"promote org member with repo:admin", "POST", "/api/orgs/acme/members/bob/role", []string{admin}, true},
		{"transfer org with repo:admin", "POST", "/api/orgs/acme/transfer", []string{admin}, true},
		{"push content with repo:admin only", "POST", "/api/repos/alice/proj/issues", []string{admin}, false},
		{"account SSH key with repo:admin", "POST", "/api/user/keys", []string{admin}, false},
		{"instance admin with repo:admin", "POST", "/api/admin/settings", []string{admin}, false},
		{"turn on 2FA", "POST", "/api/user/totp/enable", []string{read, write, issues, pulls}, false},
		{"turn off 2FA", "POST", "/api/user/totp/disable", []string{read, write, issues, pulls}, false},
		{"add collaborator", "POST", "/api/repos/alice/proj/collaborators", []string{read, write, issues, pulls}, false},
		{"add deploy key", "POST", "/api/repos/alice/proj/keys", []string{read, write, issues, pulls}, false},
		{"add webhook", "POST", "/api/repos/alice/proj/hooks", []string{read, write, issues, pulls}, false},
		{"transfer repo", "POST", "/api/repos/alice/proj/transfer", []string{read, write, issues, pulls}, false},
		{"transfer org", "POST", "/api/orgs/acme/transfer", []string{read, write, issues, pulls}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(tt.method, tt.target, nil)
			c := Claims{UserID: 1, Scoped: true, Scopes: tt.scopes}
			if got := ScopeAllows(c, req); got != tt.want {
				t.Errorf("ScopeAllows(%s %s, %v) = %v, want %v", tt.method, tt.target, tt.scopes, got, tt.want)
			}
		})
	}
}

func TestScopeAllows_UnscopedClaimsAllowEverything(t *testing.T) {
	for _, target := range []string{"/settings/notifications", "/api/repos/alice/proj/hooks", "/alice"} {
		req := httptest.NewRequest(http.MethodPost, target, nil)
		if !ScopeAllows(Claims{UserID: 1}, req) {
			t.Errorf("unscoped claims refused on %s", target)
		}
	}
}

func TestAuth_OAuthToken_ScopedClaims(t *testing.T) {
	oauth := &stubOAuth{user: &model.User{ID: 42, Username: "bob", IsSuperadmin: true}, scopes: []string{model.ScopeRepoRead}}
	req := httptest.NewRequest(http.MethodGet, "/api/repos/bob/proj", nil)
	req.Header.Set("Authorization", "Bearer oauthtoken")

	var got Claims
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got, _ = ClaimsFromContext(r.Context())
		w.WriteHeader(http.StatusOK)
	})
	rr := httptest.NewRecorder()
	Auth(testSecret, "cz_token", nil, oauth, testUnauthorized)(h).ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", rr.Code)
	}
	if got.UserID != 42 || got.Username != "bob" || !got.Scoped {
		t.Errorf("claims mismatch: %+v", got)
	}
	if got.IsSuperadmin {
		t.Error("an OAuth-app token must not carry superadmin")
	}
}

func TestAuth_OAuthToken_InsufficientScope(t *testing.T) {
	oauth := &stubOAuth{user: &model.User{ID: 42, Username: "bob"}, scopes: []string{model.ScopeRepoRead}}
	req := httptest.NewRequest(http.MethodPost, "/api/repos/bob/proj/issues", nil)
	req.Header.Set("Authorization", "Bearer oauthtoken")

	rr := runAuth(Auth(testSecret, "cz_token", nil, oauth, testUnauthorized), req)

	if rr.Code != http.StatusForbidden {
		t.Fatalf("want 403, got %d", rr.Code)
	}
	want := `Bearer error="insufficient_scope", scope="issues:write"`
	if got := rr.Header().Get("WWW-Authenticate"); got != want {
		t.Errorf("WWW-Authenticate = %q, want %q", got, want)
	}
}

func TestAuth_OAuthToken_ClosedRouteHasNoScopeHint(t *testing.T) {
	oauth := &stubOAuth{user: &model.User{ID: 42, Username: "bob"}, scopes: []string{model.ScopeRepoWrite}}
	req := httptest.NewRequest(http.MethodGet, "/settings/notifications", nil)
	req.Header.Set("Authorization", "Bearer oauthtoken")

	rr := runAuth(Auth(testSecret, "cz_token", nil, oauth, testUnauthorized), req)

	if rr.Code != http.StatusForbidden {
		t.Fatalf("want 403, got %d", rr.Code)
	}
	if got := rr.Header().Get("WWW-Authenticate"); got != `Bearer error="insufficient_scope"` {
		t.Errorf("WWW-Authenticate = %q", got)
	}
}

// A scoped token on an optional-auth page must be refused, not downgraded to
// anonymous: silently ignoring it would hide the misuse from the app developer.
func TestOptionalAuth_OAuthToken_ClosedRoute_Forbidden(t *testing.T) {
	oauth := &stubOAuth{user: &model.User{ID: 42, Username: "bob"}, scopes: []string{model.ScopeRepoRead}}
	req := httptest.NewRequest(http.MethodGet, "/bob", nil)
	req.Header.Set("Authorization", "Bearer oauthtoken")

	rr := runAuth(OptionalAuth(testSecret, "cz_token", nil, oauth), req)

	if rr.Code != http.StatusForbidden {
		t.Errorf("want 403, got %d", rr.Code)
	}
}

func TestOptionalAuth_OAuthToken_AllowedRoute_InjectsClaims(t *testing.T) {
	oauth := &stubOAuth{user: &model.User{ID: 42, Username: "bob"}, scopes: []string{model.ScopeRepoRead}}
	req := httptest.NewRequest(http.MethodGet, "/api/repos/bob/proj", nil)
	req.Header.Set("Authorization", "Bearer oauthtoken")

	var got Claims
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got, _ = ClaimsFromContext(r.Context())
		w.WriteHeader(http.StatusOK)
	})
	rr := httptest.NewRecorder()
	OptionalAuth(testSecret, "cz_token", nil, oauth)(h).ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", rr.Code)
	}
	if got.UserID != 42 || !got.HasScope(model.ScopeRepoRead) || got.HasScope(model.ScopeRepoWrite) {
		t.Errorf("claims mismatch: %+v", got)
	}
}

// A JWT is not an OAuth token: the resolver miss must fall through to JWT parsing
// and yield unscoped claims.
func TestAuth_JWTWithOAuthResolver_Unscoped(t *testing.T) {
	oauth := &stubOAuth{err: errors.New("not found")}
	req := httptest.NewRequest(http.MethodPost, "/settings/notifications", nil)
	req.Header.Set("Authorization", "Bearer "+makeValidJWT(t, 7, "alice", false))

	var got Claims
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got, _ = ClaimsFromContext(r.Context())
		w.WriteHeader(http.StatusOK)
	})
	rr := httptest.NewRecorder()
	Auth(testSecret, "cz_token", nil, oauth, testUnauthorized)(h).ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", rr.Code)
	}
	if got.UserID != 7 || got.Scoped {
		t.Errorf("claims mismatch: %+v", got)
	}
}

func TestPAT_ScopeEnforcement(t *testing.T) {
	read := []string{model.ScopeRepoRead}
	every := []string{model.ScopeRepoRead, model.ScopeRepoWrite, model.ScopeIssuesWrite, model.ScopePullsWrite}
	const closed = `Bearer error="insufficient_scope"`
	tests := []struct {
		name      string
		optional  bool
		method    string
		target    string
		scopes    []string
		want      int
		challenge string
	}{
		{"repo read with repo:read", false, "GET", "/api/repos/bob/proj", read, http.StatusOK, ""},
		{"repo read on optional auth with repo:read", true, "GET", "/api/repos/bob/proj", read, http.StatusOK, ""},
		{"repo read without scopes", true, "GET", "/api/repos/bob/proj", nil, http.StatusForbidden, closed + `, scope="repo:read"`},
		{"create issue with repo:read", false, "POST", "/api/repos/bob/proj/issues", read, http.StatusForbidden, closed + `, scope="issues:write"`},
		{"push advert with repo:read", true, "GET", "/bob/proj/info/refs?service=git-receive-pack", read, http.StatusForbidden, closed + `, scope="repo:write"`},
		{"mint a PAT with every scope", false, "POST", "/api/user/tokens", every, http.StatusForbidden, closed},
		{"add an SSH key with every scope", false, "POST", "/api/user/keys", every, http.StatusForbidden, closed},
		{"admin API with every scope", false, "POST", "/api/admin/settings", every, http.StatusForbidden, closed},
		{"add webhook with every scope", false, "POST", "/api/repos/bob/proj/hooks", every, http.StatusForbidden, closed + `, scope="repo:admin"`},
		{"profile page with every scope", true, "GET", "/bob", every, http.StatusForbidden, closed},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pat := &stubPAT{token: &model.AccessToken{ID: 1, Scopes: tt.scopes}, user: &model.User{ID: 42, Username: "bob"}}
			mw := Auth(testSecret, "cz_token", pat, nil, testUnauthorized)
			if tt.optional {
				mw = OptionalAuth(testSecret, "cz_token", pat, nil)
			}
			req := httptest.NewRequest(tt.method, tt.target, nil)
			req.Header.Set("Authorization", "Bearer czp_token")

			rr := runAuth(mw, req)

			if rr.Code != tt.want {
				t.Fatalf("want %d, got %d", tt.want, rr.Code)
			}
			if got := rr.Header().Get("WWW-Authenticate"); got != tt.challenge {
				t.Errorf("WWW-Authenticate = %q, want %q", got, tt.challenge)
			}
		})
	}
}
