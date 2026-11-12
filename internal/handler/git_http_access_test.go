package handler_test

// Integration tests: git smart HTTP must not reveal a private repo to a caller
// who cannot read it. All tests require TEST_DATABASE_DSN and skip otherwise.

import (
	"database/sql"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/service"
	"github.com/mkappworks-dev/cloudzilla-app/internal/store"
	"github.com/mkappworks-dev/cloudzilla-app/internal/testutil"
)

type gitCaller struct {
	name string
	// bearer is a JWT; basic is a PAT sent as git sends it, as the Basic password.
	bearer, basic string
}

func requestGit(h http.Handler, method, path string, c gitCaller) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader("0000"))
	if c.bearer != "" {
		req.Header.Set("Authorization", "Bearer "+c.bearer)
	}
	if c.basic != "" {
		req.SetBasicAuth("x", c.basic)
	}
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	return rr
}

func gitResponse(rr *httptest.ResponseRecorder) string {
	return strconv.Itoa(rr.Code) + " " + rr.Header().Get("WWW-Authenticate") + "\n" + rr.Body.String()
}

func seedPAT(t *testing.T, db *sql.DB, userID int64, scope string) string {
	t.Helper()
	tokens := service.NewAccessTokenService(store.NewAccessTokenStore(db), store.NewUserStore(db))
	raw, _, err := tokens.Generate(t.Context(), userID, "git "+scope, []string{scope}, nil)
	if err != nil {
		t.Fatalf("generate PAT: %v", err)
	}
	return raw
}

var gitTransportRequests = []struct{ method, path string }{
	{http.MethodGet, "/info/refs"},
	{http.MethodGet, "/info/refs?service=git-upload-pack"},
	{http.MethodGet, "/info/refs?service=git-receive-pack"},
	{http.MethodPost, "/git-upload-pack"},
	{http.MethodPost, "/git-receive-pack"},
}

// A missing repo is answered exactly as a private one, so neither the status,
// the challenge nor the body confirms that the private repo exists. The
// read-only PAT can't push, so its receive-pack requests fail on scope; that
// refusal must not depend on the repo either.
func TestGitHTTP_PrivateRepo_LooksLikeMissingRepo(t *testing.T) {
	db := testutil.OpenTestDB(t)
	app := newAPIRouter(t, db)
	repo := seedOwnedRepo(t, db, true)
	missingPath := "/nobody_" + testutil.UniqueSuffix(t) + "/norepo"
	stranger := seedSignedInUser(t, db)
	aliasPath := seedAlias(t, db, repo)

	callers := []gitCaller{
		{name: "anonymous"},
		{name: "stranger JWT", bearer: stranger.token},
		{name: "stranger PAT", basic: seedPAT(t, db, stranger.id, model.ScopeRepoWrite)},
		{name: "stranger read-only PAT", basic: seedPAT(t, db, stranger.id, model.ScopeRepoRead)},
	}
	for _, c := range callers {
		for _, rt := range gitTransportRequests {
			for _, path := range []string{repo.path, aliasPath} {
				t.Run(c.name+" "+rt.method+" "+path+rt.path, func(t *testing.T) {
					private := requestGit(app, rt.method, path+rt.path, c)
					missing := requestGit(app, rt.method, missingPath+rt.path, c)

					if gitResponse(private) != gitResponse(missing) {
						t.Errorf("private repo answered %q, missing repo %q; the responses must match",
							gitResponse(private), gitResponse(missing))
					}
				})
			}
		}
	}
}

// seedAlias gives repo a former name and returns that name's path.
func seedAlias(t *testing.T, db *sql.DB, repo seededRepo) string {
	t.Helper()
	old := "old_" + testutil.UniqueSuffix(t)
	testutil.Exec(t, db, `INSERT INTO repo_aliases (repo_id, owner_name, name) VALUES ($1, $2, $3)`, repo.id, repo.owner.name, old)
	return "/" + repo.owner.name + "/" + old
}

// Git follows the 301 on info/refs and rebases its remote, then replays the POSTs, which is why
// those answer 308. Only a caller who can read the moved repo is told where it went.
func TestGitHTTP_Alias_Redirects(t *testing.T) {
	db := testutil.OpenTestDB(t)
	app := newAPIRouter(t, db)
	for _, private := range []bool{false, true} {
		repo := seedOwnedRepo(t, db, private)
		aliasPath := seedAlias(t, db, repo)
		owner := gitCaller{name: "owner", bearer: repo.owner.token}
		reader := gitCaller{name: "owner", basic: seedPAT(t, db, repo.owner.id, model.ScopeRepoRead)}
		anonymous := gitCaller{name: "anonymous"}

		type redirectCase struct {
			caller gitCaller
			method string
			suffix string
			want   int
			loc    string
		}
		cases := []redirectCase{
			{owner, http.MethodGet, "/info/refs?service=git-upload-pack", 301, repo.path + "/info/refs?service=git-upload-pack"},
			{owner, http.MethodGet, "/info/refs?service=git-receive-pack", 301, repo.path + "/info/refs?service=git-receive-pack"},
			{owner, http.MethodPost, "/git-upload-pack", 308, repo.path + "/git-upload-pack"},
			{owner, http.MethodPost, "/git-receive-pack", 308, repo.path + "/git-receive-pack"},
			{reader, http.MethodGet, "/info/refs?service=git-upload-pack", 301, repo.path + "/info/refs?service=git-upload-pack"},
		}
		if !private {
			cases = append(cases, redirectCase{anonymous, http.MethodGet, "/info/refs?service=git-upload-pack", 301, repo.path + "/info/refs?service=git-upload-pack"})
		}
		for _, tc := range cases {
			t.Run(strconv.FormatBool(private)+" "+tc.caller.name+" "+tc.method+" "+tc.suffix, func(t *testing.T) {
				rr := requestGit(app, tc.method, aliasPath+tc.suffix, tc.caller)
				if rr.Code != tc.want || rr.Header().Get("Location") != tc.loc {
					t.Errorf("got %d Location %q, want %d %q", rr.Code, rr.Header().Get("Location"), tc.want, tc.loc)
				}
			})
		}

		t.Run(strconv.FormatBool(private)+" .git suffix is kept", func(t *testing.T) {
			rr := requestGit(app, http.MethodGet, aliasPath+".git/info/refs?service=git-upload-pack", owner)
			if want := repo.path + ".git/info/refs?service=git-upload-pack"; rr.Header().Get("Location") != want {
				t.Errorf("Location %q, want %q", rr.Header().Get("Location"), want)
			}
		})
	}
}

// An alias is not a live path: once a repo is soft-deleted its old names stop resolving.
func TestGitHTTP_Alias_DeletedRepoLooksMissing(t *testing.T) {
	db := testutil.OpenTestDB(t)
	app := newAPIRouter(t, db)
	repo := seedOwnedRepo(t, db, false)
	aliasPath := seedAlias(t, db, repo)
	testutil.Exec(t, db, `UPDATE repositories SET deleted_at = NOW() WHERE id = $1`, repo.id)

	rr := requestGit(app, http.MethodGet, aliasPath+"/info/refs?service=git-upload-pack", gitCaller{bearer: repo.owner.token})
	if rr.Code != http.StatusNotFound {
		t.Errorf("got %d, want 404", rr.Code)
	}
}

// The matching responses must still be the useful ones: git prompts for
// credentials only on a 401 challenge, and a signed-in caller who can't read
// the repo is told it doesn't exist.
func TestGitHTTP_PrivateRepoNonReader_Status(t *testing.T) {
	db := testutil.OpenTestDB(t)
	app := newAPIRouter(t, db)
	repo := seedOwnedRepo(t, db, true)
	stranger := seedSignedInUser(t, db)
	anonymous := gitCaller{name: "anonymous"}
	signedIn := gitCaller{name: "stranger", basic: seedPAT(t, db, stranger.id, model.ScopeRepoWrite)}

	cases := []struct {
		caller gitCaller
		method string
		path   string
		want   int
	}{
		{anonymous, http.MethodGet, "/info/refs", http.StatusBadRequest},
		{anonymous, http.MethodGet, "/info/refs?service=git-upload-pack", http.StatusUnauthorized},
		{anonymous, http.MethodGet, "/info/refs?service=git-receive-pack", http.StatusUnauthorized},
		{anonymous, http.MethodPost, "/git-upload-pack", http.StatusUnauthorized},
		{anonymous, http.MethodPost, "/git-receive-pack", http.StatusUnauthorized},
		{signedIn, http.MethodGet, "/info/refs?service=git-upload-pack", http.StatusNotFound},
		{signedIn, http.MethodGet, "/info/refs?service=git-receive-pack", http.StatusNotFound},
		{signedIn, http.MethodPost, "/git-upload-pack", http.StatusNotFound},
		{signedIn, http.MethodPost, "/git-receive-pack", http.StatusNotFound},
	}
	for _, tc := range cases {
		t.Run(tc.caller.name+" "+tc.method+" "+tc.path, func(t *testing.T) {
			rr := requestGit(app, tc.method, repo.path+tc.path, tc.caller)

			if rr.Code != tc.want {
				t.Errorf("want %d, got %d: %s", tc.want, rr.Code, rr.Body.String())
			}
			if challenged := rr.Header().Get("WWW-Authenticate") != ""; challenged != (tc.want == http.StatusUnauthorized) {
				t.Errorf("WWW-Authenticate %q on a %d", rr.Header().Get("WWW-Authenticate"), rr.Code)
			}
		})
	}
}

// A reader who may not push keeps their credential: on a 401 git's credential
// helper would erase it.
func TestGitHTTP_ReaderPush_403(t *testing.T) {
	db := testutil.OpenTestDB(t)
	app := newAPIRouter(t, db)
	repo := seedOwnedRepo(t, db, false)
	reader := gitCaller{name: "reader", basic: seedPAT(t, db, seedSignedInUser(t, db).id, model.ScopeRepoWrite)}

	for _, path := range []string{"/info/refs?service=git-receive-pack", "/git-receive-pack"} {
		t.Run(path, func(t *testing.T) {
			method := http.MethodGet
			if path == "/git-receive-pack" {
				method = http.MethodPost
			}
			rr := requestGit(app, method, repo.path+path, reader)

			if rr.Code != http.StatusForbidden || rr.Header().Get("WWW-Authenticate") != "" {
				t.Errorf("want a 403 without a challenge, got %d %q: %s", rr.Code, rr.Header().Get("WWW-Authenticate"), rr.Body.String())
			}
		})
	}
}
