package handler_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"

	gogit "github.com/go-git/go-git/v5"

	"github.com/mkappworks-dev/cloudzilla-app/internal/config"
	"github.com/mkappworks-dev/cloudzilla-app/internal/handler"
	"github.com/mkappworks-dev/cloudzilla-app/internal/service"
	"github.com/mkappworks-dev/cloudzilla-app/internal/testutil"
)

// seedCodeRepo commits README.md at the root and in lib/, so a sidebar that
// matched the active file by name would mark both, and lib/util/helper.js for
// a folder inside a folder.
func seedCodeRepo(t *testing.T) (http.Handler, seededRepo, string) {
	t.Helper()
	db := testutil.OpenTestDB(t)
	reposRoot := t.TempDir()
	r := seedOwnedRepo(t, db, false)
	git, err := gogit.PlainInit(filepath.Join(reposRoot, r.owner.name, r.name+".git"), true)
	if err != nil {
		t.Fatalf("init bare repo: %v", err)
	}
	code := service.NewCodeService(config.GitConfig{ReposRoot: reposRoot})
	for _, path := range []string{"README.md", "lib/README.md", "lib/config.js", "lib/util/helper.js"} {
		if err := code.CommitFile(r.owner.name, r.name, "main", path, []byte("x\n"), raceAuthor, "Add "+path); err != nil {
			t.Fatalf("commit %s: %v", path, err)
		}
	}
	if err := code.CreateBranch(r.owner.name, r.name, "feature/x", "main"); err != nil {
		t.Fatalf("create feature/x: %v", err)
	}
	return newAPIRouterAt(t, db, reposRoot), r, branchHash(t, git, "main").String()
}

func getAnonymous(h http.Handler, path string) *httptest.ResponseRecorder {
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, path, nil))
	return rr
}

func fileTreeSidebar(t *testing.T, body string) string {
	t.Helper()
	label := strings.Index(body, `aria-label="Repository file tree"`)
	if label < 0 {
		t.Fatalf("no file tree sidebar in body:\n%s", body)
	}
	start := strings.LastIndex(body[:label], "<aside")
	end := strings.Index(body[start:], "</aside>")
	if end < 0 {
		t.Fatalf("file tree sidebar never closes")
	}
	return body[start : start+end]
}

func TestCodePages_KeepTheFileTree(t *testing.T) {
	h, r, sha := seedCodeRepo(t)
	tests := []struct {
		name, url, active string
		libOpen           bool
	}{
		{"blob in a folder", "/blob/main/lib/config.js", "/blob/main/lib/config.js", true},
		{"blob sharing a root file's name", "/blob/main/lib/README.md", "/blob/main/lib/README.md", true},
		{"blob at the root", "/blob/main/README.md", "/blob/main/README.md", false},
		{"blob at a commit", "/blob/" + sha + "/lib/config.js", "/blob/" + sha + "/lib/config.js", true},
		{"folder", "/tree/main/lib", "", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rr := getAnonymous(h, r.path+tt.url)
			if rr.Code != http.StatusOK {
				t.Fatalf("want 200, got %d: %s", rr.Code, rr.Body.String())
			}
			body := rr.Body.String()
			sidebar := fileTreeSidebar(t, body)
			if !strings.Contains(sidebar, `x-data="fileTree"`) {
				t.Errorf("sidebar lacks its fileTree scope:\n%s", sidebar)
			}
			if strings.Contains(tt.url, "/blob/") && !strings.Contains(body, `id="L1"`) {
				t.Errorf("blob page lacks line anchors:\n%s", body)
			}

			wantCurrent := 0
			if tt.active != "" {
				wantCurrent = 1
				if want := `href="` + r.path + tt.active + `" aria-current="page"`; !strings.Contains(sidebar, want) {
					t.Errorf("want %q in sidebar:\n%s", want, sidebar)
				}
			}
			if got := strings.Count(sidebar, `aria-current="page"`); got != wantCurrent {
				t.Errorf("aria-current count = %d, want %d:\n%s", got, wantCurrent, sidebar)
			}
			if got := strings.Contains(sidebar, `/lib/config.js"`); got != tt.libOpen {
				t.Errorf("lib/ children listed = %v, want %v:\n%s", got, tt.libOpen, sidebar)
			}
		})
	}
}

func TestPageTree_RedirectsAFileToItsBlobPage(t *testing.T) {
	h, r, _ := seedCodeRepo(t)
	rr := getAnonymous(h, r.path+"/tree/main/lib/config.js")
	if want := r.path + "/blob/main/lib/config.js"; rr.Code != http.StatusFound || rr.Header().Get("Location") != want {
		t.Errorf("want 302 to %s, got %d to %q", want, rr.Code, rr.Header().Get("Location"))
	}
}

func openFoldersCookie(folders ...string) string {
	b, _ := json.Marshal(folders)
	return url.PathEscape(string(b))
}

func getWithOpenFolders(h http.Handler, path, cookie string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, path, nil)
	req.AddCookie(&http.Cookie{Name: "cz_tree_open", Value: cookie})
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	return rr
}

func TestFileTree_KeepsRememberedFoldersOpen(t *testing.T) {
	h, r, _ := seedCodeRepo(t)
	tests := []struct {
		name, cookie      string
		libOpen, utilOpen bool
	}{
		{"remembered folder", openFoldersCookie("lib"), true, false},
		{"remembered subfolder under a closed folder", openFoldersCookie("lib/util"), false, false},
		{"remembered folder and subfolder", openFoldersCookie("lib", "lib/util"), true, true},
		{"unescapable cookie", "%zz", false, false},
		{"cookie that isn't JSON", "not-json", false, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rr := getWithOpenFolders(h, r.path+"/blob/main/README.md", tt.cookie)
			if rr.Code != http.StatusOK {
				t.Fatalf("want 200, got %d: %s", rr.Code, rr.Body.String())
			}
			sidebar := fileTreeSidebar(t, rr.Body.String())
			if got := strings.Contains(sidebar, `/lib/config.js"`); got != tt.libOpen {
				t.Errorf("lib/ children listed = %v, want %v:\n%s", got, tt.libOpen, sidebar)
			}
			if got := strings.Contains(sidebar, `/lib/util/helper.js"`); got != tt.utilOpen {
				t.Errorf("lib/util/ children listed = %v, want %v:\n%s", got, tt.utilOpen, sidebar)
			}
			if got := strings.Count(sidebar, `aria-current="page"`); got != 1 {
				t.Errorf("active entries = %d, want 1 (only README.md):\n%s", got, sidebar)
			}
		})
	}
}

func TestFileTree_ClosedFoldersLoadTheirChildren(t *testing.T) {
	h, r, _ := seedCodeRepo(t)
	rr := getAnonymous(h, r.path+"/tree/main/lib")
	if rr.Code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", rr.Code, rr.Body.String())
	}
	sidebar := fileTreeSidebar(t, rr.Body.String())
	if want := `hx-get="/fragments` + r.path + `/tree/main/lib/util"`; !strings.Contains(sidebar, want) {
		t.Errorf("closed folder lacks %q:\n%s", want, sidebar)
	}
	if strings.Contains(sidebar, `hx-get="/fragments`+r.path+`/tree/main/lib"`) {
		t.Errorf("open folder must not fetch the children it already has:\n%s", sidebar)
	}
}

func TestFileTreeChildrenFragment(t *testing.T) {
	h, r, _ := seedCodeRepo(t)

	rr := getAnonymous(h, "/fragments"+r.path+"/tree/main/lib")
	if rr.Code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", rr.Code, rr.Body.String())
	}
	body := rr.Body.String()
	for _, want := range []string{
		`href="` + r.path + `/blob/main/lib/config.js"`,
		`href="` + r.path + `/tree/main/lib/util"`,
		`hx-get="/fragments` + r.path + `/tree/main/lib/util"`,
		`padding-left: 20px`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("want %q in fragment:\n%s", want, body)
		}
	}
	if strings.Contains(body, "<aside") {
		t.Errorf("fragment must hold only the folder's items:\n%s", body)
	}

	for _, path := range []string{"/tree/main/README.md", "/tree/main/nope"} {
		if rr := getAnonymous(h, "/fragments"+r.path+path); rr.Code != http.StatusNotFound {
			t.Errorf("%s: want 404, got %d", path, rr.Code)
		}
	}

	rr = getAnonymous(h, "/fragments"+r.path+"/tree/feature/x/lib")
	if want := `href="` + r.path + `/blob/feature/x/lib/config.js"`; rr.Code != http.StatusOK || !strings.Contains(rr.Body.String(), want) {
		t.Errorf("ref with a slash: want 200 with %q, got %d:\n%s", want, rr.Code, rr.Body.String())
	}
}

func TestFileTreeChildrenFragment_ExpandAll(t *testing.T) {
	h, r, _ := seedCodeRepo(t)

	page := getAnonymous(h, r.path+"/blob/main/lib/README.md")
	if want := `hx-get="/fragments` + r.path + `/tree/main/?expand=all&amp;active=lib%2FREADME.md"`; !strings.Contains(page.Body.String(), want) {
		t.Errorf("page lacks the Expand all button %q", want)
	}

	rr := getAnonymous(h, "/fragments"+r.path+"/tree/main/?expand=all&active=lib%2FREADME.md")
	if rr.Code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", rr.Code, rr.Body.String())
	}
	body := rr.Body.String()
	if !strings.Contains(body, `/lib/util/helper.js"`) {
		t.Errorf("expand all left lib/util closed:\n%s", body)
	}
	if strings.Contains(body, `hx-get="/fragments`+r.path+`/tree/main/lib`) {
		t.Errorf("an opened folder still lazy-loads its children:\n%s", body)
	}
	if want := `href="` + r.path + `/blob/main/lib/README.md" aria-current="page"`; strings.Count(body, `aria-current="page"`) != 1 || !strings.Contains(body, want) {
		t.Errorf("want only lib/README.md marked active:\n%s", body)
	}

	t.Run("budget", func(t *testing.T) {
		handler.UseExpandAllBudget(t, 1)
		body := getAnonymous(h, "/fragments"+r.path+"/tree/main/?expand=all").Body.String()
		if !strings.Contains(body, `/lib/config.js"`) || strings.Contains(body, `/lib/util/helper.js"`) {
			t.Errorf("budget 1 should open lib only:\n%s", body)
		}
		if want := `hx-get="/fragments` + r.path + `/tree/main/lib/util"`; !strings.Contains(body, want) {
			t.Errorf("a folder past the budget must still lazy-load (%q):\n%s", want, body)
		}
	})
}

func TestFileTree_HiddenCookieHidesThePanel(t *testing.T) {
	h, r, _ := seedCodeRepo(t)
	for _, page := range []string{"/blob/main/README.md", "/tree/main/lib"} {
		for _, tt := range []struct {
			cookie string
			want   string
		}{
			{"", `x-data="fileTreePanel(false)"`},
			{"1", `x-data="fileTreePanel(true)"`},
			{"0", `x-data="fileTreePanel(false)"`},
		} {
			req := httptest.NewRequest(http.MethodGet, r.path+page, nil)
			if tt.cookie != "" {
				req.AddCookie(&http.Cookie{Name: "cz_tree_hidden", Value: tt.cookie})
			}
			rr := httptest.NewRecorder()
			h.ServeHTTP(rr, req)
			if rr.Code != http.StatusOK || !strings.Contains(rr.Body.String(), tt.want) {
				t.Errorf("%s with cz_tree_hidden=%q: want 200 with %q, got %d", page, tt.cookie, tt.want, rr.Code)
			}
			if got := strings.Count(rr.Body.String(), `aria-label="Show files"`); got != 1 {
				t.Errorf("%s: want one Show files button, got %d", page, got)
			}
		}
	}
}
