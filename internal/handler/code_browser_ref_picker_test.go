package handler_test

import (
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"testing"

	"github.com/go-git/go-git/v5/plumbing"

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
		{"/tree/main/a.txt", "/tree/feature/a.txt"},
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
		{"/tree/fix/render-cache/a.txt", `aria-label="Switch branch or tag, current: fix/render-cache"`},
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

	rr := requestAPI(api, http.MethodGet, r.path+"/archive/fix/render-cache.zip", r.owner.token)
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

// shaLinks returns the distinct code-browser hrefs into repoPath whose ref is
// a commit SHA, leaving out the ref picker's branch and tag items.
func shaLinks(body, repoPath string) []string {
	re := regexp.MustCompile(`href="(` + regexp.QuoteMeta(repoPath) + `/(?:tree|blob|blame|raw|commits)/[0-9a-f]{7,40}(?:/[^"]*)?)"`)
	seen := map[string]bool{}
	var links []string
	for _, m := range re.FindAllStringSubmatch(body, -1) {
		if !seen[m[1]] {
			seen[m[1]] = true
			links = append(links, m[1])
		}
	}
	return links
}

func TestCodeBrowserPages_AtACommitLinkToPagesThatResolve(t *testing.T) {
	db := testutil.OpenTestDB(t)
	reposRoot := t.TempDir()
	api := newAPIRouterAt(t, db, reposRoot)
	r := seedRaceRepo(t, db, reposRoot)
	commitOnMain(t, reposRoot, r, "lib/config.js", []byte("x\n"))
	// PlainInit points HEAD at a missing master, which turns an unknown ref
	// into ErrEmptyRepo and an empty 200 page instead of a 404.
	if err := r.git.Storer.SetReference(plumbing.NewSymbolicReference(plumbing.HEAD, plumbing.NewBranchReferenceName("main"))); err != nil {
		t.Fatalf("point HEAD at main: %v", err)
	}
	sha := branchHash(t, r.git, "main").String()

	for _, page := range []string{
		"/tree/" + sha,
		"/tree/" + sha + "/lib",
		"/tree/" + sha + "/lib/config.js",
		"/blob/" + sha + "/lib/config.js",
		"/blame/" + sha + "/lib/config.js",
		"/commits/" + sha,
	} {
		t.Run(page, func(t *testing.T) {
			rr := requestAPI(api, http.MethodGet, r.path+page, "")
			if rr.Code != http.StatusOK {
				t.Fatalf("want 200, got %d", rr.Code)
			}
			body := rr.Body.String()
			for _, want := range []string{`<span class="font-medium">` + sha[:7] + `</span>`, `current: ` + sha[:7] + `"`} {
				if !strings.Contains(body, want) {
					t.Errorf("ref label: want %q in body", want)
				}
			}
			if want := `href="` + r.path + "/commit/" + sha + `"`; !strings.Contains(body, want) {
				t.Errorf("latest commit: want %q in body", want)
			}
			links := shaLinks(body, r.path)
			if len(links) == 0 {
				t.Fatalf("no links at a SHA in body:\n%s", body)
			}
			for _, href := range links {
				if code := requestAPI(api, http.MethodGet, href, "").Code; code != http.StatusOK {
					t.Errorf("GET %s = %d, want 200", href, code)
				}
			}
		})
	}
}

func TestPageCommit_LinksToItsParent(t *testing.T) {
	db := testutil.OpenTestDB(t)
	reposRoot := t.TempDir()
	api := newAPIRouterAt(t, db, reposRoot)
	r := seedRaceRepo(t, db, reposRoot)
	parent := r.mainTip.String()

	rr := requestAPI(api, http.MethodGet, r.path+"/commit/"+r.mainPushed.String(), "")
	if rr.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", rr.Code)
	}
	link := `href="` + r.path + "/commit/" + parent + `"`
	if want := link + ` class="font-mono ml-1 hover:text-foreground hover:underline">` + parent[:7] + `</a>`; !strings.Contains(rr.Body.String(), want) {
		t.Fatalf("want %q in body", want)
	}
	if code := requestAPI(api, http.MethodGet, r.path+"/commit/"+parent, "").Code; code != http.StatusOK {
		t.Errorf("GET parent = %d, want 200", code)
	}
}
