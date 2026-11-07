package handler_test

import (
	"context"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/store"
	"github.com/mkappworks-dev/cloudzilla-app/internal/testutil"
)

func TestSubmitReview_HTMXAnswersWithoutBody_AuthorMayOnlyComment(t *testing.T) {
	db := testutil.OpenTestDB(t)
	suffix := testutil.UniqueSuffix(t)
	authorID := testutil.SeedUser(t, db, suffix)
	author := "testuser_" + suffix
	repoID := testutil.SeedRepo(t, db, authorID, author, suffix)
	pull := &model.PullRequest{RepoID: repoID, AuthorID: authorID, Title: "Review me", State: model.PRStateOpen, HeadBranch: "feature", BaseBranch: "main"}
	if err := store.NewPullStore(db).Create(context.Background(), pull); err != nil {
		t.Fatalf("seed pull: %v", err)
	}
	srv, _ := newNotifyServer(t, db)
	token := makeIssueJWT(t, authorID, author)
	path := srv.URL + "/api/repos/" + author + "/" + "testrepo_" + suffix + "/pulls/" + itoa(pull.Number) + "/reviews"

	post := func(state string) (*http.Response, string) {
		form := url.Values{"state": {state}, "body": {"note"}}
		req, _ := http.NewRequest(http.MethodPost, path, strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.Header.Set("HX-Request", "true")
		req.Header.Set("Authorization", "Bearer "+token)
		resp, err := srv.Client().Do(req)
		if err != nil {
			t.Fatalf("post review: %v", err)
		}
		defer func() { _ = resp.Body.Close() }()
		b, _ := io.ReadAll(resp.Body)
		return resp, string(b)
	}

	if resp, body := post("commented"); resp.StatusCode != http.StatusNoContent || body != "" {
		t.Errorf("author comment review: want 204 with no body, got %d %q", resp.StatusCode, body)
	}
	if resp, _ := post("approved"); resp.StatusCode != http.StatusUnprocessableEntity {
		t.Errorf("author approval: want 422, got %d", resp.StatusCode)
	}
}
