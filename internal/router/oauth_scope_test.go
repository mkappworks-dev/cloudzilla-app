package router_test

// Integration tests for OAuth-app scope enforcement through the real route table.
// All tests require TEST_DATABASE_DSN and skip otherwise.

import (
	"context"
	"database/sql"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/mkappworks-dev/cloudzilla-app/internal/config"
	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/router"
	"github.com/mkappworks-dev/cloudzilla-app/internal/service"
	"github.com/mkappworks-dev/cloudzilla-app/internal/store"
	"github.com/mkappworks-dev/cloudzilla-app/internal/testutil"
)

const (
	testJWTSecret   = "test-router-secret-32bytes-min!!"
	testRedirectURI = "https://client.example/cb"
)

func newTestRouter(t *testing.T) (http.Handler, *service.Services, *sql.DB) {
	t.Helper()
	db := testutil.OpenTestDB(t)
	cfg := &config.Config{
		Server: config.ServerConfig{BaseURL: "http://localhost"},
		Auth:   config.AuthConfig{JWTSecret: testJWTSecret, JWTExpiry: time.Hour, CookieName: "cz_token"},
		Git:    config.GitConfig{ReposRoot: t.TempDir()},
	}
	svc := service.New(store.New(db), cfg)
	return router.New(svc, cfg, fstest.MapFS{}), svc, db
}

func makeJWT(t *testing.T, userID int64, username string) string {
	t.Helper()
	tok := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"sub":      float64(userID),
		"username": username,
		"exp":      float64(time.Now().Add(time.Hour).Unix()),
	})
	s, err := tok.SignedString([]byte(testJWTSecret))
	if err != nil {
		t.Fatalf("makeJWT: %v", err)
	}
	return s
}

// grantOAuthToken runs the authorization-code flow for userID and returns the bearer token.
func grantOAuthToken(t *testing.T, svc *service.Services, userID int64, scopes ...string) string {
	t.Helper()
	ctx := context.Background()
	app, secret, err := svc.OAuthApp.CreateApp(ctx, userID, "Scope Test App", "", "", []string{testRedirectURI})
	if err != nil {
		t.Fatalf("CreateApp: %v", err)
	}
	code, err := svc.OAuthApp.Authorize(ctx, app.ID, userID, testRedirectURI, scopes, app)
	if err != nil {
		t.Fatalf("Authorize: %v", err)
	}
	token, err := svc.OAuthApp.ExchangeCode(ctx, app.ClientID, secret, code, testRedirectURI)
	if err != nil {
		t.Fatalf("ExchangeCode: %v", err)
	}
	return token
}

func TestOAuthTokenScopes_ThroughRouter(t *testing.T) {
	h, svc, db := newTestRouter(t)
	suffix := testutil.UniqueSuffix(t)
	userID := testutil.SeedUser(t, db, suffix)
	username := "testuser_" + suffix
	email := username + "@test.invalid"
	testutil.SeedRepo(t, db, userID, username, suffix)
	repoPath := "/api/repos/" + username + "/testrepo_" + suffix

	readTok := grantOAuthToken(t, svc, userID, model.ScopeRepoRead)
	issuesTok := grantOAuthToken(t, svc, userID, model.ScopeIssuesWrite)
	pullsTok := grantOAuthToken(t, svc, userID, model.ScopePullsWrite)
	writeTok := grantOAuthToken(t, svc, userID, model.ScopeRepoWrite)
	allTok := grantOAuthToken(t, svc, userID, model.ScopeRepoRead, model.ScopeRepoWrite, model.ScopeIssuesWrite, model.ScopePullsWrite)
	session := makeJWT(t, userID, username)

	tests := []struct {
		name   string
		token  string
		method string
		path   string
		body   string
		want   int
	}{
		{"repo read with repo:read", readTok, "GET", repoPath, "", http.StatusOK},
		{"repo read with issues:write", issuesTok, "GET", repoPath, "", http.StatusOK},
		{"create issue with repo:read", readTok, "POST", repoPath + "/issues", "{", http.StatusForbidden},
		// A malformed body gets past the scope gate and is rejected by the handler.
		{"create issue with issues:write", issuesTok, "POST", repoPath + "/issues", "{", http.StatusBadRequest},
		{"create release with issues:write", issuesTok, "POST", repoPath + "/releases", "{", http.StatusForbidden},
		{"merge PR with pulls:write", pullsTok, "PATCH", repoPath + "/pulls/999", `{"state":"merged"}`, http.StatusForbidden},
		{"enable auto-merge with pulls:write", pullsTok, "PATCH", repoPath + "/pulls/999", `{"auto_merge":"enable"}`, http.StatusForbidden},
		// Past the scope checks, the missing pull request is reported.
		{"merge PR with repo:write", writeTok, "PATCH", repoPath + "/pulls/999", `{"state":"merged"}`, http.StatusNotFound},
		{"apply suggestion with pulls:write", pullsTok, "POST", repoPath + "/pulls/1/line_comments/1/apply", "", http.StatusForbidden},
		{"update repo settings with every scope", allTok, "PATCH", repoPath, "{}", http.StatusForbidden},
		{"add webhook with every scope", allTok, "POST", repoPath + "/hooks", "{}", http.StatusForbidden},
		{"own profile page with every scope", allTok, "GET", "/" + username, "", http.StatusForbidden},
		{"notification settings with every scope", allTok, "GET", "/settings/notifications", "", http.StatusForbidden},
		{"mint a PAT with every scope", allTok, "POST", "/api/user/tokens", "name=x", http.StatusForbidden},
		{"register an OAuth app with every scope", allTok, "POST", "/api/oauth/apps", "{}", http.StatusForbidden},
		{"unknown token on a required-auth API", "not-a-real-token", "POST", repoPath + "/issues", "{", http.StatusUnauthorized},
		{"session on notification settings", session, "GET", "/settings/notifications", "", http.StatusOK},
		{"session on own profile page", session, "GET", "/" + username, "", http.StatusOK},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(tt.method, tt.path, strings.NewReader(tt.body))
			req.Header.Set("Authorization", "Bearer "+tt.token)
			req.Header.Set("Accept", "application/json")
			rr := httptest.NewRecorder()
			h.ServeHTTP(rr, req)

			if rr.Code != tt.want {
				t.Fatalf("want %d, got %d: %s", tt.want, rr.Code, rr.Body.String())
			}
			if rr.Code == http.StatusForbidden {
				if got := rr.Header().Get("WWW-Authenticate"); !strings.Contains(got, `error="insufficient_scope"`) {
					t.Errorf("WWW-Authenticate = %q, want insufficient_scope challenge", got)
				}
				if strings.Contains(rr.Body.String(), email) {
					t.Error("refused response leaks the user's email")
				}
			}
		})
	}
}

// The profile page shows the owner's email to a first-party session; an OAuth
// token for the same user must not reach it.
func TestOAuthToken_CannotReadOwnEmailFromProfile(t *testing.T) {
	h, svc, db := newTestRouter(t)
	suffix := testutil.UniqueSuffix(t)
	userID := testutil.SeedUser(t, db, suffix)
	username := "testuser_" + suffix
	email := username + "@test.invalid"

	get := func(token string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, "/"+username, nil)
		req.Header.Set("Authorization", "Bearer "+token)
		rr := httptest.NewRecorder()
		h.ServeHTTP(rr, req)
		return rr
	}

	if rr := get(makeJWT(t, userID, username)); !strings.Contains(rr.Body.String(), email) {
		t.Fatalf("precondition: session view of own profile should show email (status %d)", rr.Code)
	}
	rr := get(grantOAuthToken(t, svc, userID, model.ScopeRepoRead, model.ScopeRepoWrite))
	if rr.Code != http.StatusForbidden {
		t.Errorf("want 403, got %d", rr.Code)
	}
	if strings.Contains(rr.Body.String(), email) {
		t.Error("OAuth token read the user's email from the profile page")
	}
}

// UpdateComment and DeleteComment serve both the issue and pull route trees, so
// the comment must belong to the issue or pull the URL names; otherwise a token
// scoped to one could edit the other's comments.
func TestOAuthToken_CommentMustBelongToURLParent(t *testing.T) {
	h, svc, db := newTestRouter(t)
	ctx := context.Background()
	suffix := testutil.UniqueSuffix(t)
	userID := testutil.SeedUser(t, db, suffix)
	username := "testuser_" + suffix
	repoName := "testrepo_" + suffix
	testutil.SeedRepo(t, db, userID, username, suffix)
	repo, err := svc.Repo.Get(ctx, username, repoName)
	if err != nil {
		t.Fatalf("Repo.Get: %v", err)
	}
	issue, err := svc.Issue.Create(ctx, username, repoName, userID, "an issue", "", "public")
	if err != nil {
		t.Fatalf("Issue.Create: %v", err)
	}
	comment, err := svc.Comment.CreateForIssue(ctx, *repo, issue.ID, issue.Number, userID, username, "original")
	if err != nil {
		t.Fatalf("CreateForIssue: %v", err)
	}
	pullsTok := grantOAuthToken(t, svc, userID, model.ScopePullsWrite)
	issuesTok := grantOAuthToken(t, svc, userID, model.ScopeIssuesWrite)
	base := "/api/repos/" + username + "/" + repoName

	tests := []struct {
		name   string
		token  string
		method string
		path   string
		want   int
	}{
		{"edit issue comment via pulls path", pullsTok, "PATCH", fmt.Sprintf("%s/pulls/%d/comments/%d", base, issue.Number, comment.ID), http.StatusNotFound},
		{"edit issue comment under another issue number", issuesTok, "PATCH", fmt.Sprintf("%s/issues/%d/comments/%d", base, issue.Number+1, comment.ID), http.StatusNotFound},
		{"edit issue comment via its own issue", issuesTok, "PATCH", fmt.Sprintf("%s/issues/%d/comments/%d", base, issue.Number, comment.ID), http.StatusOK},
		// Last, so a regression that deletes the comment cannot mask the cases above.
		{"delete issue comment via pulls path", pullsTok, "DELETE", fmt.Sprintf("%s/pulls/%d/comments/%d", base, issue.Number, comment.ID), http.StatusNotFound},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(tt.method, tt.path, strings.NewReader(`{"body":"edited"}`))
			req.Header.Set("Authorization", "Bearer "+tt.token)
			rr := httptest.NewRecorder()
			h.ServeHTTP(rr, req)
			if rr.Code != tt.want {
				t.Errorf("want %d, got %d: %s", tt.want, rr.Code, rr.Body.String())
			}
		})
	}
}
