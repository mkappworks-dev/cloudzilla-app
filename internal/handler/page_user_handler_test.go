package handler_test

import (
	"context"
	"net/http"
	"net/http/httptest"
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

func TestPageUser_PrivatePinHiddenFromVisitors(t *testing.T) {
	db := testutil.OpenTestDB(t)
	ctx := context.Background()
	suffix := testutil.UniqueSuffix(t)
	ownerID := testutil.SeedUser(t, db, suffix)
	owner := "testuser_" + suffix
	visitorID := testutil.SeedUser(t, db, suffix+"_visitor")
	secret := testutil.SeedRepo(t, db, ownerID, owner, suffix+"_secret")
	if _, err := db.ExecContext(ctx, `UPDATE repositories SET private = TRUE WHERE id = $1`, secret); err != nil {
		t.Fatalf("make private: %v", err)
	}

	cfg := &config.Config{
		Auth: config.AuthConfig{JWTSecret: testJWTSecret, JWTExpiry: 24 * time.Hour, CookieName: testCookieName},
		Git:  config.GitConfig{ReposRoot: t.TempDir()},
	}
	svc := service.New(store.New(db), cfg)
	r := chi.NewRouter()
	r.Get("/{owner}", handler.New(svc, cfg).PageUser)
	router := middleware.OptionalAuth(testJWTSecret, testCookieName, nil, nil)(r)

	if err := svc.User.PinRepo(ctx, ownerID, secret); err != nil {
		t.Fatalf("pin: %v", err)
	}

	pinnedCard := `href="/` + owner + `/testrepo_` + suffix + `_secret"`
	for _, tc := range []struct {
		name  string
		token string
		want  bool
	}{
		{"anonymous", "", false},
		{"visitor", makeIssueJWT(t, visitorID, owner+"_visitor"), false},
		{"owner", makeIssueJWT(t, ownerID, owner), true},
	} {
		req := httptest.NewRequest(http.MethodGet, "/"+owner, nil)
		if tc.token != "" {
			req.Header.Set("Authorization", "Bearer "+tc.token)
		}
		rr := httptest.NewRecorder()
		router.ServeHTTP(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("%s: want 200, got %d", tc.name, rr.Code)
		}
		if got := strings.Contains(rr.Body.String(), pinnedCard); got != tc.want {
			t.Errorf("%s: private pinned card rendered = %v, want %v", tc.name, got, tc.want)
		}
	}
}
