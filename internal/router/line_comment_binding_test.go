package router_test

import (
	"context"
	"database/sql"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/mkappworks-dev/cloudzilla-app/internal/service"
	"github.com/mkappworks-dev/cloudzilla-app/internal/testutil"
)

func seedOpenPull(t *testing.T, db *sql.DB, repoID, authorID int64, number int) {
	t.Helper()
	testutil.Exec(t, db,
		`INSERT INTO pull_requests (repo_id, number, author_id, title, state, head_branch, base_branch)
		 VALUES ($1, $2, $3, 'p', 'open', 'feature', 'main')`,
		repoID, number, authorID)
}

// Line comment IDs are global, so every /line_comments/{id} route must refuse a
// comment that isn't on the pull request in its URL. Otherwise write access to
// one repo reaches suggestions and threads in repos the caller cannot read.
func TestLineComment_MustBelongToURLPull(t *testing.T) {
	h, svc, db := newTestRouter(t)
	ctx := context.Background()
	const secret = "B-SECRET"

	attackerSuffix := testutil.UniqueSuffix(t)
	attackerID := testutil.SeedUser(t, db, attackerSuffix)
	attacker := "testuser_" + attackerSuffix
	repoAName := "repoa_" + attackerSuffix
	repoA, err := svc.Repo.Create(ctx, attacker, repoAName, "", false)
	if err != nil {
		t.Fatalf("Repo.Create: %v", err)
	}
	author := service.GitAuthor{Name: attacker, Email: attacker + "@test.invalid"}
	if err := svc.Code.CommitFile(attacker, repoAName, "feature", "secret.txt", []byte("original\n"), author, "init"); err != nil {
		t.Fatalf("CommitFile: %v", err)
	}
	seedOpenPull(t, db, repoA.ID, attackerID, 1)
	seedOpenPull(t, db, repoA.ID, attackerID, 2)

	victimSuffix := testutil.UniqueSuffix(t)
	victimID := testutil.SeedUser(t, db, victimSuffix)
	victim := "testuser_" + victimSuffix
	repoBID := testutil.SeedRepo(t, db, victimID, victim, victimSuffix)
	repoBName := "testrepo_" + victimSuffix
	testutil.Exec(t, db, `UPDATE repositories SET private = true WHERE id = $1`, repoBID)
	seedOpenPull(t, db, repoBID, victimID, 1)

	lineComment := func(owner, repoName string, number int, authorID int64, authorName, body string) int64 {
		t.Helper()
		c, err := svc.PullLineComment.Create(ctx, owner, repoName, number, authorID, authorName, "secret.txt", "right", 1, body)
		if err != nil {
			t.Fatalf("PullLineComment.Create: %v", err)
		}
		return c.ID
	}
	suggestion := func(s string) string { return "```suggestion\n" + s + "\n```" }
	onB := lineComment(victim, repoBName, 1, victimID, victim, suggestion(secret))
	// Authored while the attacker could still read B, e.g. as a since-removed collaborator.
	attackerOnB := lineComment(victim, repoBName, 1, attackerID, attacker, "old note")
	onA1 := lineComment(attacker, repoAName, 1, attackerID, attacker, suggestion("applied from A"))
	onA2 := lineComment(attacker, repoAName, 2, attackerID, attacker, suggestion("from A#2"))

	pullA1 := fmt.Sprintf("/api/repos/%s/%s/pulls/1", attacker, repoAName)
	pullB1 := fmt.Sprintf("/api/repos/%s/%s/pulls/1", victim, repoBName)
	token := makeJWT(t, attackerID, attacker)

	tests := []struct {
		name   string
		method string
		path   string
		want   int
	}{
		{"apply another repo's suggestion", "POST", fmt.Sprintf("%s/line_comments/%d/apply", pullA1, onB), http.StatusNotFound},
		{"apply another pull's suggestion", "POST", fmt.Sprintf("%s/line_comments/%d/apply", pullA1, onA2), http.StatusNotFound},
		{"edit own comment under an unreadable repo's pull", "PATCH", fmt.Sprintf("%s/line_comments/%d", pullB1, onA1), http.StatusNotFound},
		{"edit own comment on a repo no longer readable", "PATCH", fmt.Sprintf("%s/line_comments/%d", pullB1, attackerOnB), http.StatusNotFound},
		{"apply own pull's suggestion", "POST", fmt.Sprintf("%s/line_comments/%d/apply", pullA1, onA1), http.StatusNoContent},
		{"edit own comment on own pull", "PATCH", fmt.Sprintf("%s/line_comments/%d", pullA1, onA1), http.StatusOK},
		// Deletes last, so a regression that deletes a comment cannot mask the cases above.
		{"delete another repo's comment", "DELETE", fmt.Sprintf("%s/line_comments/%d", pullA1, onB), http.StatusNotFound},
		{"delete another pull's comment", "DELETE", fmt.Sprintf("%s/line_comments/%d", pullA1, onA2), http.StatusNotFound},
		{"delete own comment on a repo no longer readable", "DELETE", fmt.Sprintf("%s/line_comments/%d", pullB1, attackerOnB), http.StatusNotFound},
		{"delete own comment on own pull", "DELETE", fmt.Sprintf("%s/line_comments/%d", pullA1, onA1), http.StatusOK},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// HTMX, because that response re-renders the whole thread at the comment's line.
			req := httptest.NewRequest(tt.method, tt.path, strings.NewReader("body=edited"))
			req.Header.Set("Authorization", "Bearer "+token)
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			req.Header.Set("HX-Request", "true")
			rr := httptest.NewRecorder()
			h.ServeHTTP(rr, req)
			if rr.Code != tt.want {
				t.Errorf("want %d, got %d: %s", tt.want, rr.Code, rr.Body.String())
			}
			if strings.Contains(rr.Body.String(), secret) {
				t.Errorf("response leaks repo B's line comment: %s", rr.Body.String())
			}
		})
	}

	// Every suggestion targets line 1, so a later apply would overwrite a leaked one.
	if n, err := svc.Code.CommitCount(attacker, repoAName, "feature"); err != nil || n != 2 {
		t.Errorf("want 2 commits on the head branch (init, own suggestion), got %d (err %v)", n, err)
	}
	head, err := svc.Code.GetRawBlob(attacker, repoAName, "feature", "secret.txt")
	if err != nil {
		t.Fatalf("GetRawBlob: %v", err)
	}
	if !strings.Contains(string(head), "applied from A") {
		t.Errorf("own suggestion was not applied: %q", head)
	}
	for _, id := range []int64{onA2, attackerOnB} {
		if _, err := svc.PullLineComment.GetComment(ctx, id); err != nil {
			t.Errorf("line comment %d was deleted: %v", id, err)
		}
	}
}
