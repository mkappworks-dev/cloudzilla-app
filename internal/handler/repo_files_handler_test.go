package handler_test

import (
	"fmt"
	"net/http"
	"net/url"
	"path/filepath"
	"testing"

	gogit "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/mkappworks-dev/cloudzilla-app/internal/testutil"
)

func TestSubmitNewFile_AuthorEmailFollowsKeepEmailPrivate(t *testing.T) {
	router, db, reposRoot := newEmailPrivacyRouter(t)
	suffix := testutil.UniqueSuffix(t)
	userID := testutil.SeedUser(t, db, suffix)
	owner := "testuser_" + suffix
	repoName := "testrepo_" + suffix
	testutil.SeedRepo(t, db, userID, owner, suffix)
	gitRepo, err := gogit.PlainInit(filepath.Join(reposRoot, owner, repoName+".git"), true)
	if err != nil {
		t.Fatalf("init bare repo: %v", err)
	}
	token := makeIssueJWT(t, userID, owner)

	headAuthor := func(path string) string {
		t.Helper()
		rr := postForm(t, router, token, "/"+owner+"/"+repoName+"/new/main",
			url.Values{"path": {path}, "content": {"hello\n"}, "message": {"Add " + path}})
		if rr.Code != http.StatusSeeOther {
			t.Fatalf("commit %s: want 303, got %d: %s", path, rr.Code, rr.Body.String())
		}
		ref, err := gitRepo.Reference(plumbing.NewBranchReferenceName("main"), true)
		if err != nil {
			t.Fatalf("resolve main: %v", err)
		}
		c, err := gitRepo.CommitObject(ref.Hash())
		if err != nil {
			t.Fatalf("load head commit: %v", err)
		}
		if c.Author.Name != owner {
			t.Errorf("author name = %q, want %q", c.Author.Name, owner)
		}
		return c.Author.Email
	}

	wantNoreply := fmt.Sprintf("%d+%s@users.noreply.git.example.com", userID, owner)
	if got := headAuthor("private.txt"); got != wantNoreply {
		t.Errorf("setting on: author email = %q, want %q", got, wantNoreply)
	}

	saveEmailSettings(t, router, token, url.Values{})
	if got, want := headAuthor("public.txt"), owner+"@test.invalid"; got != want {
		t.Errorf("setting off: author email = %q, want %q", got, want)
	}
}

func TestSubmitNewFile_RefusesPathCollisions(t *testing.T) {
	db := testutil.OpenTestDB(t)
	reposRoot := t.TempDir()
	api := newAPIRouterAt(t, db, reposRoot)
	r := seedRaceRepo(t, db, reposRoot)
	if rr := postForm(t, api, r.owner.token, r.path+"/new/main", url.Values{"path": {"docs/guide.md"}, "content": {"guide\n"}}); rr.Code != http.StatusSeeOther {
		t.Fatalf("add docs/guide.md: want 303, got %d: %s", rr.Code, rr.Body.String())
	}

	tests := []struct{ name, path, want string }{
		{"file over a directory", "docs/", "path collides with an existing entry: docs is a directory\n"},
		{"directory over a file", "a.txt/x", "path collides with an existing entry: a.txt is a file\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tip := branchHash(t, r.git, "main")
			rr := postForm(t, api, r.owner.token, r.path+"/new/main", url.Values{"path": {tt.path}, "content": {"x\n"}})
			if rr.Code != http.StatusConflict || rr.Body.String() != tt.want {
				t.Errorf("want 409 %q, got %d %q", tt.want, rr.Code, rr.Body.String())
			}
			if got := branchHash(t, r.git, "main"); got != tip {
				t.Errorf("main = %s, want it left at %s", got, tip)
			}
		})
	}
}

func TestUpdateProfileReadme_AuthorEmailFollowsKeepEmailPrivate(t *testing.T) {
	router, db, reposRoot := newEmailPrivacyRouter(t)
	suffix := testutil.UniqueSuffix(t)
	userID := testutil.SeedUser(t, db, suffix)
	owner := "testuser_" + suffix
	if _, err := db.Exec(
		`INSERT INTO repositories (owner_id, owner_name, name, description, private, default_branch)
		 VALUES ($1, $2, $2, '', false, 'main')`, userID, owner); err != nil {
		t.Fatalf("seed profile repo: %v", err)
	}
	gitRepo, err := gogit.PlainInit(filepath.Join(reposRoot, owner, owner+".git"), true)
	if err != nil {
		t.Fatalf("init bare repo: %v", err)
	}
	token := makeIssueJWT(t, userID, owner)

	headAuthorEmail := func(content string) string {
		t.Helper()
		rr := postForm(t, router, token, "/settings/profile-readme", url.Values{"content": {content}})
		if rr.Code != http.StatusSeeOther || rr.Header().Get("Location") != "/"+owner {
			t.Fatalf("save README: want 303 to /%s, got %d %q: %s", owner, rr.Code, rr.Header().Get("Location"), rr.Body.String())
		}
		ref, err := gitRepo.Reference(plumbing.NewBranchReferenceName("main"), true)
		if err != nil {
			t.Fatalf("resolve main: %v", err)
		}
		c, err := gitRepo.CommitObject(ref.Hash())
		if err != nil {
			t.Fatalf("load head commit: %v", err)
		}
		return c.Author.Email
	}

	wantNoreply := fmt.Sprintf("%d+%s@users.noreply.git.example.com", userID, owner)
	if got := headAuthorEmail("# hi\n"); got != wantNoreply {
		t.Errorf("setting on: author email = %q, want %q", got, wantNoreply)
	}

	saveEmailSettings(t, router, token, url.Values{})
	if got, want := headAuthorEmail("# hello\n"), owner+"@test.invalid"; got != want {
		t.Errorf("setting off: author email = %q, want %q", got, want)
	}
}

func TestUpdateProfileReadme_RefusesAnArchivedProfileRepo(t *testing.T) {
	router, db, reposRoot := newEmailPrivacyRouter(t)
	suffix := testutil.UniqueSuffix(t)
	userID := testutil.SeedUser(t, db, suffix)
	owner := "testuser_" + suffix
	if _, err := db.Exec(
		`INSERT INTO repositories (owner_id, owner_name, name, description, private, default_branch, is_archived)
		 VALUES ($1, $2, $2, '', false, 'main', true)`, userID, owner); err != nil {
		t.Fatalf("seed profile repo: %v", err)
	}
	gitRepo, err := gogit.PlainInit(filepath.Join(reposRoot, owner, owner+".git"), true)
	if err != nil {
		t.Fatalf("init bare repo: %v", err)
	}
	token := makeIssueJWT(t, userID, owner)

	rr := postForm(t, router, token, "/settings/profile-readme", url.Values{"content": {"# hi\n"}})
	if want := "/" + owner + "?readme_error=profile_repo_archived"; rr.Code != http.StatusSeeOther || rr.Header().Get("Location") != want {
		t.Fatalf("want 303 to %s, got %d %q", want, rr.Code, rr.Header().Get("Location"))
	}
	if _, err := gitRepo.Reference(plumbing.NewBranchReferenceName("main"), true); err == nil {
		t.Error("an archived profile repo got a commit")
	}
}
