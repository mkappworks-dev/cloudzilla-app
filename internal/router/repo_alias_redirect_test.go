package router_test

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/mkappworks-dev/cloudzilla-app/internal/service"
	"github.com/mkappworks-dev/cloudzilla-app/internal/store"
	"github.com/mkappworks-dev/cloudzilla-app/internal/testutil"
)

// aliasEnv is r2Env's public repo "proj" with the former name "oldproj", plus a private repo "secret" formerly "hidden".
type aliasEnv struct {
	r2Env
	aliases *store.RepoAliasStore
}

func newAliasEnv(t *testing.T) aliasEnv {
	t.Helper()
	e := aliasEnv{r2Env: newR2Env(t)}
	e.aliases = store.NewRepoAliasStore(e.db)
	ctx := context.Background()
	if err := e.aliases.Add(ctx, e.repo.ID, e.owner.name, "oldproj"); err != nil {
		t.Fatal(err)
	}
	secret, err := e.svc.Repo.Create(ctx, e.owner.id, e.owner.name, "secret", "", true, service.RepoInitOptions{})
	if err != nil {
		t.Fatalf("create private repo: %v", err)
	}
	t.Cleanup(func() { testutil.Exec(t, e.db, `DELETE FROM repositories WHERE id = $1`, secret.ID) })
	if err := e.aliases.Add(ctx, secret.ID, e.owner.name, "hidden"); err != nil {
		t.Fatal(err)
	}
	return e
}

func (e aliasEnv) get(method, target, token string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, target, nil)
	if token != "" {
		req.AddCookie(&http.Cookie{Name: "cz_token", Value: token})
	}
	return serve(e.h, req)
}

func TestRepoAlias_PagesRedirectKeepingSubPathAndQuery(t *testing.T) {
	e := newAliasEnv(t)
	o := e.owner.name
	tests := []struct{ name, from, to string }{
		{"repo root", "/" + o + "/oldproj", "/" + o + "/proj"},
		{"blob with query", "/" + o + "/oldproj/blob/main/README.md?plain=1", "/" + o + "/proj/blob/main/README.md?plain=1"},
		{"encoded sub-path", "/" + o + "/oldproj/blob/main/a%20b.md", "/" + o + "/proj/blob/main/a%20b.md"},
		{"encoded slash", "/" + o + "/oldproj/blob/feat%2Fx/a.md", "/" + o + "/proj/blob/feat%2Fx/a.md"},
		{"repo name case", "/" + o + "/OldProj/issues", "/" + o + "/proj/issues"},
		{"owner case", "/" + strings.ToUpper(o) + "/oldproj/issues?state=closed&page=2", "/" + o + "/proj/issues?state=closed&page=2"},
		{"api get", "/api/repos/" + o + "/oldproj/issues?state=all", "/api/repos/" + o + "/proj/issues?state=all"},
		{"api repo", "/api/repos/" + o + "/oldproj", "/api/repos/" + o + "/proj"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			rr := e.get(http.MethodGet, tc.from, "")
			wantStatus(t, rr, http.StatusMovedPermanently)
			if got := rr.Header().Get("Location"); got != tc.to {
				t.Errorf("Location = %q, want %q", got, tc.to)
			}
		})
	}
}

func TestRepoAlias_HeadRedirectsLikeGet(t *testing.T) {
	e := newAliasEnv(t)
	rr := e.get(http.MethodHead, "/"+e.owner.name+"/oldproj/releases/download/v1/a.zip", "")
	wantStatus(t, rr, http.StatusMovedPermanently)
	if got := rr.Header().Get("Location"); got != "/"+e.owner.name+"/proj/releases/download/v1/a.zip" {
		t.Errorf("Location = %q", got)
	}
}

func TestRepoAlias_NonGetGets308AndTheMethodReachesTheRepo(t *testing.T) {
	e := newAliasEnv(t)
	old := "/api/repos/" + e.owner.name + "/oldproj/issues"

	rr := e.jsonBody(t, http.MethodPost, old, e.owner.token, `{"title":"via old path"}`)
	wantStatus(t, rr, http.StatusPermanentRedirect)
	to := rr.Header().Get("Location")
	if to != "/api/repos/"+e.owner.name+"/proj/issues" {
		t.Fatalf("Location = %q", to)
	}

	wantStatus(t, e.jsonBody(t, http.MethodPost, to, e.owner.token, `{"title":"via old path"}`), http.StatusCreated)
	var n int
	if err := e.db.QueryRow(`SELECT COUNT(*) FROM issues WHERE repo_id = $1 AND title = 'via old path'`, e.repo.ID).Scan(&n); err != nil || n != 1 {
		t.Errorf("issues created = %d (err %v), want 1", n, err)
	}

	rr = e.jsonBody(t, http.MethodPatch, "/api/repos/"+e.owner.name+"/oldproj", e.owner.token, `{"description":"x"}`)
	wantStatus(t, rr, http.StatusPermanentRedirect)

	rr = e.form(t, http.MethodPost, "/"+e.owner.name+"/oldproj/milestones/new", e.owner.token, url.Values{"title": {"m"}}, false)
	wantStatus(t, rr, http.StatusPermanentRedirect)
	if got := rr.Header().Get("Location"); got != "/"+e.owner.name+"/proj/milestones/new" {
		t.Errorf("form Location = %q", got)
	}
}

func TestRepoAlias_HTMXGetUsesHXRedirect(t *testing.T) {
	e := newAliasEnv(t)
	req := httptest.NewRequest(http.MethodGet, "/"+e.owner.name+"/oldproj/issues?q=a", nil)
	req.Header.Set("HX-Request", "true")
	rr := serve(e.h, req)
	wantStatus(t, rr, http.StatusNoContent)
	if got := rr.Header().Get("HX-Redirect"); got != "/"+e.owner.name+"/proj/issues?q=a" {
		t.Errorf("HX-Redirect = %q", got)
	}
}

// sameReply compares what a client can see of two responses, minus the per-request values.
func sameReply(t *testing.T, label string, got, want *httptest.ResponseRecorder) {
	t.Helper()
	if got.Code != want.Code {
		t.Errorf("%s: status %d, want %d", label, got.Code, want.Code)
	}
	for _, rr := range []*httptest.ResponseRecorder{got, want} {
		rr.Header().Del("X-Request-Id")
		rr.Header().Del("Set-Cookie")
	}
	if g, w := got.Header(), want.Header(); len(g) != len(w) {
		t.Errorf("%s: headers %v, want %v", label, g, w)
	} else {
		for k := range w {
			if strings.Join(g[k], ",") != strings.Join(w[k], ",") {
				t.Errorf("%s: header %s = %v, want %v", label, k, g[k], w[k])
			}
		}
	}
	if !bytes.Equal(got.Body.Bytes(), want.Body.Bytes()) {
		t.Errorf("%s: body differs from the missing-repo reply", label)
	}
}

func TestRepoAlias_PrivateTargetLooksMissingToThoseWhoCannotRead(t *testing.T) {
	e := newAliasEnv(t)
	o := e.owner.name
	for _, tc := range []struct{ viewer, token string }{{"anonymous", ""}, {"outsider", e.outsider.token}} {
		for _, p := range []string{"", "/issues", "/blob/main/README.md?plain=1"} {
			label := tc.viewer + " " + p
			missing := e.get(http.MethodGet, "/"+o+"/nosuchrepo"+p, tc.token)
			wantStatus(t, missing, http.StatusNotFound)
			sameReply(t, label, e.get(http.MethodGet, "/"+o+"/hidden"+p, tc.token), missing)
		}
		missing := e.get(http.MethodGet, "/api/repos/"+o+"/nosuchrepo/issues", tc.token)
		sameReply(t, tc.viewer+" api", e.get(http.MethodGet, "/api/repos/"+o+"/hidden/issues", tc.token), missing)
	}

	missing := e.jsonBody(t, http.MethodPost, "/api/repos/"+o+"/nosuchrepo/issues", e.outsider.token, `{"title":"t"}`)
	sameReply(t, "outsider POST", e.jsonBody(t, http.MethodPost, "/api/repos/"+o+"/hidden/issues", e.outsider.token, `{"title":"t"}`), missing)

	rr := e.get(http.MethodGet, "/"+o+"/hidden/issues", e.owner.token)
	wantStatus(t, rr, http.StatusMovedPermanently)
	if got := rr.Header().Get("Location"); got != "/"+o+"/secret/issues" {
		t.Errorf("owner Location = %q", got)
	}
	wantStatus(t, e.get(http.MethodGet, "/"+o+"/hidden/issues", e.writer.token), http.StatusNotFound)
}

func TestRepoAlias_SoftDeletedTargetDoesNotRedirect(t *testing.T) {
	e := newAliasEnv(t)
	testutil.Exec(t, e.db, `UPDATE repositories SET deleted_at = NOW() WHERE id = $1`, e.repo.ID)
	missing := e.get(http.MethodGet, "/"+e.owner.name+"/nosuchrepo/issues", "")
	sameReply(t, "deleted target", e.get(http.MethodGet, "/"+e.owner.name+"/oldproj/issues", ""), missing)
}

func TestRepoAlias_LivePathWinsAndNewRepoAtOldNameIsServed(t *testing.T) {
	e := newAliasEnv(t)
	o := e.owner.name

	rr := e.get(http.MethodGet, "/"+o+"/proj/issues", "")
	wantStatus(t, rr, http.StatusOK)
	if loc := rr.Header().Get("Location"); loc != "" {
		t.Errorf("live path redirected to %q", loc)
	}
	// A 404 inside a live repo is the handler's own, not an alias miss.
	wantStatus(t, e.get(http.MethodGet, "/"+o+"/proj/issues/9999", ""), http.StatusNotFound)

	fresh, err := e.svc.Repo.Create(context.Background(), e.owner.id, o, "oldproj", "", false, service.RepoInitOptions{})
	if err != nil {
		t.Fatalf("create at the old name: %v", err)
	}
	t.Cleanup(func() { testutil.Exec(t, e.db, `DELETE FROM repositories WHERE id = $1`, fresh.ID) })
	rr = e.get(http.MethodGet, "/"+o+"/oldproj/issues", "")
	wantStatus(t, rr, http.StatusOK)
	if loc := rr.Header().Get("Location"); loc != "" {
		t.Errorf("new repo at the old name redirected to %q", loc)
	}
	if _, err := e.svc.Repo.ResolveAlias(context.Background(), o, "oldproj"); err == nil {
		t.Error("the alias survived a new repo claiming its name")
	}
}

// The web middleware skips git paths; the git handler answers them with its own 301 that keeps the query.
func TestRepoAlias_GitPathsAreAnsweredByTheGitHandlers(t *testing.T) {
	e := newAliasEnv(t)
	rr := e.get(http.MethodGet, "/"+e.owner.name+"/oldproj/info/refs?service=git-upload-pack", "")
	wantStatus(t, rr, http.StatusMovedPermanently)
	if loc := rr.Header().Get("Location"); !strings.HasSuffix(loc, "/proj/info/refs?service=git-upload-pack") {
		t.Errorf("Location = %q, want the repo's current git URL with the query kept", loc)
	}
}
