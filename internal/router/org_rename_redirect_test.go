package router_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/mkappworks-dev/cloudzilla-app/internal/service"
	"github.com/mkappworks-dev/cloudzilla-app/internal/testutil"
)

// orgRenameEnv is an org owned by r2Env's owner with a public repo "pub" and a private repo "priv", renamed once.
type orgRenameEnv struct {
	r2Env
	oldName, newName string
}

func newOrgRenameEnv(t *testing.T) orgRenameEnv {
	t.Helper()
	e := orgRenameEnv{r2Env: newR2Env(t)}
	ctx := context.Background()
	org, err := e.svc.Org.Create(ctx, e.owner.id, "testorg_"+testutil.UniqueSuffix(t), "", "")
	if err != nil {
		t.Fatal(err)
	}
	testutil.DeleteOrgOnCleanup(t, e.db, org.ID)
	for name, private := range map[string]bool{"pub": false, "priv": true} {
		if _, err := e.svc.Org.CreateRepo(ctx, org.ID, e.owner.id, name, "", private, service.RepoInitOptions{AddREADME: true}); err != nil {
			t.Fatalf("create %s: %v", name, err)
		}
	}
	e.oldName, e.newName = org.Name, "renamed_"+testutil.UniqueSuffix(t)
	rr := e.form(t, http.MethodPost, "/api/orgs/"+e.oldName+"/rename", e.owner.token,
		url.Values{"name": {e.newName}, "password": {r2Password}}, false)
	wantStatus(t, rr, http.StatusSeeOther)
	if got := rr.Header().Get("Location"); got != "/orgs/"+e.newName+"/settings" {
		t.Fatalf("rename Location = %q", got)
	}
	return e
}

func (e orgRenameEnv) get(target, token string) *httptest.ResponseRecorder {
	return aliasEnv{r2Env: e.r2Env}.get(http.MethodGet, target, token)
}

func TestOrgRename_OldPathsRedirectKeepingSubPathAndQuery(t *testing.T) {
	e := newOrgRenameEnv(t)
	for _, tc := range []struct{ name, from, to string }{
		{"profile", "/" + e.oldName, "/" + e.newName},
		{"repo root", "/" + e.oldName + "/pub", "/" + e.newName + "/pub"},
		{"sub-path with query", "/" + e.oldName + "/pub/issues?state=closed", "/" + e.newName + "/pub/issues?state=closed"},
		{"owner case", "/" + strings.ToUpper(e.oldName) + "/pub", "/" + e.newName + "/pub"},
		{"api", "/api/repos/" + e.oldName + "/pub/issues", "/api/repos/" + e.newName + "/pub/issues"},
		{"git info/refs", "/" + e.oldName + "/pub/info/refs?service=git-upload-pack", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rr := e.get(tc.from, "")
			wantStatus(t, rr, http.StatusMovedPermanently)
			loc := rr.Header().Get("Location")
			if tc.to != "" && loc != tc.to {
				t.Errorf("Location = %q, want %q", loc, tc.to)
			}
			if tc.to == "" && !strings.HasSuffix(loc, "/"+e.newName+"/pub/info/refs?service=git-upload-pack") {
				t.Errorf("Location = %q, want the new git URL with the query kept", loc)
			}
		})
	}
	wantStatus(t, e.get("/"+e.newName+"/pub", ""), http.StatusOK)
}

func TestOrgRename_PrivateRepoThroughTheOldNameLooksMissing(t *testing.T) {
	e := newOrgRenameEnv(t)
	o := e.oldName
	for _, tc := range []struct{ viewer, token string }{{"anonymous", ""}, {"outsider", e.outsider.token}} {
		for _, p := range []string{"", "/issues"} {
			missing := e.get("/"+o+"/nosuchrepo"+p, tc.token)
			wantStatus(t, missing, http.StatusNotFound)
			sameReply(t, tc.viewer+" "+p, e.get("/"+o+"/priv"+p, tc.token), missing)
		}
	}
	rr := e.get("/"+o+"/priv/issues", e.owner.token)
	wantStatus(t, rr, http.StatusMovedPermanently)
	if got := rr.Header().Get("Location"); got != "/"+e.newName+"/priv/issues" {
		t.Errorf("owner Location = %q", got)
	}
}

func TestOrgRename_ClaimedOldNameStopsRedirecting(t *testing.T) {
	e := newOrgRenameEnv(t)
	claimed, err := e.svc.Org.Create(context.Background(), e.outsider.id, e.oldName, "", "")
	if err != nil {
		t.Fatalf("claim the old name: %v", err)
	}
	testutil.DeleteOrgOnCleanup(t, e.db, claimed.ID)

	wantStatus(t, e.get("/"+e.oldName+"/pub", ""), http.StatusNotFound)
	wantStatus(t, e.get("/"+e.oldName, ""), http.StatusOK)
}

func TestOrgRename_OnlyAnOwnerWithAConfirmationRenames(t *testing.T) {
	e := newOrgRenameEnv(t)
	target := "/api/orgs/" + e.newName + "/rename"
	next := "again_" + testutil.UniqueSuffix(t)

	wantStatus(t, e.form(t, http.MethodPost, target, e.outsider.token, url.Values{"name": {next}}, false), http.StatusForbidden)
	wantStatus(t, e.form(t, http.MethodPost, target, e.owner.token, url.Values{"name": {next}}, false), http.StatusForbidden)
	wantStatus(t, e.form(t, http.MethodPost, target, e.owner.token, url.Values{"name": {e.owner.name}, "password": {r2Password}}, false), http.StatusConflict)
	wantStatus(t, e.form(t, http.MethodPost, target, e.owner.token, url.Values{"name": {"bad name"}, "password": {r2Password}}, false), http.StatusUnprocessableEntity)
	wantStatus(t, e.get("/"+e.newName, ""), http.StatusOK)
}
