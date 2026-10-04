package handler_test

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	gogit "github.com/go-git/go-git/v5"

	"github.com/mkappworks-dev/cloudzilla-app/internal/config"
	"github.com/mkappworks-dev/cloudzilla-app/internal/service"
	"github.com/mkappworks-dev/cloudzilla-app/internal/testutil"
)

// seedCodeRepo commits README.md at the root and in lib/, so a sidebar that
// matched the active file by name would mark both.
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
	for _, path := range []string{"README.md", "lib/README.md", "lib/config.js"} {
		if err := code.CommitFile(r.owner.name, r.name, "main", path, []byte("x\n"), raceAuthor, "Add "+path); err != nil {
			t.Fatalf("commit %s: %v", path, err)
		}
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
			if !strings.Contains(sidebar, `x-data="{ filter: '' }"`) {
				t.Errorf("sidebar lacks the filter's x-data scope:\n%s", sidebar)
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
