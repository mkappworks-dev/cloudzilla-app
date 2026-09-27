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
