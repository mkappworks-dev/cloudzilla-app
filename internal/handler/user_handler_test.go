package handler_test

// Integration tests for public user surfaces. All tests require TEST_DATABASE_DSN and skip otherwise.

import (
	"context"
	"database/sql"
	"encoding/json"
	"maps"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/mkappworks-dev/cloudzilla-app/internal/config"
	"github.com/mkappworks-dev/cloudzilla-app/internal/handler"
	"github.com/mkappworks-dev/cloudzilla-app/internal/middleware"
	"github.com/mkappworks-dev/cloudzilla-app/internal/service"
	"github.com/mkappworks-dev/cloudzilla-app/internal/store"
	"github.com/mkappworks-dev/cloudzilla-app/internal/testutil"
)

func newUserTestRouter(t *testing.T, db *sql.DB) (http.Handler, *service.Services) {
	t.Helper()
	cfg := &config.Config{
		Auth: config.AuthConfig{JWTSecret: testJWTSecret, JWTExpiry: 24 * time.Hour, CookieName: testCookieName},
		Git:  config.GitConfig{ReposRoot: t.TempDir()},
	}
	svc := service.New(store.New(db), cfg)
	h := handler.New(svc, cfg)
	r := chi.NewRouter()
	r.Get("/api/users/{username}", h.GetUser)
	r.Get("/api/repos/{owner}/{repo}/stargazers", h.ListStargazers)
	r.Get("/{owner}", h.PageUser)
	return middleware.OptionalAuth(testJWTSecret, testCookieName, nil, nil)(r), svc
}

// An exact allowlist, so a field added to model.User can't reach unauthenticated callers unnoticed.
var publicUserKeys = []string{"avatar_url", "bio", "created_at", "id", "username"}

func assertPublicUserKeys(t *testing.T, obj map[string]any) {
	t.Helper()
	if got := slices.Sorted(maps.Keys(obj)); !slices.Equal(got, publicUserKeys) {
		t.Errorf("user JSON keys = %v, want exactly %v", got, publicUserKeys)
	}
}

func TestGetUser_Anonymous_ReturnsOnlyPublicFields(t *testing.T) {
	db := testutil.OpenTestDB(t)
	suffix := testutil.UniqueSuffix(t)
	userID := testutil.SeedUser(t, db, suffix)
	username := "testuser_" + suffix
	createdAt := time.Date(2024, 1, 2, 3, 4, 5, 0, time.UTC)
	if _, err := db.ExecContext(context.Background(),
		`UPDATE users SET bio = 'the bio', avatar_url = 'https://avatar.test/a.png', created_at = $1 WHERE id = $2`,
		createdAt, userID); err != nil {
		t.Fatalf("set profile fields: %v", err)
	}
	router, _ := newUserTestRouter(t, db)

	req := httptest.NewRequest(http.MethodGet, "/api/users/"+username, nil)
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", rr.Code, rr.Body.String())
	}
	if strings.Contains(rr.Body.String(), username+"@test.invalid") {
		t.Errorf("response leaks the user's email: %s", rr.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	assertPublicUserKeys(t, body)
	if body["id"] != float64(userID) || body["username"] != username {
		t.Errorf("got id=%v username=%v, want id=%d username=%s", body["id"], body["username"], userID, username)
	}
	if body["bio"] != "the bio" || body["avatar_url"] != "https://avatar.test/a.png" {
		t.Errorf("got bio=%v avatar_url=%v, want the seeded values", body["bio"], body["avatar_url"])
	}
	createdAtStr, _ := body["created_at"].(string)
	if got, err := time.Parse(time.RFC3339Nano, createdAtStr); err != nil || !got.Equal(createdAt) {
		t.Errorf("created_at = %v, want %s", body["created_at"], createdAt.Format(time.RFC3339))
	}
}

func TestListStargazers_HXRequest_ReturnsOnlyPublicFields(t *testing.T) {
	db := testutil.OpenTestDB(t)
	suffix := testutil.UniqueSuffix(t)
	ownerID := testutil.SeedUser(t, db, suffix)
	ownerName := "testuser_" + suffix
	testutil.SeedRepo(t, db, ownerID, ownerName, suffix)
	fanSuffix := suffix + "_fan"
	fanID := testutil.SeedUser(t, db, fanSuffix)
	fanName := "testuser_" + fanSuffix
	router, svc := newUserTestRouter(t, db)

	repoName := "testrepo_" + suffix
	if err := svc.Star.Star(context.Background(), ownerName, repoName, fanID); err != nil {
		t.Fatalf("star: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/repos/"+ownerName+"/"+repoName+"/stargazers", nil)
	req.Header.Set("HX-Request", "true")
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", rr.Code, rr.Body.String())
	}
	if strings.Contains(rr.Body.String(), fanName+"@test.invalid") {
		t.Errorf("response leaks the stargazer's email: %s", rr.Body.String())
	}
	var body []map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(body) != 1 {
		t.Fatalf("want 1 stargazer, got %d: %s", len(body), rr.Body.String())
	}
	assertPublicUserKeys(t, body[0])
	if body[0]["username"] != fanName {
		t.Errorf("username = %v, want %s", body[0]["username"], fanName)
	}
}

func TestPageUser_EmailShownOnlyToProfileOwner(t *testing.T) {
	db := testutil.OpenTestDB(t)
	suffix := testutil.UniqueSuffix(t)
	ownerID := testutil.SeedUser(t, db, suffix)
	ownerName := "testuser_" + suffix
	otherSuffix := suffix + "_other"
	otherID := testutil.SeedUser(t, db, otherSuffix)
	router, _ := newUserTestRouter(t, db)

	tests := []struct {
		name      string
		token     string
		wantEmail bool
	}{
		{"anonymous", "", false},
		{"other user", makeIssueJWT(t, otherID, "testuser_"+otherSuffix), false},
		{"owner", makeIssueJWT(t, ownerID, ownerName), true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/"+ownerName, nil)
			if tc.token != "" {
				req.Header.Set("Authorization", "Bearer "+tc.token)
			}
			rr := httptest.NewRecorder()
			router.ServeHTTP(rr, req)

			if rr.Code != http.StatusOK {
				t.Fatalf("want 200, got %d", rr.Code)
			}
			if got := strings.Contains(rr.Body.String(), ownerName+"@test.invalid"); got != tc.wantEmail {
				t.Errorf("email shown = %v, want %v", got, tc.wantEmail)
			}
		})
	}
}
