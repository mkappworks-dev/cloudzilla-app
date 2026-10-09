package handler_test

// Integration tests for the repo Checks page. All tests require TEST_DATABASE_DSN and skip otherwise.

import (
	"net/http"
	"path/filepath"
	"strings"
	"testing"

	gogit "github.com/go-git/go-git/v5"

	"github.com/mkappworks-dev/cloudzilla-app/internal/config"
	"github.com/mkappworks-dev/cloudzilla-app/internal/service"
	"github.com/mkappworks-dev/cloudzilla-app/internal/testutil"
)

type checksFixture struct {
	app     http.Handler
	repo    seededRepo
	headSHA string
}

func seedChecksFixture(t *testing.T, private bool) checksFixture {
	t.Helper()
	db := testutil.OpenTestDB(t)
	reposRoot := t.TempDir()
	repo := seedOwnedRepo(t, db, private)
	if _, err := gogit.PlainInit(filepath.Join(reposRoot, repo.owner.name, repo.name+".git"), true); err != nil {
		t.Fatalf("init bare repo: %v", err)
	}
	code := service.NewCodeService(config.GitConfig{ReposRoot: reposRoot})
	if _, err := code.CommitFile(repo.owner.name, repo.name, "main", "a.txt", []byte("x\n"), raceAuthor, "Add a.txt"); err != nil {
		t.Fatalf("commit a.txt: %v", err)
	}
	head, _, err := code.ResolveRef(repo.owner.name, repo.name, "main")
	if err != nil {
		t.Fatalf("resolve main: %v", err)
	}
	return checksFixture{app: newAPIRouterAt(t, db, reposRoot), repo: repo, headSHA: head.Hash.String()}
}

func (f checksFixture) postStatus(t *testing.T, sha, body string) {
	t.Helper()
	rr := requestAPIBody(f.app, http.MethodPost, "/api/repos"+f.repo.path+"/statuses/"+sha, f.repo.owner.token, body)
	if rr.Code != http.StatusCreated && rr.Code != http.StatusOK {
		t.Fatalf("post status: %d %s", rr.Code, rr.Body.String())
	}
}

func TestPageChecks_RendersOneCardPerCommit(t *testing.T) {
	f := seedChecksFixture(t, false)
	unknownSHA := strings.Repeat("cd", 20)
	f.postStatus(t, f.headSHA, `{"state":"success","context":"ci/build","description":"Build passed","target_url":"https://ci.example.com/runs/1"}`)
	f.postStatus(t, f.headSHA, `{"state":"failure","context":"ci/test","description":"2 tests <b>failed</b>"}`)
	f.postStatus(t, unknownSHA, `{"state":"pending","context":"ci/build"}`)

	rr := requestAPI(f.app, http.MethodGet, f.repo.path+"/checks", "")
	if rr.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", rr.Code)
	}
	body := rr.Body.String()
	for _, want := range []string{
		"Add a.txt",
		"1 of 2 passed",
		`href="` + f.repo.path + "/commit/" + f.headSHA + `"`,
		f.headSHA[:7],
		"ci/build", "ci/test", "Build passed", "2 tests &lt;b&gt;failed&lt;/b&gt;",
		`href="https://ci.example.com/runs/1"`,
		unknownSHA,
		"0 of 1 passed",
	} {
		assertContains(t, body, want)
	}
	if strings.Index(body, unknownSHA) > strings.Index(body, "Add a.txt") {
		t.Error("the most recently updated commit must come first")
	}
	if strings.Contains(body, "Actions are coming soon") || strings.Contains(body, "Go test") {
		t.Error("the Actions mockup must not render")
	}
}

func TestPageChecks_EmptyStateShowsCurlExample(t *testing.T) {
	f := seedChecksFixture(t, false)

	rr := requestAPI(f.app, http.MethodGet, f.repo.path+"/checks", "")
	if rr.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", rr.Code)
	}
	body := rr.Body.String()
	assertContains(t, body, "No checks reported yet")
	assertContains(t, body, "http://localhost:8080/api/repos"+f.repo.path+"/statuses/")
}

func TestPageChecks_Paginates(t *testing.T) {
	f := seedChecksFixture(t, false)
	for i := 0; i < 21; i++ {
		f.postStatus(t, strings.Repeat(string(rune('a'+i)), 40)[:40], `{"state":"success"}`)
	}

	first := requestAPI(f.app, http.MethodGet, f.repo.path+"/checks", "").Body.String()
	assertContains(t, first, `href="`+f.repo.path+`/checks?page=2"`)

	second := requestAPI(f.app, http.MethodGet, f.repo.path+"/checks?page=2", "").Body.String()
	assertContains(t, second, `href="`+f.repo.path+`/checks?page=1"`)
	if strings.Contains(second, `/checks?page=3"`) {
		t.Error("the last page must not link to an older page")
	}
}

func TestPageChecks_PagePastTheEndLinksBack(t *testing.T) {
	f := seedChecksFixture(t, false)
	f.postStatus(t, f.headSHA, `{"state":"success"}`)

	body := requestAPI(f.app, http.MethodGet, f.repo.path+"/checks?page=3", "").Body.String()
	assertContains(t, body, "No more checks.")
	assertContains(t, body, `href="`+f.repo.path+`/checks?page=2"`)
	if strings.Contains(body, "No checks reported yet") {
		t.Error("a page past the end must not claim the repo has no checks")
	}
}

func TestPageChecks_SubnavTabIsActive(t *testing.T) {
	f := seedChecksFixture(t, false)

	body := requestAPI(f.app, http.MethodGet, f.repo.path+"/checks", "").Body.String()
	assertContains(t, body, `href="`+f.repo.path+`/checks" aria-current="page"`)
}

func TestPageActions_IsGone(t *testing.T) {
	f := seedChecksFixture(t, false)

	rr := requestAPI(f.app, http.MethodGet, f.repo.path+"/actions", f.repo.owner.token)
	if rr.Code != http.StatusNotFound {
		t.Errorf("want 404, got %d", rr.Code)
	}
}
