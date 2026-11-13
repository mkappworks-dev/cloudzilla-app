package handler_test

import (
	"encoding/json"
	"net/http"
	"path/filepath"
	"strings"
	"testing"

	gogit "github.com/go-git/go-git/v5"

	"github.com/mkappworks-dev/cloudzilla-app/internal/config"
	"github.com/mkappworks-dev/cloudzilla-app/internal/service"
	"github.com/mkappworks-dev/cloudzilla-app/internal/testutil"
)

func TestMergePull_FromFork_WritesOnlyTheBase(t *testing.T) {
	for _, strategy := range []string{"ff", "merge", "squash"} {
		t.Run(strategy, func(t *testing.T) {
			db := testutil.OpenTestDB(t)
			root := t.TempDir()
			api := newAPIRouterAt(t, db, root)
			up := seedOwnedRepo(t, db, false)
			upGit, err := gogit.PlainInit(filepath.Join(root, up.owner.name, up.name+".git"), true)
			if err != nil {
				t.Fatal(err)
			}
			code := service.NewCodeService(config.GitConfig{ReposRoot: root})
			if _, err := code.CommitFile(up.owner.name, up.name, "main", "a.txt", []byte("one\n"), raceAuthor, "Add a"); err != nil {
				t.Fatal(err)
			}

			forker := seedSignedInUser(t, db)
			rr := requestAPIBody(api, http.MethodPost, "/api/repos"+up.path+"/fork", forker.token, "{}")
			if rr.Code != http.StatusCreated && rr.Code != http.StatusOK {
				t.Fatalf("fork: %d %s", rr.Code, rr.Body.String())
			}
			var fork struct {
				Name      string `json:"name"`
				OwnerName string `json:"owner_name"`
			}
			if err := json.Unmarshal(rr.Body.Bytes(), &fork); err != nil || fork.Name == "" {
				t.Fatalf("fork response %q: %v", rr.Body.String(), err)
			}
			if err := code.CreateBranch(forker.name, fork.Name, "fix-x", "main"); err != nil {
				t.Fatal(err)
			}
			if _, err := code.CommitFile(forker.name, fork.Name, "fix-x", "b.txt", []byte("two\n"), raceAuthor, "Add b"); err != nil {
				t.Fatal(err)
			}
			forkGit, err := gogit.PlainOpen(filepath.Join(root, forker.name, fork.Name+".git"))
			if err != nil {
				t.Fatal(err)
			}
			forkTip := branchHash(t, forkGit, "fix-x")
			forkMain := branchHash(t, forkGit, "main")

			rr = requestAPIBody(api, http.MethodPost, "/api/repos"+up.path+"/pulls", forker.token,
				`{"title":"Add b","head_branch":"fix-x","base_branch":"main","head_repo":"`+forker.name+`/`+fork.Name+`"}`)
			if rr.Code != http.StatusCreated {
				t.Fatalf("open pull: %d %s", rr.Code, rr.Body.String())
			}
			rr = requestAPIBody(api, http.MethodPatch, "/api/repos"+up.path+"/pulls/1", up.owner.token,
				`{"state":"merged","merge_strategy":"`+strategy+`"}`)
			if rr.Code >= 300 {
				t.Fatalf("merge: %d %s", rr.Code, rr.Body.String())
			}

			var state, mergedHead string
			if err := db.QueryRow(`SELECT state, COALESCE(merged_head_sha, '') FROM pull_requests WHERE repo_id = $1 AND number = 1`, up.id).Scan(&state, &mergedHead); err != nil {
				t.Fatal(err)
			}
			if state != "merged" {
				t.Errorf("state = %q, want merged", state)
			}
			if mergedHead != forkTip.String() {
				t.Errorf("merged_head_sha = %q, want the fork's tip %s", mergedHead, forkTip)
			}
			if _, err := code.GetBlob(up.owner.name, up.name, "main", "b.txt"); err != nil {
				t.Errorf("base main lacks the fork's file after a %s merge: %v", strategy, err)
			}
			if tip, err := upGit.CommitObject(branchHash(t, upGit, "main")); err != nil {
				t.Fatal(err)
			} else if strings.Contains(tip.Message, "refs/pull/") {
				t.Errorf("merge message %q names the internal head ref", tip.Message)
			}
			if got := branchHash(t, forkGit, "fix-x"); got != forkTip {
				t.Errorf("fork fix-x moved: %s, want %s", got, forkTip)
			}
			if got := branchHash(t, forkGit, "main"); got != forkMain {
				t.Errorf("fork main moved: %s, want %s", got, forkMain)
			}
		})
	}
}
