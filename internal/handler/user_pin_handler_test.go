package handler_test

import (
	"context"
	"math"
	"net/http"
	"net/http/httptest"
	"strconv"
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

func TestPinRepo_UnreadableOrMissingRepoIs404(t *testing.T) {
	db := testutil.OpenTestDB(t)
	ctx := context.Background()
	suffix := testutil.UniqueSuffix(t)
	userID := testutil.SeedUser(t, db, suffix)
	otherSuffix := suffix + "_other"
	otherID := testutil.SeedUser(t, db, otherSuffix)
	other := "testuser_" + otherSuffix
	publicID := testutil.SeedRepo(t, db, otherID, other, otherSuffix+"_public")
	privateID := testutil.SeedRepo(t, db, otherID, other, otherSuffix+"_private")
	if _, err := db.ExecContext(ctx, `UPDATE repositories SET private = TRUE WHERE id = $1`, privateID); err != nil {
		t.Fatalf("make private: %v", err)
	}

	cfg := &config.Config{
		Auth: config.AuthConfig{JWTSecret: testJWTSecret, JWTExpiry: 24 * time.Hour, CookieName: testCookieName},
		Git:  config.GitConfig{ReposRoot: t.TempDir()},
	}
	svc := service.New(store.New(db), cfg)
	r := chi.NewRouter()
	r.Post("/api/users/{id}/pinned-repos/{repoID}", handler.New(svc, cfg).PinRepo)
	unauthorized := func(w http.ResponseWriter, _ *http.Request) { http.Error(w, "unauthorized", http.StatusUnauthorized) }
	router := middleware.Auth(testJWTSecret, testCookieName, nil, nil, unauthorized)(r)
	token := makeIssueJWT(t, userID, "testuser_"+suffix)

	for _, tc := range []struct {
		name   string
		repoID int64
		want   int
	}{
		{"another user's private repo", privateID, http.StatusNotFound},
		{"nonexistent repo", math.MaxInt64, http.StatusNotFound},
		{"another user's public repo", publicID, http.StatusOK},
	} {
		path := "/api/users/" + strconv.FormatInt(userID, 10) + "/pinned-repos/" + strconv.FormatInt(tc.repoID, 10)
		req := httptest.NewRequest(http.MethodPost, path, nil)
		req.Header.Set("Authorization", "Bearer "+token)
		rr := httptest.NewRecorder()
		router.ServeHTTP(rr, req)
		if rr.Code != tc.want {
			t.Errorf("%s: want %d, got %d: %s", tc.name, tc.want, rr.Code, rr.Body.String())
		}
	}
}
