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

	callers := []gitCaller{
		{name: "anonymous"},
		{name: "stranger JWT", bearer: stranger.token},
		{name: "stranger PAT", basic: seedPAT(t, db, stranger.id, model.ScopeRepoWrite)},
		{name: "stranger read-only PAT", basic: seedPAT(t, db, stranger.id, model.ScopeRepoRead)},
	}
	for _, c := range callers {
		for _, rt := range gitTransportRequests {
			t.Run(c.name+" "+rt.method+" "+rt.path, func(t *testing.T) {
				private := requestGit(app, rt.method, repo.path+rt.path, c)
				missing := requestGit(app, rt.method, missingPath+rt.path, c)

				if gitResponse(private) != gitResponse(missing) {
					t.Errorf("private repo answered %q, missing repo %q; the responses must match",
						gitResponse(private), gitResponse(missing))
				}
			})
		}
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
