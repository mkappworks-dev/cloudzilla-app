package handler_test

import (
	"database/sql"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/mkappworks-dev/cloudzilla-app/internal/testutil"
)

// A left-side line numbers the base file, so applying it would edit an unrelated head line.
func TestApplySuggestion_LeftSide_Unprocessable(t *testing.T) {
	db := testutil.OpenTestDB(t)
	reposRoot := t.TempDir()
	api := newAPIRouterAt(t, db, reposRoot)
	r := seedRaceRepo(t, db, reposRoot)
	var suggestionID int64
	err := db.QueryRow(
		`INSERT INTO pull_line_comments (pull_id, repo_id, author_id, author_name, path, diff_side, line, body, is_suggestion, suggestion_body)
		 VALUES ($1, $2, $3, $4, 'a.txt', 'left', 1, 'try this', true, 'uno') RETURNING id`,
		seedOpenPull(t, db, r.seededRepo), r.id, r.owner.id, r.owner.name,
	).Scan(&suggestionID)
	if err != nil {
		t.Fatalf("seed suggestion: %v", err)
	}

	path := "/api/repos" + r.path + "/pulls/1/line_comments/" + strconv.FormatInt(suggestionID, 10) + "/apply"
	rr := requestAPI(api, http.MethodPost, path, r.owner.token)

	if want := `{"error":"only right-side suggestions can be applied"}` + "\n"; rr.Code != http.StatusUnprocessableEntity || rr.Body.String() != want {
		t.Errorf("want 422 %s, got %d %s", want, rr.Code, rr.Body.String())
	}
	if got := branchHash(t, r.git, "feature"); got != r.featureTip {
		t.Errorf("feature = %s, want it still at %s", got, r.featureTip)
	}
}

func TestCreateLineComment_DiffSideMustBeLeftOrRight(t *testing.T) {
	db := testutil.OpenTestDB(t)
	api := newAPIRouter(t, db)
	repo := seedOwnedRepo(t, db, false)
	seedOpenPull(t, db, repo)
	create := func(diffSide string) *httptest.ResponseRecorder {
		return requestAPIBody(api, http.MethodPost, "/api/repos"+repo.path+"/pulls/1/line_comments", repo.owner.token,
			`{"path":"a.txt","line":1,"diff_side":"`+diffSide+`","body":"note"}`)
	}

	if rr := create("left"); rr.Code != http.StatusCreated {
		t.Errorf("left: want 201, got %d %s", rr.Code, rr.Body.String())
	}
	rr := create("middle")
	if want := `{"error":"diff_side must be left or right"}` + "\n"; rr.Code != http.StatusUnprocessableEntity || rr.Body.String() != want {
		t.Errorf("middle: want 422 %s, got %d %s", want, rr.Code, rr.Body.String())
	}
}

func TestApplySuggestion_RecordsWhoAppliedItAndRefusesASecondApply(t *testing.T) {
	db := testutil.OpenTestDB(t)
	reposRoot := t.TempDir()
	api := newAPIRouterAt(t, db, reposRoot)
	r := seedRaceRepo(t, db, reposRoot)
	var suggestionID int64
	err := db.QueryRow(
		`INSERT INTO pull_line_comments (pull_id, repo_id, author_id, author_name, path, diff_side, line, body, is_suggestion, suggestion_body)
		 VALUES ($1, $2, $3, $4, 'f.txt', 'right', 1, 'try this', true, 'uno') RETURNING id`,
		seedOpenPull(t, db, r.seededRepo), r.id, r.owner.id, r.owner.name,
	).Scan(&suggestionID)
	if err != nil {
		t.Fatalf("seed suggestion: %v", err)
	}
	path := "/api/repos" + r.path + "/pulls/1/line_comments/" + strconv.FormatInt(suggestionID, 10) + "/apply"

	if rr := requestAPI(api, http.MethodPost, path, r.owner.token); rr.Code != http.StatusNoContent {
		t.Fatalf("apply: want 204, got %d %s", rr.Code, rr.Body.String())
	}
	var sha, by string
	var at sql.NullTime
	if err := db.QueryRow(`SELECT applied_at, applied_sha, applied_by FROM pull_line_comments WHERE id=$1`, suggestionID).Scan(&at, &sha, &by); err != nil {
		t.Fatalf("read applied columns: %v", err)
	}
	if want := branchHash(t, r.git, "feature").String(); !at.Valid || sha != want || by != r.owner.name {
		t.Errorf("applied = (%v, %q, %q), want a time, the new feature tip %q and %q", at.Valid, sha, by, want, r.owner.name)
	}
	if rr := requestAPI(api, http.MethodPost, path, r.owner.token); rr.Code != http.StatusConflict {
		t.Errorf("second apply: want 409, got %d %s", rr.Code, rr.Body.String())
	}
}

func TestApplySuggestionBatch_CommitsOnceAndMarksEveryCommentApplied(t *testing.T) {
	db := testutil.OpenTestDB(t)
	reposRoot := t.TempDir()
	api := newAPIRouterAt(t, db, reposRoot)
	r := seedRaceRepo(t, db, reposRoot)
	pullID := seedOpenPull(t, db, r.seededRepo)
	seed := func(path string, line int, text string) int64 {
		var id int64
		err := db.QueryRow(
			`INSERT INTO pull_line_comments (pull_id, repo_id, author_id, author_name, path, diff_side, line, body, is_suggestion, suggestion_body)
			 VALUES ($1, $2, $3, $4, $5, 'right', $6, 'try this', true, $7) RETURNING id`,
			pullID, r.id, r.owner.id, r.owner.name, path, line, text,
		).Scan(&id)
		if err != nil {
			t.Fatalf("seed suggestion: %v", err)
		}
		return id
	}
	first, second := seed("f.txt", 1, "uno"), seed("f.txt", 2, "dos")
	url := "/api/repos" + r.path + "/pulls/1/suggestions/apply"

	rr := requestAPIBody(api, http.MethodPost, url, r.owner.token, `{"ids":[`+strconv.FormatInt(first, 10)+`,`+strconv.FormatInt(second, 10)+`]}`)
	if rr.Code != http.StatusNoContent {
		t.Fatalf("batch: want 204, got %d %s", rr.Code, rr.Body.String())
	}
	tip := branchHash(t, r.git, "feature")
	commit, err := r.git.CommitObject(tip)
	if err != nil || len(commit.ParentHashes) != 1 || commit.ParentHashes[0] != r.featureTip {
		t.Fatalf("want one commit on top of %s, got %v (%v)", r.featureTip, commit, err)
	}
	var applied int
	if err := db.QueryRow(`SELECT count(*) FROM pull_line_comments WHERE id IN ($1, $2) AND applied_sha = $3`, first, second, tip.String()).Scan(&applied); err != nil || applied != 2 {
		t.Errorf("want both comments marked applied in %s, got %d (%v)", tip, applied, err)
	}

	if rr := requestAPIBody(api, http.MethodPost, url, r.owner.token, `{"ids":[`+strconv.FormatInt(first, 10)+`]}`); rr.Code != http.StatusConflict {
		t.Errorf("re-applying: want 409, got %d %s", rr.Code, rr.Body.String())
	}
}

func TestApplySuggestionBatch_OverlapChangesNothing(t *testing.T) {
	db := testutil.OpenTestDB(t)
	reposRoot := t.TempDir()
	api := newAPIRouterAt(t, db, reposRoot)
	r := seedRaceRepo(t, db, reposRoot)
	pullID := seedOpenPull(t, db, r.seededRepo)
	var ids [2]int64
	for i, text := range []string{"uno", "eins"} {
		err := db.QueryRow(
			`INSERT INTO pull_line_comments (pull_id, repo_id, author_id, author_name, path, diff_side, line, body, is_suggestion, suggestion_body)
			 VALUES ($1, $2, $3, $4, 'f.txt', 'right', 1, 'try this', true, $5) RETURNING id`,
			pullID, r.id, r.owner.id, r.owner.name, text,
		).Scan(&ids[i])
		if err != nil {
			t.Fatalf("seed suggestion: %v", err)
		}
	}
	rr := requestAPIBody(api, http.MethodPost, "/api/repos"+r.path+"/pulls/1/suggestions/apply", r.owner.token,
		`{"ids":[`+strconv.FormatInt(ids[0], 10)+`,`+strconv.FormatInt(ids[1], 10)+`]}`)
	if rr.Code != http.StatusUnprocessableEntity {
		t.Errorf("want 422, got %d %s", rr.Code, rr.Body.String())
	}
	if got := branchHash(t, r.git, "feature"); got != r.featureTip {
		t.Errorf("feature = %s, want it left at %s", got, r.featureTip)
	}
}
