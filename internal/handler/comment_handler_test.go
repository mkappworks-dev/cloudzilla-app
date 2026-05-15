package handler_test

// Integration tests for comment handler. All tests require TEST_DATABASE_DSN and skip otherwise.

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/mkappworks-dev/cloudzilla-app/internal/config"
	"github.com/mkappworks-dev/cloudzilla-app/internal/handler"
	"github.com/mkappworks-dev/cloudzilla-app/internal/middleware"
	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/service"
	"github.com/mkappworks-dev/cloudzilla-app/internal/store"
	"github.com/mkappworks-dev/cloudzilla-app/internal/testutil"
)

// newCommentHandler builds a Handler with the full set of services required by
// the comment handler: Comment, Issue, Repo, Notification, and auth-related services.
func newCommentHandler(db *sql.DB) *handler.Handler {
	cfg := &config.Config{
		Auth: config.AuthConfig{
			JWTSecret:  testJWTSecret,
			JWTExpiry:  24 * time.Hour,
			CookieName: testCookieName,
		},
	}
	userSvc := service.NewUserService(store.NewUserStore(db), cfg.Auth)
	repoSvc := service.NewRepoService(store.NewRepoStore(db), store.NewUserStore(db), store.NewOrgStore(db), nil, nil, nil, config.GitConfig{})
	emailSvc := service.NewEmailService(config.SMTPConfig{})
	notifSvc := service.NewNotificationService(store.NewNotificationStore(db), store.NewWatchStore(db), emailSvc, userSvc)
	issueSvc := service.NewIssueService(store.NewIssueStore(db), store.NewRepoStore(db), repoSvc)
	svc := &service.Services{
		User:         userSvc,
		Repo:         repoSvc,
		Issue:        issueSvc,
		Comment:      service.NewCommentService(store.NewCommentStore(db), store.NewMentionStore(db), userSvc, notifSvc),
		Notification: notifSvc,
		SiteSetting:  service.NewSiteSettingService(store.NewSiteSettingStore(db), store.NewUserStore(db)),
		AuditLog:     service.NewAuditService(store.NewAuditLogStore(db)),
	}
	return handler.New(svc, cfg)
}

// commentRouter wraps comment handler methods in a chi.Mux so URL params populate.
func commentRouter(h *handler.Handler) *chi.Mux {
	r := chi.NewRouter()
	r.Post("/api/repos/{owner}/{repo}/issues/{number}/comments", h.CreateIssueComment)
	r.Delete("/api/comments/{id}", h.DeleteComment)
	return r
}

// commentRouterWithAuth wraps the comment router in Auth middleware.
func commentRouterWithAuth(h *handler.Handler) http.Handler {
	return middleware.Auth(testJWTSecret, testCookieName, nil, nil, func(w http.ResponseWriter, _ *http.Request) { http.Error(w, "unauthorized", http.StatusUnauthorized) })(commentRouter(h))
}

// commentBody serializes a comment body string into a JSON request buffer.
func commentBody(body string) *bytes.Buffer {
	b, _ := json.Marshal(map[string]string{"body": body})
	return bytes.NewBuffer(b)
}

// seedIssueForComment inserts a minimal issue row for use as a comment parent.
// Returns the issue's repo-scoped Number.
func seedIssueForComment(t *testing.T, db *sql.DB, repoID, authorID int64) int {
	t.Helper()
	is := store.NewIssueStore(db)
	issue := &model.Issue{
		RepoID:     repoID,
		AuthorID:   authorID,
		Title:      "Comment parent issue",
		State:      model.IssueStateOpen,
		Visibility: "public",
	}
	if err := is.Create(context.Background(), issue); err != nil {
		t.Fatalf("seedIssueForComment: %v", err)
	}
	return issue.Number
}

// TestCreateIssueComment_NoAuth_401 verifies that POST to the issue comment endpoint
// returns HTTP 401 when no Authorization header is present.
func TestCreateIssueComment_NoAuth_401(t *testing.T) {
	db := testutil.OpenTestDB(t)
	h := newCommentHandler(db)
	router := commentRouterWithAuth(h)

	req := httptest.NewRequest(http.MethodPost,
		"/api/repos/owner/repo/issues/1/comments",
		commentBody("hello"))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Errorf("want 401, got %d", rr.Code)
	}
}

// TestCreateIssueComment_EmptyBody_400 verifies that an authenticated request with an
// empty comment body returns HTTP 400 (body is required).
func TestCreateIssueComment_EmptyBody_400(t *testing.T) {
	db := testutil.OpenTestDB(t)
	suffix := testutil.UniqueSuffix(t)
	ownerID := testutil.SeedUser(t, db, suffix)
	ownerName := "testuser_" + suffix
	repoName := "testrepo_" + suffix
	repoID := testutil.SeedRepo(t, db, ownerID, ownerName, suffix)
	issueNumber := seedIssueForComment(t, db, repoID, ownerID)

	h := newCommentHandler(db)
	router := commentRouterWithAuth(h)
	token := makeIssueJWT(t, ownerID, ownerName)

	req := httptest.NewRequest(http.MethodPost,
		"/api/repos/"+ownerName+"/"+repoName+"/issues/"+itoa(issueNumber)+"/comments",
		commentBody("   "))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Errorf("want 400 for empty body, got %d: %s", rr.Code, rr.Body.String())
	}
}

// TestCreateIssueComment_ValidAuth_201 verifies that an authenticated user can create
// a comment on an existing issue and receives HTTP 201 with the comment in the body.
func TestCreateIssueComment_ValidAuth_201(t *testing.T) {
	db := testutil.OpenTestDB(t)
	suffix := testutil.UniqueSuffix(t)
	ownerID := testutil.SeedUser(t, db, suffix)
	ownerName := "testuser_" + suffix
	repoName := "testrepo_" + suffix
	repoID := testutil.SeedRepo(t, db, ownerID, ownerName, suffix)
	issueNumber := seedIssueForComment(t, db, repoID, ownerID)

	h := newCommentHandler(db)
	router := commentRouterWithAuth(h)
	token := makeIssueJWT(t, ownerID, ownerName)

	req := httptest.NewRequest(http.MethodPost,
		"/api/repos/"+ownerName+"/"+repoName+"/issues/"+itoa(issueNumber)+"/comments",
		commentBody("This is my comment"))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusCreated {
		t.Fatalf("want 201, got %d: %s", rr.Code, rr.Body.String())
	}
	var body map[string]any
	if err := json.NewDecoder(rr.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if id, _ := body["id"].(float64); id == 0 {
		t.Error("created comment must have non-zero ID")
	}
}
