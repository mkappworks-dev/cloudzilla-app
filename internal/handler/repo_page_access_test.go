package handler_test

// Integration tests: repo pages must not reveal a private repo to a viewer who
// cannot read it. All tests require TEST_DATABASE_DSN and skip otherwise.

import (
	"database/sql"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	gogit "github.com/go-git/go-git/v5"

	"github.com/mkappworks-dev/cloudzilla-app/internal/config"
	"github.com/mkappworks-dev/cloudzilla-app/internal/service"
	"github.com/mkappworks-dev/cloudzilla-app/internal/testutil"
)

type repoPageCase struct{ method, page string }

var newIssueAndPullPageCases = []repoPageCase{
	{http.MethodGet, "/issues/new"},
	{http.MethodPost, "/issues/new"},
	{http.MethodGet, "/pulls/new"},
	{http.MethodPost, "/pulls/new"},
}

// Pages that need more than read access.
var gatedRepoPageCases = []repoPageCase{
	{http.MethodGet, "/discussions/new"},
	{http.MethodPost, "/discussions/new"},
	{http.MethodGet, "/milestones/new"},
	{http.MethodPost, "/milestones/new"},
	{http.MethodPost, "/milestones/1"},
	{http.MethodGet, "/releases/new"},
	{http.MethodGet, "/new/main"},
	{http.MethodPost, "/new/main"},
	{http.MethodGet, "/settings"},
	{http.MethodPost, "/settings/general"},
	{http.MethodPost, "/settings/features"},
	{http.MethodPost, "/settings/visibility"},
	{http.MethodGet, "/wiki/new"},
	{http.MethodGet, "/wiki/Home/edit"},
}

// A 403 for a private repo against a 404 for a missing one would confirm that
// the private repo exists, so the two responses must be identical.
func TestRepoPages_PrivateRepoNonReader_LooksLikeMissingRepo(t *testing.T) {
	for _, tc := range append(newIssueAndPullPageCases, gatedRepoPageCases...) {
		t.Run(tc.method+tc.page, func(t *testing.T) {
			db := testutil.OpenTestDB(t)
			repo := seedOwnedRepo(t, db, true)
			token := seedSignedInUser(t, db).token
			missingPath := "/nobody_" + testutil.UniqueSuffix(t) + "/norepo"

			private := requestPage(t, db, tc.method, repo.path+tc.page, token)
			missing := requestPage(t, db, tc.method, missingPath+tc.page, token)

			if private.Code != http.StatusNotFound {
				t.Errorf("want 404, got %d", private.Code)
			}
			if private.Code != missing.Code || private.Body.String() != missing.Body.String() {
				t.Errorf("private repo response (%d) must match a missing repo's (%d)", private.Code, missing.Code)
			}
		})
	}
}

func TestNewIssueAndPullPages_PrivateRepoOwner_RendersForm(t *testing.T) {
	for _, tc := range newIssueAndPullPageCases {
		t.Run(tc.method+tc.page, func(t *testing.T) {
			db := testutil.OpenTestDB(t)
			repo := seedOwnedRepo(t, db, true)

			rr := requestPage(t, db, tc.method, repo.path+tc.page, repo.owner.token)

			if rr.Code != http.StatusOK {
				t.Errorf("want 200, got %d", rr.Code)
			}
			assertContains(t, rr.Body.String(), repo.name)
		})
	}
}

func TestGatedRepoPages_PublicRepoNonWriter_Forbidden(t *testing.T) {
	for _, tc := range gatedRepoPageCases {
		t.Run(tc.method+tc.page, func(t *testing.T) {
			db := testutil.OpenTestDB(t)
			repo := seedOwnedRepo(t, db, false)

			rr := requestPage(t, db, tc.method, repo.path+tc.page, seedSignedInUser(t, db).token)

			if rr.Code != http.StatusForbidden {
				t.Errorf("want 403, got %d", rr.Code)
			}
		})
	}
}

// privateRepoFixture is a private repo with a.txt committed on main and one of
// each sub-resource a page route can name, so a handler that loaded one before
// the read check would answer differently from a missing repo.
type privateRepoFixture struct {
	app    http.Handler
	repo   seededRepo
	params *strings.Replacer
}

func seedPrivateRepoFixture(t *testing.T, db *sql.DB) privateRepoFixture {
	t.Helper()
	reposRoot := t.TempDir()
	repo := seedOwnedRepo(t, db, true)
	git, err := gogit.PlainInit(filepath.Join(reposRoot, repo.owner.name, repo.name+".git"), true)
	if err != nil {
		t.Fatalf("init bare repo: %v", err)
	}
	code := service.NewCodeService(config.GitConfig{ReposRoot: reposRoot})
	if err := code.CommitFile(repo.owner.name, repo.name, "main", "a.txt", []byte("x\n"), raceAuthor, "Add a.txt"); err != nil {
		t.Fatalf("commit a.txt: %v", err)
	}

	testutil.Exec(t, db, `INSERT INTO issues (repo_id, number, author_id, title) VALUES ($1, 1, $2, 'issue')`, repo.id, repo.owner.id)
	seedOpenPull(t, db, repo)
	testutil.Exec(t, db, `INSERT INTO milestones (repo_id, number, title) VALUES ($1, 1, 'v1')`, repo.id)
	testutil.Exec(t, db, `INSERT INTO releases (repo_id, tag_name, author_id) VALUES ($1, '1', $2)`, repo.id, repo.owner.id)
	testutil.Exec(t, db,
		`INSERT INTO discussions (repo_id, category_id, number, title, author_id, author_name)
		 VALUES ($1, (SELECT MIN(id) FROM discussion_categories), 1, 'idea', $2, $3)`,
		repo.id, repo.owner.id, repo.owner.name)
	var projectID int64
	if err := db.QueryRow(`INSERT INTO projects (repo_id, name) VALUES ($1, 'board') RETURNING id`, repo.id).Scan(&projectID); err != nil {
		t.Fatalf("seed project: %v", err)
	}

	return privateRepoFixture{
		app:  newAPIRouterAt(t, db, reposRoot),
		repo: repo,
		params: strings.NewReplacer(
			"{ref}", "main",
			"{sha}", branchHash(t, git, "main").String(),
			"{id}", strconv.FormatInt(projectID, 10),
			"/*", "/a.txt",
		),
	}
}

// Git smart HTTP must answer a private repo with a 401, so git prompts for
// credentials.
var gitTransportRoutes = map[string]bool{
	"/{owner}/{repo}/info/refs":        true,
	"/{owner}/{repo}/git-upload-pack":  true,
	"/{owner}/{repo}/git-receive-pack": true,
}

// repoPageRoutes lists every route under /{owner}/{repo}, so a page added
// without the read check fails the test below.
func repoPageRoutes(t *testing.T, h http.Handler) []repoAPIRoute {
	t.Helper()
	var routes []repoAPIRoute
	err := chi.Walk(h.(chi.Routes), func(method, pattern string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
		if strings.HasPrefix(pattern+"/", "/{owner}/{repo}/") && !gitTransportRoutes[pattern] {
			routes = append(routes, repoAPIRoute{method, pattern})
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk routes: %v", err)
	}
	return routes
}

func (f privateRepoFixture) path(pattern, repoPath string) string {
	return repoAPIPath(f.params.Replace(pattern), repoPath)
}

// pageResponse leaves out the repo path, which a response may echo back (the
// sign-in redirect's next parameter does) without revealing anything.
func pageResponse(rr *httptest.ResponseRecorder, repoPath string) string {
	hide := strings.NewReplacer(repoPath, "{repo}", url.QueryEscape(repoPath), "{repo}")
	return strconv.Itoa(rr.Code) + " " + hide.Replace(rr.Header().Get("Location")) + "\n" + hide.Replace(rr.Body.String())
}

func TestRepoPageRoutes_PrivateRepo_LooksLikeMissingRepo(t *testing.T) {
	db := testutil.OpenTestDB(t)
	f := seedPrivateRepoFixture(t, db)
	missingPath := "/nobody_" + testutil.UniqueSuffix(t) + "/norepo"
	viewers := []struct{ name, token string }{
		{"anonymous", ""},
		{"stranger", seedSignedInUser(t, db).token},
	}

	routes := repoPageRoutes(t, f.app)
	if len(routes) < 40 {
		t.Fatalf("found %d repo page routes; the prefix filter looks broken", len(routes))
	}
	for _, v := range viewers {
		for _, rt := range routes {
			t.Run(v.name+" "+rt.method+" "+rt.pattern, func(t *testing.T) {
				private := requestAPI(f.app, rt.method, f.path(rt.pattern, f.repo.path), v.token)
				missing := requestAPI(f.app, rt.method, f.path(rt.pattern, missingPath), v.token)

				if pageResponse(private, f.repo.path) != pageResponse(missing, missingPath) {
					t.Errorf("private repo answered %d %q, missing repo %d %q; the responses must match",
						private.Code, private.Header().Get("Location"), missing.Code, missing.Header().Get("Location"))
				}
			})
		}
	}
}

// Also proves the fixture's sub-resources exist, so the test above compares
// real pages rather than two 404s.
func TestRepoPages_PrivateRepoOwner_Renders(t *testing.T) {
	db := testutil.OpenTestDB(t)
	f := seedPrivateRepoFixture(t, db)

	for _, pattern := range []string{
		"/refs", "/tree/{ref}", "/blob/{ref}/*", "/blame/{ref}/*", "/commits/{ref}", "/commit/{sha}",
		"/issues", "/issues/1", "/pulls", "/pulls/1", "/releases", "/releases/tag/1",
		"/milestones", "/milestones/1", "/projects", "/projects/{id}", "/discussions", "/discussions/1",
		"/network/dependencies", "/actions", "/pulse", "/graphs/contributors", "/stargazers",
	} {
		t.Run(pattern, func(t *testing.T) {
			rr := requestAPI(f.app, http.MethodGet, f.repo.path+f.params.Replace(pattern), f.repo.owner.token)

			if rr.Code != http.StatusOK {
				t.Errorf("want 200, got %d", rr.Code)
			}
		})
	}
	t.Run("/commits", func(t *testing.T) {
		rr := requestAPI(f.app, http.MethodGet, f.repo.path+"/commits", f.repo.owner.token)

		if want := f.repo.path + "/commits/main"; rr.Code != http.StatusFound || rr.Header().Get("Location") != want {
			t.Errorf("want 302 to %s, got %d to %q", want, rr.Code, rr.Header().Get("Location"))
		}
	})
}
