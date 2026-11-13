package handler_test

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/mkappworks-dev/cloudzilla-app/internal/config"
	"github.com/mkappworks-dev/cloudzilla-app/internal/service"
	"github.com/mkappworks-dev/cloudzilla-app/internal/testutil"
)

type forkedRace struct {
	raceRepo
	forker    signedInUser
	forkName  string
	forkRepo  string // "owner/name"
	forkPulls string // the fork's own path
}

// forkWithBranch forks a seeded repo as a new user and gives the fork a fix-x branch with one commit of its own.
func forkWithBranch(t *testing.T, db *sql.DB, api http.Handler, reposRoot string) forkedRace {
	t.Helper()
	r := seedRaceRepo(t, db, reposRoot)
	f := forkedRace{raceRepo: r, forker: seedSignedInUser(t, db)}
	rr := requestAPIBody(api, http.MethodPost, "/api/repos"+r.path+"/fork", f.forker.token, "{}")
	if rr.Code != http.StatusCreated {
		t.Fatalf("fork: %d %s", rr.Code, rr.Body.String())
	}
	var fork struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &fork); err != nil || fork.Name == "" {
		t.Fatalf("fork response %q: %v", rr.Body.String(), err)
	}
	f.forkName = fork.Name
	f.forkRepo = f.forker.name + "/" + fork.Name
	f.forkPulls = "/" + f.forkRepo
	code := service.NewCodeService(config.GitConfig{ReposRoot: reposRoot})
	if err := code.CreateBranch(f.forker.name, fork.Name, "fix-x", "main"); err != nil {
		t.Fatal(err)
	}
	if _, err := code.CommitFile(f.forker.name, fork.Name, "fix-x", "x.txt", []byte("x\n"), raceAuthor, "Add x"); err != nil {
		t.Fatal(err)
	}
	return f
}

func TestPageNewPull_OffersTheViewersForkAsHead(t *testing.T) {
	db := testutil.OpenTestDB(t)
	reposRoot := t.TempDir()
	api := newAPIRouterAt(t, db, reposRoot)
	f := forkWithBranch(t, db, api, reposRoot)

	page := requestAPI(api, http.MethodGet, f.path+"/pulls/new", f.forker.token).Body.String()

	assertContains(t, page, `name="head_repo"`)
	assertContains(t, page, `<option value="`+f.forkRepo+`" selected>`)
	assertContains(t, page, `<option value="fix-x"`)
}

func TestPageNewPull_ListsOnlyReposTheViewerCanWrite(t *testing.T) {
	db := testutil.OpenTestDB(t)
	reposRoot := t.TempDir()
	api := newAPIRouterAt(t, db, reposRoot)
	f := forkWithBranch(t, db, api, reposRoot)
	stranger := seedSignedInUser(t, db)

	page := requestAPI(api, http.MethodGet, f.path+"/pulls/new", stranger.token).Body.String()

	if strings.Contains(page, f.forkRepo) {
		t.Errorf("a user with no write access to the fork is offered %s as a head", f.forkRepo)
	}
}

func TestPageNewPullSubmit_OpensAPullFromTheFork(t *testing.T) {
	db := testutil.OpenTestDB(t)
	reposRoot := t.TempDir()
	api := newAPIRouterAt(t, db, reposRoot)
	f := forkWithBranch(t, db, api, reposRoot)

	rr := postForm(t, api, f.forker.token, f.path+"/pulls/new", url.Values{
		"title":       {"Add x"},
		"head_repo":   {f.forkRepo},
		"head_branch": {"fix-x"},
		"base_branch": {"main"},
	})

	if rr.Code != http.StatusSeeOther {
		t.Fatalf("want 303, got %d: %s", rr.Code, rr.Body.String())
	}
	if n := countRows(t, db, `SELECT COUNT(*) FROM pull_requests p JOIN repositories h ON h.id = p.head_repo_id
		WHERE p.repo_id = $1 AND h.owner_name = $2 AND h.name = $3 AND p.head_branch = 'fix-x'`, f.id, f.forker.name, f.forkName); n != 1 {
		t.Errorf("want one pull request from the fork, got %d", n)
	}
}

func openForkPullViaAPI(t *testing.T, api http.Handler, f forkedRace) {
	t.Helper()
	rr := requestAPIBody(api, http.MethodPost, "/api/repos"+f.path+"/pulls", f.forker.token,
		`{"title":"Add x","head_branch":"fix-x","base_branch":"main","head_repo":"`+f.forkRepo+`"}`)
	if rr.Code != http.StatusCreated {
		t.Fatalf("open pull: %d %s", rr.Code, rr.Body.String())
	}
}

func TestPagePullDetail_NamesTheForkHead(t *testing.T) {
	db := testutil.OpenTestDB(t)
	reposRoot := t.TempDir()
	api := newAPIRouterAt(t, db, reposRoot)
	f := forkWithBranch(t, db, api, reposRoot)
	openForkPullViaAPI(t, api, f)

	page := requestAPI(api, http.MethodGet, f.path+"/pulls/1", f.owner.token).Body.String()

	assertContains(t, page, f.forkRepo+":fix-x")
	if strings.Contains(page, "git checkout fix-x") {
		t.Error("the check-out instructions name a branch that isn't in this repository")
	}
}

func TestPagePullDetail_PrivateForkIsNamedOnlyAsAFork(t *testing.T) {
	db := testutil.OpenTestDB(t)
	reposRoot := t.TempDir()
	api := newAPIRouterAt(t, db, reposRoot)
	f := forkWithBranch(t, db, api, reposRoot)
	openForkPullViaAPI(t, api, f)
	testutil.Exec(t, db, `UPDATE repositories SET private = true WHERE owner_name = $1 AND name = $2`, f.forker.name, f.forkName)

	page := requestAPI(api, http.MethodGet, f.path+"/pulls/1", f.owner.token).Body.String()

	if strings.Contains(page, f.forkRepo) {
		t.Errorf("the page names the private fork %s to a reader of the base only", f.forkRepo)
	}
	assertContains(t, page, "a fork")
}
