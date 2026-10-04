package handler_test

import (
	"database/sql"
	"net/http"
	"testing"
	"time"

	"github.com/go-git/go-git/v5/plumbing"

	"github.com/mkappworks-dev/cloudzilla-app/internal/testutil"
)

func requireCIOnMain(t *testing.T, db *sql.DB, repoID int64) {
	t.Helper()
	testutil.Exec(t, db, `INSERT INTO branch_protections (repo_id, pattern, require_status_checks) VALUES ($1, 'main', '{ci}')`, repoID)
}

func postStatus(t *testing.T, api http.Handler, r raceRepo, sha plumbing.Hash, state string) {
	t.Helper()
	rr := requestAPIBody(api, http.MethodPost, "/api/repos"+r.path+"/statuses/"+sha.String(), r.owner.token,
		`{"state":"`+state+`","context":"ci"}`)
	if rr.Code != http.StatusCreated {
		t.Fatalf("post status: want 201, got %d %s", rr.Code, rr.Body.String())
	}
}

func pullState(t *testing.T, db *sql.DB, pullID int64) string {
	t.Helper()
	var state string
	if err := db.QueryRow(`SELECT state FROM pull_requests WHERE id = $1`, pullID).Scan(&state); err != nil {
		t.Fatalf("read pull state: %v", err)
	}
	return state
}

func TestMergePull_RequiredCheckOnHeadCommit(t *testing.T) {
	tests := []struct {
		name      string
		passingOn func(raceRepo) plumbing.Hash
		wantCode  int
		wantState string
	}{
		{"passed on the head commit", func(r raceRepo) plumbing.Hash { return r.featureTip }, http.StatusOK, "merged"},
		// CheckMerge skips status checks for an empty head SHA; this case catches
		// a fix that drops the SHA instead of correcting it.
		{"passed on another commit", func(r raceRepo) plumbing.Hash { return r.mainTip }, http.StatusUnprocessableEntity, "open"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			db := testutil.OpenTestDB(t)
			reposRoot := t.TempDir()
			api := newAPIRouterAt(t, db, reposRoot)
			r := seedRaceRepo(t, db, reposRoot)
			pullID := seedOpenPull(t, db, r.seededRepo)
			requireCIOnMain(t, db, r.id)
			postStatus(t, api, r, tc.passingOn(r), "success")

			rr := requestAPIBody(api, http.MethodPatch, "/api/repos"+r.path+"/pulls/1", r.owner.token,
				`{"state":"merged","merge_strategy":"squash"}`)

			if rr.Code != tc.wantCode {
				t.Errorf("merge: want %d, got %d %s", tc.wantCode, rr.Code, rr.Body.String())
			}
			if got := pullState(t, db, pullID); got != tc.wantState {
				t.Errorf("pull state = %q, want %q", got, tc.wantState)
			}
		})
	}
}

func TestCreateStatus_PassingRequiredCheckOnHead_AutoMerges(t *testing.T) {
	db := testutil.OpenTestDB(t)
	reposRoot := t.TempDir()
	api := newAPIRouterAt(t, db, reposRoot)
	r := seedRaceRepo(t, db, reposRoot)
	pullID := seedOpenPull(t, db, r.seededRepo)
	requireCIOnMain(t, db, r.id)
	rr := requestAPIBody(api, http.MethodPatch, "/api/repos"+r.path+"/pulls/1", r.owner.token,
		`{"auto_merge":"enable","auto_merge_strategy":"squash"}`)
	if rr.Code != http.StatusOK {
		t.Fatalf("enable auto-merge: want 200, got %d %s", rr.Code, rr.Body.String())
	}

	postStatus(t, api, r, r.featureTip, "success")

	// CreateStatus merges in a goroutine after it responds.
	deadline := time.Now().Add(5 * time.Second)
	for pullState(t, db, pullID) != "merged" {
		if time.Now().After(deadline) {
			t.Fatalf("pull still %q 5s after its head commit passed the required check", pullState(t, db, pullID))
		}
		time.Sleep(20 * time.Millisecond)
	}
}
