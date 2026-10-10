package handler_test

import (
	"database/sql"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"testing"

	"github.com/mkappworks-dev/cloudzilla-app/internal/testutil"
)

// seedNewPRMetadata grants role to a PR opener and a would-be reviewer, and
// adds one label to pick.
func seedNewPRMetadata(t *testing.T, db *sql.DB, r raceRepo, role string) (labelID int64, reviewer, opener signedInUser) {
	t.Helper()
	reviewer, opener = seedSignedInUser(t, db), seedSignedInUser(t, db)
	for _, u := range []signedInUser{reviewer, opener} {
		testutil.Exec(t, db, `INSERT INTO permissions (repo_id, user_id, role) VALUES ($1, $2, $3)`, r.id, u.id, role)
	}
	if err := db.QueryRow(`INSERT INTO labels (repo_id, name) VALUES ($1, 'bug') RETURNING id`, r.id).Scan(&labelID); err != nil {
		t.Fatalf("seed label: %v", err)
	}
	return labelID, reviewer, opener
}

// openPRWithMetadata submits the new-PR form as opener with a label and a
// reviewer picked, and returns the label and review-request counts on the repo.
func openPRWithMetadata(t *testing.T, db *sql.DB, api http.Handler, r raceRepo, opener signedInUser, labelID int64, reviewer string) (labels, reviewRequests int) {
	t.Helper()
	rr := postForm(t, api, opener.token, r.path+"/pulls/new", url.Values{
		"title":       {"change"},
		"head_branch": {"feature"},
		"base_branch": {"main"},
		"labels":      {strconv.FormatInt(labelID, 10)},
		"reviewers":   {reviewer},
	})
	if rr.Code != http.StatusSeeOther {
		t.Fatalf("open PR: want 303, got %d: %s", rr.Code, rr.Body.String())
	}
	labels = countRows(t, db, `SELECT COUNT(*) FROM pull_labels pl JOIN pull_requests p ON p.id = pl.pull_id WHERE p.repo_id = $1`, r.id)
	reviewRequests = countRows(t, db, `SELECT COUNT(*) FROM pull_reviews WHERE repo_id = $1`, r.id)
	return labels, reviewRequests
}

func TestPageNewPullSubmit_ReaderOpensPRWithoutLabelsOrReviewers(t *testing.T) {
	db := testutil.OpenTestDB(t)
	reposRoot := t.TempDir()
	api := newAPIRouterAt(t, db, reposRoot)
	r := seedRaceRepo(t, db, reposRoot)
	labelID, reviewer, reader := seedNewPRMetadata(t, db, r, "reader")

	labels, reviewRequests := openPRWithMetadata(t, db, api, r, reader, labelID, reviewer.name)
	if labels != 0 || reviewRequests != 0 {
		t.Errorf("reader attached %d labels and %d review requests, want none", labels, reviewRequests)
	}
	if n := countRows(t, db, `SELECT COUNT(*) FROM pull_requests WHERE repo_id = $1`, r.id); n != 1 {
		t.Errorf("want the reader's PR created, got %d pull requests", n)
	}
}

func TestPageNewPullSubmit_WriterOpensPRWithLabelsAndReviewers(t *testing.T) {
	db := testutil.OpenTestDB(t)
	reposRoot := t.TempDir()
	api := newAPIRouterAt(t, db, reposRoot)
	r := seedRaceRepo(t, db, reposRoot)
	labelID, reviewer, writer := seedNewPRMetadata(t, db, r, "writer")

	labels, reviewRequests := openPRWithMetadata(t, db, api, r, writer, labelID, reviewer.name)
	if labels != 1 || reviewRequests != 1 {
		t.Errorf("writer attached %d labels and %d review requests, want 1 and 1", labels, reviewRequests)
	}
}

func TestPageNewPull_ReviewerPickerOnlyForWriters(t *testing.T) {
	db := testutil.OpenTestDB(t)
	reposRoot := t.TempDir()
	api := newAPIRouterAt(t, db, reposRoot)
	r := seedRaceRepo(t, db, reposRoot)
	_, _, reader := seedNewPRMetadata(t, db, r, "reader")

	page := requestAPI(api, http.MethodGet, r.path+"/pulls/new?head=feature", reader.token).Body.String()
	if strings.Contains(page, `name="reviewers"`) {
		t.Error("reader is offered the reviewer picker")
	}

	page = requestAPI(api, http.MethodGet, r.path+"/pulls/new?head=feature", r.owner.token).Body.String()
	assertContains(t, page, `name="reviewers"`)
}
