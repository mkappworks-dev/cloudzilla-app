package handler_test

import (
	"database/sql"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/go-git/go-git/v5/plumbing"

	"github.com/mkappworks-dev/cloudzilla-app/internal/config"
	"github.com/mkappworks-dev/cloudzilla-app/internal/service"
	"github.com/mkappworks-dev/cloudzilla-app/internal/testutil"
)

// boxFixture is a repo with an open PR (feature, behind main by one commit) and a reviewer who requested changes.
type boxFixture struct {
	raceRepo
	app           http.Handler
	reposRoot     string
	pullID        int64
	reviewID      int64
	reviewer      signedInUser
	writer, admin signedInUser
}

func newBoxFixture(t *testing.T, db *sql.DB) boxFixture {
	t.Helper()
	reposRoot := t.TempDir()
	f := boxFixture{app: newAPIRouterAt(t, db, reposRoot), reposRoot: reposRoot}
	f.raceRepo = seedRaceRepo(t, db, reposRoot)
	f.pullID = seedOpenPull(t, db, f.seededRepo)
	f.reviewer = seedSignedInUser(t, db)
	f.writer = seedSignedInUser(t, db)
	f.admin = seedSignedInUser(t, db)
	testutil.Exec(t, db, `INSERT INTO permissions (repo_id, user_id, role) VALUES ($1, $2, 'writer')`, f.id, f.writer.id)
	testutil.Exec(t, db, `INSERT INTO permissions (repo_id, user_id, role) VALUES ($1, $2, 'admin')`, f.id, f.admin.id)
	if err := db.QueryRow(
		`INSERT INTO pull_reviews (pull_id, repo_id, author_id, author_name, state, body, submitted_at)
		 VALUES ($1, $2, $3, $4, 'changes_requested', 'rework this', NOW()) RETURNING id`,
		f.pullID, f.id, f.reviewer.id, f.reviewer.name,
	).Scan(&f.reviewID); err != nil {
		t.Fatalf("seed review: %v", err)
	}
	return f
}

func (f boxFixture) dismissPath() string {
	return "/api/repos" + f.path + "/pulls/1/reviews/" + itoa(int(f.reviewID)) + "/dismiss"
}

func postHTMXForm(h http.Handler, path, token string, form url.Values) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("HX-Request", "true")
	req.Header.Set("Authorization", "Bearer "+token)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	return rr
}

func reviewState(t *testing.T, db *sql.DB, id int64) string {
	t.Helper()
	var s string
	if err := db.QueryRow(`SELECT state FROM pull_reviews WHERE id = $1`, id).Scan(&s); err != nil {
		t.Fatalf("read review: %v", err)
	}
	return s
}

func TestDismissReview_OnlyRepoAdminsAndOwners(t *testing.T) {
	db := testutil.OpenTestDB(t)
	f := newBoxFixture(t, db)
	form := url.Values{"reason": {"the author reworked it"}}

	for name, token := range map[string]string{"reviewer": f.reviewer.token, "writer": f.writer.token} {
		if rr := postHTMXForm(f.app, f.dismissPath(), token, form); rr.Code != http.StatusForbidden && rr.Code != http.StatusNotFound {
			t.Errorf("%s: want 403 or 404, got %d", name, rr.Code)
		}
	}
	if got := reviewState(t, db, f.reviewID); got != "changes_requested" {
		t.Fatalf("a refused dismissal changed the review to %s", got)
	}

	if rr := postHTMXForm(f.app, f.dismissPath(), f.admin.token, url.Values{"reason": {"  "}}); rr.Code != http.StatusUnprocessableEntity {
		t.Errorf("blank reason: want 422, got %d", rr.Code)
	}
	if rr := postHTMXForm(f.app, f.dismissPath(), f.admin.token, form); rr.Code != http.StatusNoContent {
		t.Fatalf("admin: want 204, got %d: %s", rr.Code, rr.Body.String())
	}
	if got := reviewState(t, db, f.reviewID); got != "dismissed" {
		t.Errorf("review state = %s, want dismissed", got)
	}
	if rr := postHTMXForm(f.app, f.dismissPath(), f.owner.token, form); rr.Code != http.StatusConflict {
		t.Errorf("dismissing twice: want 409, got %d", rr.Code)
	}

	var reason, reviewer string
	deadline := time.Now().Add(5 * time.Second)
	for {
		err := db.QueryRow(
			`SELECT metadata->>'reason', metadata->>'reviewer' FROM audit_log WHERE action = 'pull.review.dismiss' AND actor_id = $1`,
			f.admin.id,
		).Scan(&reason, &reviewer)
		if err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("no audit entry for the dismissal: %v", err)
		}
		time.Sleep(50 * time.Millisecond)
	}
	if reason != "the author reworked it" || reviewer != f.reviewer.name {
		t.Errorf("audit metadata = reason %q, reviewer %q", reason, reviewer)
	}

	body := requestAPI(f.app, http.MethodGet, f.path+"/pulls/1", f.owner.token).Body.String()
	assertContains(t, body, "Dismissed by")
	assertContains(t, body, "the author reworked it")
}

func TestPullDetail_DismissOfferedToManagersOnly(t *testing.T) {
	db := testutil.OpenTestDB(t)
	f := newBoxFixture(t, db)
	dialog := `id="dismiss-review-` + itoa(int(f.reviewID)) + `"`

	for _, tc := range []struct {
		name  string
		token string
		want  bool
	}{
		{"owner", f.owner.token, true},
		{"admin", f.admin.token, true},
		{"writer", f.writer.token, false},
		{"reader", seedSignedInUser(t, db).token, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			body := requestAPI(f.app, http.MethodGet, f.path+"/pulls/1", tc.token).Body.String()
			assertContains(t, body, "Changes requested by")
			assertContains(t, body, f.reviewer.name)
			if got := strings.Contains(body, dialog); got != tc.want {
				t.Errorf("dismiss dialog on the page = %v, want %v", got, tc.want)
			}
			if got := strings.Contains(body, ">Dismiss review</button>"); got != tc.want {
				t.Errorf("Dismiss review button on the page = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestUpdatePullBranch_WritersMergeBaseIntoHead(t *testing.T) {
	db := testutil.OpenTestDB(t)
	f := newBoxFixture(t, db)
	path := "/api/repos" + f.path + "/pulls/1/update-branch"
	oldHead := f.featureTip

	if rr := postHTMXForm(f.app, path, seedSignedInUser(t, db).token, nil); rr.Code != http.StatusForbidden && rr.Code != http.StatusNotFound {
		t.Errorf("stranger: want 403 or 404, got %d", rr.Code)
	}
	if got := branchHash(t, f.git, "feature"); got != oldHead {
		t.Fatalf("a refused update moved feature to %s", got)
	}

	page := func(token string) string {
		return requestAPI(f.app, http.MethodGet, f.path+"/pulls/1", token).Body.String()
	}
	assertContains(t, page(f.writer.token), "Update branch")
	if strings.Contains(page(seedSignedInUser(t, db).token), "Update branch") {
		t.Error("a reader is offered Update branch")
	}

	if rr := postHTMXForm(f.app, path, f.writer.token, nil); rr.Code != http.StatusNoContent {
		t.Fatalf("writer: want 204, got %d: %s", rr.Code, rr.Body.String())
	}
	tip := branchHash(t, f.git, "feature")
	commit, err := f.git.CommitObject(tip)
	if err != nil {
		t.Fatalf("read feature tip: %v", err)
	}
	if want := []plumbing.Hash{oldHead, f.mainTip}; len(commit.ParentHashes) != 2 || commit.ParentHashes[0] != want[0] || commit.ParentHashes[1] != want[1] {
		t.Errorf("update commit parents = %v, want %v", commit.ParentHashes, want)
	}
	if strings.Contains(page(f.writer.token), "Update branch") {
		t.Error("Update branch is still offered once the branch has caught up")
	}
	if rr := postHTMXForm(f.app, path, f.writer.token, nil); rr.Code != http.StatusConflict {
		t.Errorf("updating an up-to-date branch: want 409, got %d", rr.Code)
	}
}

func TestPullDetail_ConflictsSayWhatToDo(t *testing.T) {
	db := testutil.OpenTestDB(t)
	f := newBoxFixture(t, db)
	testutil.Exec(t, db, `DELETE FROM pull_reviews WHERE id = $1`, f.reviewID)
	code := service.NewCodeService(config.GitConfig{ReposRoot: f.reposRoot})
	for branch, content := range map[string]string{"main": "main edit\n", "feature": "feature edit\n"} {
		if _, err := code.CommitFile(f.owner.name, f.name, branch, "a.txt", []byte(content), raceAuthor, "Edit a.txt"); err != nil {
			t.Fatalf("commit on %s: %v", branch, err)
		}
	}

	for name, token := range map[string]string{"writer": f.writer.token, "reader": seedSignedInUser(t, db).token} {
		body := requestAPI(f.app, http.MethodGet, f.path+"/pulls/1", token).Body.String()
		assertContains(t, body, "Resolve the conflicts on your machine")
		assertContains(t, body, "git merge origin/main")
		assertContains(t, body, "git push origin feature")
		assertContains(t, body, "Check again")
		if strings.Contains(body, "Update branch") {
			t.Errorf("%s: Update branch is offered for a conflicted branch", name)
		}
	}
}

func TestPullDetail_NonWriterSeesANeutralReadyHeader(t *testing.T) {
	db := testutil.OpenTestDB(t)
	f := newBoxFixture(t, db)
	testutil.Exec(t, db, `DELETE FROM pull_reviews WHERE id = $1`, f.reviewID)

	reader := requestAPI(f.app, http.MethodGet, f.path+"/pulls/1", seedSignedInUser(t, db).token).Body.String()
	assertContains(t, reader, "Ready, waiting for a maintainer")
	if strings.Contains(reader, "Ready to merge") {
		t.Error("a reader sees the green Ready to merge header")
	}
	writer := requestAPI(f.app, http.MethodGet, f.path+"/pulls/1", f.writer.token).Body.String()
	assertContains(t, writer, "Ready to merge")
	if strings.Contains(writer, "waiting for a maintainer") {
		t.Error("a writer is told to wait for a maintainer")
	}
}

func TestPullDetail_CloseAndReadyAppearOnce(t *testing.T) {
	const closeTrigger = "getElementById('close-pull-dialog').showModal()"
	db := testutil.OpenTestDB(t)
	f := newBoxFixture(t, db)
	testutil.Exec(t, db, `DELETE FROM pull_reviews WHERE id = $1`, f.reviewID)

	body := requestAPI(f.app, http.MethodGet, f.path+"/pulls/1", f.owner.token).Body.String()
	if n := strings.Count(body, closeTrigger); n != 1 {
		t.Errorf("close triggers on an open pull = %d, want 1", n)
	}

	testutil.Exec(t, db, `UPDATE pull_requests SET is_draft = true WHERE id = $1`, f.pullID)
	body = requestAPI(f.app, http.MethodGet, f.path+"/pulls/1", f.owner.token).Body.String()
	if n := strings.Count(body, "Ready for review</button>"); n != 1 {
		t.Errorf("Ready for review buttons on a draft = %d, want 1", n)
	}
	if n := strings.Count(body, closeTrigger); n != 1 {
		t.Errorf("close triggers on a draft = %d, want 1", n)
	}
}
