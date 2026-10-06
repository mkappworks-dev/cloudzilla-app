package router_test

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/mkappworks-dev/cloudzilla-app/internal/service"
	"github.com/mkappworks-dev/cloudzilla-app/internal/testutil"
)

func TestDiffPages_HighlightTheirCode(t *testing.T) {
	h, svc, db := newTestRouter(t)
	suffix := testutil.UniqueSuffix(t)
	ownerID := testutil.SeedUser(t, db, suffix)
	owner := "testuser_" + suffix
	repoName := "hl_" + suffix
	repo, err := svc.Repo.Create(context.Background(), ownerID, owner, repoName, "", false, service.RepoInitOptions{})
	if err != nil {
		t.Fatalf("Repo.Create: %v", err)
	}
	author := service.GitAuthor{Name: owner, Email: owner + "@test.invalid"}
	commit := func(branch, src, msg string) {
		t.Helper()
		if err := svc.Code.CommitFile(owner, repoName, branch, "main.go", []byte(src), author, msg); err != nil {
			t.Fatalf("CommitFile %s: %v", branch, err)
		}
	}
	commit("main", "package main\n\nfunc main() {}\n", "base")
	if err := svc.Code.CreateBranch(owner, repoName, "feature", "main"); err != nil {
		t.Fatalf("CreateBranch: %v", err)
	}
	commit("feature", "package main\n\nfunc main() { run() }\n", "head")
	seedOpenPull(t, db, repo.ID, ownerID, 1)

	log, err := svc.Code.GetCommits(owner, repoName, "feature", 1, 1)
	if err != nil || len(log.Commits) == 0 {
		t.Fatalf("GetCommits: %v, %+v", err, log)
	}
	token := makeJWT(t, ownerID, owner)

	base := "/" + owner + "/" + repoName
	for name, path := range map[string]string{
		"commit page":   base + "/commit/" + log.Commits[0].FullHash,
		"PR files page": base + "/pulls/1/files",
	} {
		rr := serve(h, browserRequest(http.MethodGet, path, token, nil))
		if rr.Code != http.StatusOK {
			t.Fatalf("%s: GET %s = %d, want 200", name, path, rr.Code)
		}
		body := rr.Body.String()
		if !strings.Contains(body, `class="hl overflow-x-auto"`) || !strings.Contains(body, `<span class="hl-k`) {
			t.Errorf("%s: diff lines are not highlighted", name)
		}
	}
}
