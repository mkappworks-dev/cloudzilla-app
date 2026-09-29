package handler_test

import (
	"context"
	"database/sql"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/mkappworks-dev/cloudzilla-app/internal/config"
	"github.com/mkappworks-dev/cloudzilla-app/internal/handler"
	"github.com/mkappworks-dev/cloudzilla-app/internal/middleware"
	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/service"
	"github.com/mkappworks-dev/cloudzilla-app/internal/store"
	"github.com/mkappworks-dev/cloudzilla-app/internal/testutil"
)

func newNotifyServer(t *testing.T, db *sql.DB) (*httptest.Server, *service.Services) {
	t.Helper()
	cfg := &config.Config{
		Auth: config.AuthConfig{JWTSecret: testJWTSecret, JWTExpiry: 24 * time.Hour, CookieName: testCookieName},
		Git:  config.GitConfig{ReposRoot: t.TempDir()},
	}
	svc := service.New(store.New(db), cfg)
	h := handler.New(svc, cfg)
	r := chi.NewRouter()
	r.Route("/api/repos/{owner}/{repo}", func(r chi.Router) {
		r.Patch("/issues/{number}", h.UpdateIssue)
		r.Post("/issues/{number}/comments", h.CreateIssueComment)
		r.Patch("/pulls/{number}", h.UpdatePull)
		r.Post("/pulls/{number}/comments", h.CreatePullComment)
		r.Post("/pulls/{number}/reviews", h.SubmitReview)
		r.Post("/discussions/{number}/replies", h.CreateReply)
	})
	unauthorized := func(w http.ResponseWriter, _ *http.Request) { http.Error(w, "unauthorized", http.StatusUnauthorized) }
	srv := httptest.NewServer(middleware.Auth(testJWTSecret, testCookieName, nil, nil, unauthorized)(r))
	t.Cleanup(srv.Close)
	return srv, svc
}

func sendAs(t *testing.T, srv *httptest.Server, token, method, url, body string) {
	t.Helper()
	req, err := http.NewRequest(method, url, strings.NewReader(body))
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := srv.Client().Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, url, err)
	}
	respBody, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		t.Fatalf("%s %s: got %d: %s", method, url, resp.StatusCode, respBody)
	}
}

func awaitNotification(t *testing.T, db *sql.DB, userID, repoID, actorID int64, want model.NotificationType) {
	t.Helper()
	notifs := store.NewNotificationStore(db)
	deadline := time.Now().Add(5 * time.Second)
	for {
		list, err := notifs.ListByUser(context.Background(), userID)
		if err != nil {
			t.Fatalf("list notifications: %v", err)
		}
		for _, n := range list {
			if n.Type == want && n.RepoID == repoID && n.ActorID == actorID {
				return
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("no %s notification after the request finished", want)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// httptest.NewRecorder never cancels the request context; a real server does
// once the handler returns, which is what each fire-and-forget notify must survive.
func TestNotify_DeliveredAfterRequestEnds(t *testing.T) {
	db := testutil.OpenTestDB(t)
	ctx := context.Background()
	suffix := testutil.UniqueSuffix(t)
	authorID := testutil.SeedUser(t, db, suffix)
	author := "testuser_" + suffix
	actorID := testutil.SeedUser(t, db, suffix+"_actor")
	repoName := "testrepo_" + suffix
	repoID := testutil.SeedRepo(t, db, authorID, author, suffix)
	testutil.Exec(t, db, `INSERT INTO permissions (user_id, repo_id, role) VALUES ($1, $2, 'writer')`, actorID, repoID)
	srv, svc := newNotifyServer(t, db)

	issue := seedIssueForComment(t, db, repoID, authorID)
	pull := &model.PullRequest{RepoID: repoID, AuthorID: authorID, Title: "Notify me", State: model.PRStateOpen, HeadBranch: "feature", BaseBranch: "main"}
	if err := store.NewPullStore(db).Create(ctx, pull); err != nil {
		t.Fatalf("seed pull: %v", err)
	}
	categories, err := svc.Discussion.ListCategories(ctx)
	if err != nil || len(categories) == 0 {
		t.Fatalf("discussion categories: %v (%d found)", err, len(categories))
	}
	discussion, err := svc.Discussion.Create(ctx, author, repoName, authorID, author, categories[0].ID, "Question", "")
	if err != nil {
		t.Fatalf("seed discussion: %v", err)
	}

	base := srv.URL + "/api/repos/" + author + "/" + repoName
	token := makeIssueJWT(t, actorID, author+"_actor")
	// State changes run last so the comments and review land on an open PR.
	for _, tc := range []struct {
		name, method, path, body string
		want                     model.NotificationType
	}{
		{"issue comment", http.MethodPost, fmt.Sprintf("/issues/%d/comments", issue), `{"body":"Looks good"}`, model.NotifIssueComment},
		{"pull comment", http.MethodPost, fmt.Sprintf("/pulls/%d/comments", pull.Number), `{"body":"Looks good"}`, model.NotifPRComment},
		{"review", http.MethodPost, fmt.Sprintf("/pulls/%d/reviews", pull.Number), `{"state":"approved","body":"LGTM"}`, model.NotifPRReview},
		{"discussion reply", http.MethodPost, fmt.Sprintf("/discussions/%d/replies", discussion.Number), `{"body":"Try this"}`, model.NotifDiscussionReply},
		{"issue closed", http.MethodPatch, fmt.Sprintf("/issues/%d", issue), `{"state":"closed"}`, model.NotifIssueClosed},
		{"pull closed", http.MethodPatch, fmt.Sprintf("/pulls/%d", pull.Number), `{"state":"closed"}`, model.NotifPRClosed},
	} {
		t.Run(tc.name, func(t *testing.T) {
			sendAs(t, srv, token, tc.method, base+tc.path, tc.body)
			awaitNotification(t, db, authorID, repoID, actorID, tc.want)
		})
	}
}
