package router_test

// Integration tests for personal access token scope enforcement through the real
// route table. All tests require TEST_DATABASE_DSN and skip otherwise.

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/service"
	"github.com/mkappworks-dev/cloudzilla-app/internal/testutil"
)

func mintPAT(t *testing.T, svc *service.Services, userID int64, scopes ...string) string {
	t.Helper()
	raw, _, err := svc.AccessToken.Generate(context.Background(), userID, "scope test", scopes, nil)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	return raw
}

// seedPrivateRepo creates a private repo on disk and in the database, so only a
// request carrying its owner's identity can read it.
func seedPrivateRepo(t *testing.T, svc *service.Services, userID int64, username, suffix string) string {
	t.Helper()
	name := "patrepo_" + suffix
	if _, err := svc.Repo.Create(context.Background(), userID, username, name, "", true, service.RepoInitOptions{}); err != nil {
		t.Fatalf("Repo.Create: %v", err)
	}
	return name
}

func TestPATScopes_ThroughRouter(t *testing.T) {
	h, svc, db := newTestRouter(t)
	suffix := testutil.UniqueSuffix(t)
	userID := testutil.SeedUser(t, db, suffix)
	username := "testuser_" + suffix
	repoName := seedPrivateRepo(t, svc, userID, username, suffix)
	repoPath := "/api/repos/" + username + "/" + repoName
	gitPath := "/" + username + "/" + repoName

	readTok := mintPAT(t, svc, userID, model.ScopeRepoRead)
	issuesTok := mintPAT(t, svc, userID, model.ScopeIssuesWrite)
	writeTok := mintPAT(t, svc, userID, model.ScopeRepoWrite)
	allTok := mintPAT(t, svc, userID, model.ScopeRepoRead, model.ScopeRepoWrite, model.ScopeIssuesWrite, model.ScopePullsWrite)

	tests := []struct {
		name   string
		token  string
		method string
		path   string
		body   string
		want   int
	}{
		{"private repo read with repo:read", readTok, "GET", repoPath, "", http.StatusOK},
		{"create issue with repo:read", readTok, "POST", repoPath + "/issues", "{", http.StatusForbidden},
		// A malformed body gets past the scope gate and is rejected by the handler.
		{"create issue with issues:write", issuesTok, "POST", repoPath + "/issues", "{", http.StatusBadRequest},
		{"update repo settings with every scope", allTok, "PATCH", repoPath, "{}", http.StatusForbidden},
		{"add webhook with every scope", allTok, "POST", repoPath + "/hooks", "{}", http.StatusForbidden},
		{"own profile page with every scope", allTok, "GET", "/" + username, "", http.StatusForbidden},
		{"account settings with every scope", allTok, "GET", "/settings", "", http.StatusForbidden},
		{"mint a PAT with every scope", allTok, "POST", "/api/user/tokens", "name=x&scopes=repo:write", http.StatusForbidden},
		{"add an SSH key with every scope", allTok, "POST", "/api/user/keys", "{}", http.StatusForbidden},
		{"register an OAuth app with every scope", allTok, "POST", "/api/oauth/apps", "{}", http.StatusForbidden},
		{"clone private repo with repo:read", readTok, "GET", gitPath + "/info/refs?service=git-upload-pack", "", http.StatusOK},
		{"push advert with repo:read", readTok, "GET", gitPath + "/info/refs?service=git-receive-pack", "", http.StatusForbidden},
		{"push advert with repo:write", writeTok, "GET", gitPath + "/info/refs?service=git-receive-pack", "", http.StatusOK},
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
			}
		})
	}
}

// git sends a PAT as the HTTP Basic password, which the auth middleware never
// sees; the git handlers must apply the same scopes.
func TestPATScopes_GitBasicAuth(t *testing.T) {
	h, svc, db := newTestRouter(t)
	suffix := testutil.UniqueSuffix(t)
	userID := testutil.SeedUser(t, db, suffix)
	username := "testuser_" + suffix
	gitPath := "/" + username + "/" + seedPrivateRepo(t, svc, userID, username, suffix)

	readTok := mintPAT(t, svc, userID, model.ScopeRepoRead)
	pullsTok := mintPAT(t, svc, userID, model.ScopePullsWrite)
	writeTok := mintPAT(t, svc, userID, model.ScopeRepoWrite)

	tests := []struct {
		name   string
		token  string
		method string
		path   string
		want   int
	}{
		{"clone advert with repo:read", readTok, "GET", "/info/refs?service=git-upload-pack", http.StatusOK},
		{"clone advert with pulls:write", pullsTok, "GET", "/info/refs?service=git-upload-pack", http.StatusOK},
		{"push advert with repo:read", readTok, "GET", "/info/refs?service=git-receive-pack", http.StatusForbidden},
		{"push advert with pulls:write", pullsTok, "GET", "/info/refs?service=git-receive-pack", http.StatusForbidden},
		{"push with repo:read", readTok, "POST", "/git-receive-pack", http.StatusForbidden},
		{"push advert with repo:write", writeTok, "GET", "/info/refs?service=git-receive-pack", http.StatusOK},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(tt.method, gitPath+tt.path, nil)
			req.SetBasicAuth(username, tt.token)
			rr := httptest.NewRecorder()
			h.ServeHTTP(rr, req)

			if rr.Code != tt.want {
				t.Fatalf("want %d, got %d: %s", tt.want, rr.Code, rr.Body.String())
			}
			// A 401 would make git's credential helper discard the stored token.
			if rr.Code == http.StatusForbidden && !strings.Contains(rr.Body.String(), "repo:write") {
				t.Errorf("body = %q, want it to name the missing repo:write scope", rr.Body.String())
			}
		})
	}
}

func TestPATCreate_RequiresKnownScopes(t *testing.T) {
	h, _, db := newTestRouter(t)
	suffix := testutil.UniqueSuffix(t)
	userID := testutil.SeedUser(t, db, suffix)
	session := makeJWT(t, userID, "testuser_"+suffix)

	tests := []struct {
		name string
		form string
		want int
	}{
		{"no scopes", "name=ci", http.StatusBadRequest},
		{"unknown scope", "name=ci&scopes=repo:read&scopes=admin", http.StatusBadRequest},
		{"known scope", "name=ci&scopes=repo:read", http.StatusSeeOther},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/api/user/tokens", strings.NewReader(tt.form))
			req.Header.Set("Authorization", "Bearer "+session)
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			rr := httptest.NewRecorder()
			h.ServeHTTP(rr, req)

			if rr.Code != tt.want {
				t.Errorf("want %d, got %d: %s", tt.want, rr.Code, rr.Body.String())
			}
		})
	}
}
