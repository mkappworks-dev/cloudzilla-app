package handler_test

import (
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/mkappworks-dev/cloudzilla-app/internal/config"
	"github.com/mkappworks-dev/cloudzilla-app/internal/service"
	"github.com/mkappworks-dev/cloudzilla-app/internal/testutil"
)

func TestCodeBrowserPages_RefPickerListsOtherBranches(t *testing.T) {
	db := testutil.OpenTestDB(t)
	reposRoot := t.TempDir()
	api := newAPIRouterAt(t, db, reposRoot)
	r := seedRaceRepo(t, db, reposRoot)

	for _, tt := range []struct{ page, want string }{
		{"/tree/main", "/tree/feature"},
		{"/blob/main/a.txt", "/blob/feature/a.txt"},
		{"/blame/main/a.txt", "/blame/feature/a.txt"},
	} {
		rr := requestAPI(api, http.MethodGet, r.path+tt.page, r.owner.token)
		if rr.Code != http.StatusOK {
			t.Fatalf("GET %s: status %d", tt.page, rr.Code)
		}
		if want := `href="` + r.path + tt.want + `"`; !strings.Contains(rr.Body.String(), want) {
			t.Errorf("GET %s: no ref picker item %s", tt.page, want)
		}
	}
}

func TestCodeBrowserRoutes_ResolveSlashNamedRefs(t *testing.T) {
	db := testutil.OpenTestDB(t)
	reposRoot := t.TempDir()
	api := newAPIRouterAt(t, db, reposRoot)
	r := seedRaceRepo(t, db, reposRoot)
	code := service.NewCodeService(config.GitConfig{ReposRoot: reposRoot})
	if err := code.CreateBranch(r.owner.name, r.name, "fix/render-cache", "main"); err != nil {
		t.Fatalf("create fix/render-cache: %v", err)
	}

	for _, tt := range []struct{ page, want string }{
		{"/tree/fix/render-cache", `aria-label="Switch branch or tag, current: fix/render-cache"`},
		{"/blob/fix/render-cache/a.txt", `aria-label="Switch branch or tag, current: fix/render-cache"`},
		{"/blame/fix/render-cache/a.txt", `aria-label="Switch branch or tag, current: fix/render-cache"`},
		{"/commits/fix/render-cache", `aria-label="Branches and tags, current: fix/render-cache"`},
		{"/new/fix/render-cache", `action="` + r.path + `/new/fix/render-cache"`},
	} {
		rr := requestAPI(api, http.MethodGet, r.path+tt.page, r.owner.token)
		if rr.Code != http.StatusOK {
			t.Errorf("GET %s: status %d", tt.page, rr.Code)
			continue
		}
		if !strings.Contains(rr.Body.String(), tt.want) {
			t.Errorf("GET %s: missing %s", tt.page, tt.want)
		}
	}

	rr := requestAPI(api, http.MethodGet, r.path+"/tree/fix/render-cache/a.txt", r.owner.token)
	if want := r.path + "/blob/fix/render-cache/a.txt"; rr.Code != http.StatusFound || rr.Header().Get("Location") != want {
		t.Errorf("GET tree file: got %d %q, want 302 to %s", rr.Code, rr.Header().Get("Location"), want)
	}

	rr = requestAPI(api, http.MethodGet, r.path+"/archive/fix/render-cache.zip", r.owner.token)
	if rr.Code != http.StatusOK || !strings.Contains(rr.Header().Get("Content-Disposition"), r.name+"-fix-render-cache.zip") {
		t.Errorf("GET archive: status %d, Content-Disposition %q", rr.Code, rr.Header().Get("Content-Disposition"))
	}

	mainTip := branchHash(t, r.git, "main")
	rr = postForm(t, api, r.owner.token, r.path+"/new/fix/render-cache", url.Values{"path": {"n.txt"}, "content": {"n\n"}})
	if want := r.path + "/blob/fix/render-cache/n.txt"; rr.Code != http.StatusSeeOther || rr.Header().Get("Location") != want {
		t.Fatalf("POST new file: got %d %q, want 303 to %s: %s", rr.Code, rr.Header().Get("Location"), want, rr.Body.String())
	}
	if _, err := code.GetBlob(r.owner.name, r.name, "fix/render-cache", "n.txt"); err != nil {
		t.Errorf("n.txt not committed to fix/render-cache: %v", err)
	}
	if got := branchHash(t, r.git, "main"); got != mainTip {
		t.Errorf("main moved from %s to %s", mainTip, got)
	}
}
