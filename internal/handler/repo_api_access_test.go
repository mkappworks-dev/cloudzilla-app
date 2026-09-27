package handler_test

// Integration tests: the repo JSON API must not reveal a private repo to a user
// who cannot read it. All tests require TEST_DATABASE_DSN and skip otherwise.

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/mkappworks-dev/cloudzilla-app/internal/config"
	"github.com/mkappworks-dev/cloudzilla-app/internal/router"
	"github.com/mkappworks-dev/cloudzilla-app/internal/service"
	"github.com/mkappworks-dev/cloudzilla-app/internal/store"
	"github.com/mkappworks-dev/cloudzilla-app/internal/testutil"
)

var repoAPIPrefixes = []string{"/api/repos/{owner}/{repo}", "/fragments/{owner}/{repo}"}

func newAPIRouter(t *testing.T, db *sql.DB) http.Handler {
	t.Helper()
	cfg := &config.Config{
		Server: config.ServerConfig{BaseURL: "http://localhost:8080"},
		Auth:   config.AuthConfig{JWTSecret: testJWTSecret, JWTExpiry: 24 * time.Hour, CookieName: testCookieName},
		Git:    config.GitConfig{ReposRoot: t.TempDir()},
	}
	return router.New(service.New(store.New(db), cfg), cfg, fstest.MapFS{})
}

type repoAPIRoute struct{ method, pattern string }

// repoAPIRoutes lists every repo API and fragment route, so a route added
// without the read check fails the tests below.
func repoAPIRoutes(t *testing.T, h http.Handler) []repoAPIRoute {
	t.Helper()
	var routes []repoAPIRoute
	err := chi.Walk(h.(chi.Routes), func(method, pattern string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
		for _, prefix := range repoAPIPrefixes {
			if strings.HasPrefix(pattern, prefix) {
				routes = append(routes, repoAPIRoute{method, pattern})
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk routes: %v", err)
	}
	return routes
}

var routeParam = regexp.MustCompile(`\{[^}]+\}`)

// Plausible values, so a handler that validates its path parameters still
// reaches the repo lookup.
var routeParamValues = map[string]string{
	"{sha}":  strings.Repeat("a", 40),
	"{slug}": "Home",
}

func repoAPIPath(pattern, repoPath string) string {
	path := strings.Replace(pattern, "/{owner}/{repo}", repoPath, 1)
	return routeParam.ReplaceAllStringFunc(path, func(p string) string {
		if v, ok := routeParamValues[p]; ok {
			return v
		}
		return "1"
	})
}

// The body is an empty JSON object: a handler that validates it before loading
// the repo would answer a missing repo with a 400 and fail the 404 check.
func requestAPI(h http.Handler, method, path, token string) *httptest.ResponseRecorder {
	return requestAPIBody(h, method, path, token, "{}")
}

func requestAPIBody(h http.Handler, method, path, token, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	return rr
}

const missingRepoBody = `{"error":"repo not found"}` + "\n"

// A 403 for a private repo against a 404 for a missing one would confirm that
// the private repo exists, so the two responses must be identical. A missing
// repo must get the repo 404 even for a route whose sub-resource doesn't
// exist: a sub-resource 404 checked first would differ once it did.
func TestRepoAPI_PrivateRepoNonReader_LooksLikeMissingRepo(t *testing.T) {
	db := testutil.OpenTestDB(t)
	api := newAPIRouter(t, db)
	repo := seedOwnedRepo(t, db, true)
	token := seedSignedInUser(t, db).token
	missingPath := "/nobody_" + testutil.UniqueSuffix(t) + "/norepo"

	routes := repoAPIRoutes(t, api)
	if len(routes) < 100 {
		t.Fatalf("found %d repo API routes; the prefix filter looks broken", len(routes))
	}
	for _, rt := range routes {
		t.Run(rt.method+" "+rt.pattern, func(t *testing.T) {
			private := requestAPI(api, rt.method, repoAPIPath(rt.pattern, repo.path), token)
			missing := requestAPI(api, rt.method, repoAPIPath(rt.pattern, missingPath), token)

			if missing.Code != http.StatusNotFound || missing.Body.String() != missingRepoBody {
				t.Errorf("missing repo: want 404 %s, got %d %s", missingRepoBody, missing.Code, missing.Body.String())
			}
			if private.Code != missing.Code || private.Body.String() != missing.Body.String() {
				t.Errorf("private repo response (%d %s) must match a missing repo's (%d %s)",
					private.Code, private.Body.String(), missing.Code, missing.Body.String())
			}
		})
	}
}

func TestRepoAPI_PrivateRepoAnonymous_LooksLikeMissingRepo(t *testing.T) {
	db := testutil.OpenTestDB(t)
	api := newAPIRouter(t, db)
	repo := seedOwnedRepo(t, db, true)
	missingPath := "/nobody_" + testutil.UniqueSuffix(t) + "/norepo"

	for _, rt := range repoAPIRoutes(t, api) {
		t.Run(rt.method+" "+rt.pattern, func(t *testing.T) {
			private := requestAPI(api, rt.method, repoAPIPath(rt.pattern, repo.path), "")
			missing := requestAPI(api, rt.method, repoAPIPath(rt.pattern, missingPath), "")

			if private.Code != missing.Code || private.Body.String() != missing.Body.String() {
				t.Errorf("private repo response (%d %s) must match a missing repo's (%d %s)",
					private.Code, private.Body.String(), missing.Code, missing.Body.String())
			}
		})
	}
}

type repoAPICase struct{ method, path string }

var repoAPIWriteCases = []repoAPICase{
	{http.MethodPatch, ""},
	{http.MethodPost, "/labels"},
	{http.MethodPost, "/milestones"},
	{http.MethodPost, "/releases"},
	{http.MethodPost, "/branches"},
	{http.MethodPost, "/tags"},
	{http.MethodPost, "/statuses/" + strings.Repeat("a", 40)},
	{http.MethodPost, "/wiki/Home"},
	{http.MethodPut, "/topics"},
	{http.MethodPost, "/collaborators"},
	{http.MethodPost, "/keys"},
	{http.MethodPost, "/hooks"},
	{http.MethodPost, "/transfer"},
	{http.MethodPost, "/archive"},
	{http.MethodPatch, "/template"},
	{http.MethodPost, "/delete"},
}

func TestRepoAPI_ReaderWithoutWriteAccess_Forbidden(t *testing.T) {
	db := testutil.OpenTestDB(t)
	api := newAPIRouter(t, db)
	public := seedOwnedRepo(t, db, false)
	private := seedOwnedRepo(t, db, true)
	reader := seedSignedInUser(t, db)
	testutil.Exec(t, db, `INSERT INTO permissions (user_id, repo_id, role) VALUES ($1, $2, 'reader')`, reader.id, private.id)

	for label, repo := range map[string]seededRepo{"public": public, "private": private} {
		for _, tc := range repoAPIWriteCases {
			t.Run(label+" "+tc.method+tc.path, func(t *testing.T) {
				rr := requestAPI(api, tc.method, "/api/repos"+repo.path+tc.path, reader.token)

				if rr.Code != http.StatusForbidden {
					t.Errorf("want 403, got %d %s", rr.Code, rr.Body.String())
				}
			})
		}
	}
}

// Issue lookups filter by issue visibility only; the repo read check is what
// keeps a private repo's issues from everyone else.
func TestRepoAPI_PrivateRepoIssues_HiddenFromNonReaders(t *testing.T) {
	db := testutil.OpenTestDB(t)
	api := newAPIRouter(t, db)
	repo := seedOwnedRepo(t, db, true)
	stranger := seedSignedInUser(t, db).token

	created := requestAPIBody(api, http.MethodPost, "/api/repos"+repo.path+"/issues", repo.owner.token, `{"title":"secret plans"}`)
	if created.Code != http.StatusCreated {
		t.Fatalf("create issue: want 201, got %d %s", created.Code, created.Body.String())
	}
	var issue struct{ Number int }
	if err := json.Unmarshal(created.Body.Bytes(), &issue); err != nil {
		t.Fatalf("decode issue: %v", err)
	}
	number := strconv.Itoa(issue.Number)

	for _, path := range []string{
		"/api/repos" + repo.path + "/issues",
		"/api/repos" + repo.path + "/issues/" + number,
		"/api/repos" + repo.path + "/issues/" + number + "/title",
		"/fragments" + repo.path + "/issues/" + number + "/comments",
	} {
		t.Run(path, func(t *testing.T) {
			for viewer, token := range map[string]string{"signed-in": stranger, "anonymous": ""} {
				rr := requestAPI(api, http.MethodGet, path, token)
				if rr.Code != http.StatusNotFound || rr.Body.String() != missingRepoBody {
					t.Errorf("%s non-reader: want 404 %s, got %d %s", viewer, missingRepoBody, rr.Code, rr.Body.String())
				}
			}
			if rr := requestAPI(api, http.MethodGet, path, repo.owner.token); rr.Code != http.StatusOK {
				t.Errorf("owner: want 200, got %d %s", rr.Code, rr.Body.String())
			}
		})
	}
}

func TestRepoAPI_RestoreDeletedRepo_NonOwner_LooksLikeMissingRepo(t *testing.T) {
	db := testutil.OpenTestDB(t)
	api := newAPIRouter(t, db)
	repo := seedOwnedRepo(t, db, false)
	testutil.Exec(t, db, `UPDATE repositories SET deleted_at = now() WHERE id = $1`, repo.id)
	token := seedSignedInUser(t, db).token

	deleted := requestAPI(api, http.MethodPost, "/api/repos"+repo.path+"/restore", token)
	missing := requestAPI(api, http.MethodPost, "/api/repos/nobody_"+testutil.UniqueSuffix(t)+"/norepo/restore", token)

	if deleted.Code != http.StatusNotFound || deleted.Body.String() != missing.Body.String() {
		t.Errorf("deleted repo response (%d %s) must match a missing repo's (%d %s)",
			deleted.Code, deleted.Body.String(), missing.Code, missing.Body.String())
	}
	if rr := requestAPI(api, http.MethodPost, "/api/repos"+repo.path+"/restore", repo.owner.token); rr.Code != http.StatusSeeOther {
		t.Errorf("owner restore: want 303, got %d %s", rr.Code, rr.Body.String())
	}
}

func TestRepoAPI_ProjectThroughAnotherRepo_LooksLikeMissingProject(t *testing.T) {
	db := testutil.OpenTestDB(t)
	api := newAPIRouter(t, db)
	private := seedOwnedRepo(t, db, true)
	public := seedOwnedRepo(t, db, false)
	stranger := seedSignedInUser(t, db).token

	created := requestAPIBody(api, http.MethodPost, "/api/repos"+private.path+"/projects", private.owner.token, `{"name":"roadmap"}`)
	if created.Code != http.StatusCreated {
		t.Fatalf("create project: want 201, got %d %s", created.Code, created.Body.String())
	}
	var project struct{ ID int64 }
	if err := json.Unmarshal(created.Body.Bytes(), &project); err != nil {
		t.Fatalf("decode project: %v", err)
	}
	id := strconv.FormatInt(project.ID, 10)

	for _, tc := range []repoAPICase{
		{http.MethodPatch, "/projects/%s"},
		{http.MethodDelete, "/projects/%s"},
		{http.MethodPost, "/projects/%s/columns"},
	} {
		t.Run(tc.method+tc.path, func(t *testing.T) {
			other := requestAPI(api, tc.method, "/api/repos"+public.path+fmt.Sprintf(tc.path, id), stranger)
			missing := requestAPI(api, tc.method, "/api/repos"+public.path+fmt.Sprintf(tc.path, "0"), stranger)

			if other.Code != http.StatusNotFound || other.Body.String() != missing.Body.String() {
				t.Errorf("another repo's project (%d %s) must look like a missing one (%d %s)",
					other.Code, other.Body.String(), missing.Code, missing.Body.String())
			}
		})
	}
}

func seedOpenPull(t *testing.T, db *sql.DB, repo seededRepo) int64 {
	t.Helper()
	var id int64
	err := db.QueryRow(
		`INSERT INTO pull_requests (repo_id, number, author_id, title, state, head_branch, base_branch)
		 VALUES ($1, 1, $2, 'change', 'open', 'feature', 'main') RETURNING id`,
		repo.id, repo.owner.id,
	).Scan(&id)
	if err != nil {
		t.Fatalf("seed pull: %v", err)
	}
	return id
}

func TestRepoAPI_ApplySuggestionFromAnotherRepo_NotFound(t *testing.T) {
	db := testutil.OpenTestDB(t)
	api := newAPIRouter(t, db)
	private := seedOwnedRepo(t, db, true)
	own := seedOwnedRepo(t, db, false)
	seedOpenPull(t, db, own)

	var suggestionID int64
	err := db.QueryRow(
		`INSERT INTO pull_line_comments (pull_id, repo_id, author_id, author_name, path, diff_side, line, body, is_suggestion, suggestion_body)
		 VALUES ($1, $2, $3, $4, 'secret.txt', 'right', 1, 'try this', true, 'private contents') RETURNING id`,
		seedOpenPull(t, db, private), private.id, private.owner.id, private.owner.name,
	).Scan(&suggestionID)
	if err != nil {
		t.Fatalf("seed suggestion: %v", err)
	}

	path := "/api/repos" + own.path + "/pulls/1/line_comments/" + strconv.FormatInt(suggestionID, 10) + "/apply"
	rr := requestAPI(api, http.MethodPost, path, own.owner.token)

	if want := `{"error":"comment not found"}` + "\n"; rr.Code != http.StatusNotFound || rr.Body.String() != want {
		t.Errorf("want 404 %s, got %d %s", want, rr.Code, rr.Body.String())
	}
}

func TestRepoAPI_CreateFromPrivateRepo_LooksLikeMissingTemplate(t *testing.T) {
	db := testutil.OpenTestDB(t)
	api := newAPIRouter(t, db)
	plain := seedOwnedRepo(t, db, true)
	template := seedOwnedRepo(t, db, true)
	testutil.Exec(t, db, `UPDATE repositories SET is_template = true WHERE id = $1`, template.id)
	token := seedSignedInUser(t, db).token

	createFrom := func(templateID int64) *httptest.ResponseRecorder {
		return postForm(t, api, token, "/api/repos/from-template", url.Values{
			"template_repo_id": {strconv.FormatInt(templateID, 10)},
			"name":             {"copy_" + testutil.UniqueSuffix(t)},
		})
	}
	missing := createFrom(1 << 62)
	if missing.Code != http.StatusNotFound {
		t.Fatalf("missing template: want 404, got %d %s", missing.Code, missing.Body.String())
	}

	for label, id := range map[string]int64{"private repo": plain.id, "private template": template.id} {
		if rr := createFrom(id); rr.Code != missing.Code || rr.Body.String() != missing.Body.String() {
			t.Errorf("%s response (%d %s) must match a missing template's (%d %s)",
				label, rr.Code, rr.Body.String(), missing.Code, missing.Body.String())
		}
	}
}
