package handler_test

import (
	"database/sql"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strconv"
	"testing"

	gogit "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"

	"github.com/mkappworks-dev/cloudzilla-app/internal/config"
	"github.com/mkappworks-dev/cloudzilla-app/internal/service"
	"github.com/mkappworks-dev/cloudzilla-app/internal/testutil"
)

const branchMovedMsg = "branch was updated while saving; reload and try again"

var raceAuthor = service.GitAuthor{Name: "Tester", Email: "tester@example.com"}

// raceRepo is a seeded repo whose bare git repo has main and feature (branched
// from main's first commit). mainPushed and featurePushed are commits on top of
// each tip that no branch points at yet: the pushes a test lands mid-request.
type raceRepo struct {
	seededRepo
	gitDir                    string
	git                       *gogit.Repository
	mainTip, mainPushed       plumbing.Hash
	featureTip, featurePushed plumbing.Hash
}

func seedRaceRepo(t *testing.T, db *sql.DB, reposRoot string) raceRepo {
	t.Helper()
	r := raceRepo{seededRepo: seedOwnedRepo(t, db, false)}
	r.gitDir = filepath.Join(reposRoot, r.owner.name, r.name+".git")
	git, err := gogit.PlainInit(r.gitDir, true)
	if err != nil {
		t.Fatalf("init bare repo: %v", err)
	}
	r.git = git

	code := service.NewCodeService(config.GitConfig{ReposRoot: reposRoot})
	commit := func(branch, path string) {
		t.Helper()
		if err := code.CommitFile(r.owner.name, r.name, branch, path, []byte("one\ntwo\n"), raceAuthor, "Add "+path); err != nil {
			t.Fatalf("commit %s on %s: %v", path, branch, err)
		}
	}
	commit("main", "a.txt")
	if err := code.CreateBranch(r.owner.name, r.name, "feature", "main"); err != nil {
		t.Fatalf("create feature: %v", err)
	}
	commit("feature", "f.txt")
	r.featureTip = branchHash(t, git, "feature")
	r.featurePushed = pushedOnto(t, git, "feature", func() { commit("feature", "pushed.txt") })
	commit("main", "m.txt")
	r.mainTip = branchHash(t, git, "main")
	r.mainPushed = pushedOnto(t, git, "main", func() { commit("main", "pushed.txt") })
	return r
}

// pushedOnto runs commit, which must advance branch by one commit, then puts
// branch back where it was and returns the new commit.
func pushedOnto(t *testing.T, git *gogit.Repository, branch string, commit func()) plumbing.Hash {
	t.Helper()
	tip := branchHash(t, git, branch)
	commit()
	pushed := branchHash(t, git, branch)
	ref := plumbing.NewHashReference(plumbing.NewBranchReferenceName(branch), tip)
	if err := git.Storer.SetReference(ref); err != nil {
		t.Fatalf("reset %s: %v", branch, err)
	}
	return pushed
}

func branchHash(t *testing.T, git *gogit.Repository, branch string) plumbing.Hash {
	t.Helper()
	ref, err := git.Reference(plumbing.NewBranchReferenceName(branch), true)
	if err != nil {
		t.Fatalf("resolve %s: %v", branch, err)
	}
	return ref.Hash()
}

func assertPushKept(t *testing.T, rr *httptest.ResponseRecorder, wantBody string, git *gogit.Repository, branch string, pushed plumbing.Hash) {
	t.Helper()
	if rr.Code != http.StatusConflict || rr.Body.String() != wantBody {
		t.Errorf("want 409 %q, got %d %q", wantBody, rr.Code, rr.Body.String())
	}
	if got := branchHash(t, git, branch); got != pushed {
		t.Errorf("%s = %s, want pushed commit %s", branch, got, pushed)
	}
}

func TestApplySuggestion_PushLandsMidCommit_Conflict(t *testing.T) {
	db := testutil.OpenTestDB(t)
	reposRoot := t.TempDir()
	api := newAPIRouterAt(db, reposRoot)
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

	var rr *httptest.ResponseRecorder
	path := "/api/repos" + r.path + "/pulls/1/line_comments/" + strconv.FormatInt(suggestionID, 10) + "/apply"
	testutil.PushDuringCommit(t, r.gitDir, plumbing.NewBranchReferenceName("feature"), r.featureTip, r.featurePushed,
		func() { rr = requestAPI(api, http.MethodPost, path, r.owner.token) })

	assertPushKept(t, rr, `{"error":"`+branchMovedMsg+`"}`+"\n", r.git, "feature", r.featurePushed)
}

func TestMergePull_PushLandsMidMerge_ConflictAndStaysOpen(t *testing.T) {
	db := testutil.OpenTestDB(t)
	reposRoot := t.TempDir()
	api := newAPIRouterAt(db, reposRoot)
	r := seedRaceRepo(t, db, reposRoot)
	pullID := seedOpenPull(t, db, r.seededRepo)

	var rr *httptest.ResponseRecorder
	testutil.PushDuringCommit(t, r.gitDir, plumbing.NewBranchReferenceName("main"), r.mainTip, r.mainPushed,
		func() {
			rr = requestAPIBody(api, http.MethodPatch, "/api/repos"+r.path+"/pulls/1", r.owner.token,
				`{"state":"merged","merge_strategy":"squash"}`)
		})

	assertPushKept(t, rr, `{"error":"`+branchMovedMsg+`"}`+"\n", r.git, "main", r.mainPushed)
	var state string
	if err := db.QueryRow(`SELECT state FROM pull_requests WHERE id = $1`, pullID).Scan(&state); err != nil {
		t.Fatalf("read pull state: %v", err)
	}
	if state != "open" {
		t.Errorf("pull state = %q, want open", state)
	}
}

func TestSubmitNewFile_PushLandsMidCommit_Conflict(t *testing.T) {
	db := testutil.OpenTestDB(t)
	reposRoot := t.TempDir()
	api := newAPIRouterAt(db, reposRoot)
	r := seedRaceRepo(t, db, reposRoot)

	var rr *httptest.ResponseRecorder
	testutil.PushDuringCommit(t, r.gitDir, plumbing.NewBranchReferenceName("main"), r.mainTip, r.mainPushed,
		func() {
			rr = postForm(t, api, r.owner.token, r.path+"/new/main", url.Values{"path": {"web.txt"}, "content": {"web\n"}})
		})

	assertPushKept(t, rr, branchMovedMsg+"\n", r.git, "main", r.mainPushed)
}

func TestWikiEdits_PushLandsMidCommit_Conflict(t *testing.T) {
	tests := []struct {
		name string
		send func(t *testing.T, api http.Handler, repo seededRepo) *httptest.ResponseRecorder
	}{
		{"save", func(t *testing.T, api http.Handler, repo seededRepo) *httptest.ResponseRecorder {
			return postForm(t, api, repo.owner.token, "/api/repos"+repo.path+"/wiki/web", url.Values{"content": {"# web"}})
		}},
		{"rename", func(t *testing.T, api http.Handler, repo seededRepo) *httptest.ResponseRecorder {
			return postForm(t, api, repo.owner.token, "/api/repos"+repo.path+"/wiki/home",
				url.Values{"content": {"# home"}, "new_slug": {"start"}})
		}},
		{"reorder", func(t *testing.T, api http.Handler, repo seededRepo) *httptest.ResponseRecorder {
			return postForm(t, api, repo.owner.token, "/api/repos"+repo.path+"/wiki/order", url.Values{"order": {"pushed,home"}})
		}},
		{"delete", func(t *testing.T, api http.Handler, repo seededRepo) *httptest.ResponseRecorder {
			return requestAPI(api, http.MethodDelete, "/api/repos"+repo.path+"/wiki/home", repo.owner.token)
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db := testutil.OpenTestDB(t)
			reposRoot := t.TempDir()
			api := newAPIRouterAt(db, reposRoot)
			repo := seedOwnedRepo(t, db, false)
			code := service.NewCodeService(config.GitConfig{ReposRoot: reposRoot})
			save := func(slug string) {
				t.Helper()
				if err := code.WikiPageSave(repo.owner.name, repo.name, slug, "# "+slug, raceAuthor, ""); err != nil {
					t.Fatalf("save wiki page %s: %v", slug, err)
				}
			}
			save("home")
			wikiDir := filepath.Join(reposRoot, repo.owner.name, repo.name+".wiki.git")
			wiki, err := gogit.PlainOpen(wikiDir)
			if err != nil {
				t.Fatalf("open wiki: %v", err)
			}
			tip := branchHash(t, wiki, "main")
			pushed := pushedOnto(t, wiki, "main", func() { save("pushed") })

			var rr *httptest.ResponseRecorder
			testutil.PushDuringCommit(t, wikiDir, plumbing.NewBranchReferenceName("main"), tip, pushed,
				func() { rr = tt.send(t, api, repo) })

			assertPushKept(t, rr, `{"error":"`+branchMovedMsg+`"}`+"\n", wiki, "main", pushed)
		})
	}
}
