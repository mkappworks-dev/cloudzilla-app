package handler_test

// Integration tests: access to an org repo comes from the org and explicit
// repo roles, never from having created the repo. All tests require
// TEST_DATABASE_DSN and skip otherwise.

import (
	"context"
	"database/sql"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	gogit "github.com/go-git/go-git/v5"
	"github.com/mkappworks-dev/cloudzilla-app/internal/config"
	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/router"
	"github.com/mkappworks-dev/cloudzilla-app/internal/service"
	"github.com/mkappworks-dev/cloudzilla-app/internal/store"
	"github.com/mkappworks-dev/cloudzilla-app/internal/testutil"
)

type orgRepoEnv struct {
	db       *sql.DB
	router   http.Handler
	svc      *service.Services
	org      *model.Organization
	owner    signedInUser // an org owner who did not create the repo
	creator  signedInUser // an org owner who did
	repo     *model.Repository
	repoPath string
	head     string
}

// newOrgRepoEnv has creator make a private org repo with one commit.
func newOrgRepoEnv(t *testing.T) orgRepoEnv {
	t.Helper()
	ctx := context.Background()
	db := testutil.OpenTestDB(t)
	root := t.TempDir()
	cfg := &config.Config{
		Server: config.ServerConfig{BaseURL: "http://localhost:8080"},
		Auth:   config.AuthConfig{JWTSecret: testJWTSecret, JWTExpiry: 24 * time.Hour, CookieName: testCookieName},
		Git:    config.GitConfig{ReposRoot: root},
	}
	svc := service.New(store.New(db), cfg)
	owner := seedSignedInUser(t, db)
	creator := seedSignedInUser(t, db)
	org, err := svc.Org.Create(ctx, owner.id, "testorg_"+testutil.UniqueSuffix(t), "", "")
	if err != nil {
		t.Fatalf("create org: %v", err)
	}
	testutil.DeleteOrgOnCleanup(t, db, org.ID)
	if err := svc.Org.AddMember(ctx, org.ID, owner.id, creator.id, model.OrgRoleOwner); err != nil {
		t.Fatalf("add creator as owner: %v", err)
	}
	repo, err := svc.Org.CreateRepo(ctx, org.ID, creator.id, "vault", "", true, service.RepoInitOptions{AddREADME: true})
	if err != nil {
		t.Fatalf("create org repo: %v", err)
	}
	gitRepo, err := gogit.PlainOpen(filepath.Join(root, org.Name, "vault.git"))
	if err != nil {
		t.Fatalf("open org repo: %v", err)
	}
	head, err := gitRepo.Head()
	if err != nil {
		t.Fatalf("org repo head: %v", err)
	}
	h, err := router.New(svc, cfg, fstest.MapFS{})
	if err != nil {
		t.Fatalf("router.New: %v", err)
	}
	return orgRepoEnv{
		db: db, router: h, svc: svc, org: org,
		owner: owner, creator: creator, repo: repo,
		repoPath: "/" + org.Name + "/vault", head: head.Hash().String(),
	}
}

func (e orgRepoEnv) gitRefs(t *testing.T, gitService, token string) (int, string) {
	t.Helper()
	rr := requestAPI(e.router, http.MethodGet, e.repoPath+"/info/refs?service="+gitService, token)
	return rr.Code, rr.Body.String()
}

// wantNoAccess checks that git, the web page and the JSON API all treat the
// repo as missing for token's user.
func (e orgRepoEnv) wantNoAccess(t *testing.T, token string) {
	t.Helper()
	if code, body := e.gitRefs(t, "git-upload-pack", token); code != http.StatusUnauthorized || strings.Contains(body, e.head) {
		t.Errorf("git fetch: want 401 without the head, got %d (head advertised: %v)", code, strings.Contains(body, e.head))
	}
	if code, _ := e.gitRefs(t, "git-receive-pack", token); code != http.StatusUnauthorized {
		t.Errorf("git push: want 401, got %d", code)
	}
	if rr := requestAPI(e.router, http.MethodGet, e.repoPath, token); rr.Code != http.StatusNotFound {
		t.Errorf("web page: want 404, got %d", rr.Code)
	}
	if rr := requestAPI(e.router, http.MethodGet, "/api/repos"+e.repoPath, token); rr.Code != http.StatusNotFound || rr.Body.String() != missingRepoBody {
		t.Errorf("API get: want the missing-repo 404, got %d %q", rr.Code, rr.Body.String())
	}
	if rr := requestAPI(e.router, http.MethodPost, "/api/repos"+e.repoPath+"/archive", token); rr.Code != http.StatusNotFound {
		t.Errorf("API archive: want 404, got %d", rr.Code)
	}
	var archived bool
	if err := e.db.QueryRow(`SELECT is_archived FROM repositories WHERE id = $1`, e.repo.ID).Scan(&archived); err != nil || archived {
		t.Errorf("repo archived by a user without access (err %v)", err)
	}
}

func TestOrgRepo_CreatorRemovedFromOrg_LosesAccess(t *testing.T) {
	env := newOrgRepoEnv(t)
	ctx := context.Background()
	if code, body := env.gitRefs(t, "git-upload-pack", env.creator.token); code != http.StatusOK || !strings.Contains(body, env.head) {
		t.Fatalf("creator as org owner: want git fetch 200 with the head, got %d", code)
	}

	if err := env.svc.Org.RemoveMember(ctx, env.org.ID, env.owner.id, env.creator.id); err != nil {
		t.Fatalf("remove creator: %v", err)
	}

	env.wantNoAccess(t, env.creator.token)
	for name, got := range map[string]bool{
		"CanWrite":  env.svc.Repo.CanWrite(ctx, env.repo, env.creator.id),
		"CanManage": env.svc.Repo.CanManage(ctx, env.repo, env.creator.id),
		"IsOwner":   env.svc.Repo.IsOwner(ctx, env.repo, env.creator.id),
	} {
		if got {
			t.Errorf("%s = true for a creator no longer in the org", name)
		}
	}
	if code, body := env.gitRefs(t, "git-upload-pack", env.owner.token); code != http.StatusOK || !strings.Contains(body, env.head) {
		t.Errorf("remaining org owner: want git fetch 200 with the head, got %d", code)
	}
}

// A member holds no implicit role on private org repos; an explicit one
// grants exactly what it says.
func TestOrgRepo_CreatorDemotedToMember_KeepsOnlyExplicitRole(t *testing.T) {
	env := newOrgRepoEnv(t)
	ctx := context.Background()
	if err := env.svc.Org.UpdateMemberRole(ctx, env.org.ID, env.owner.id, env.creator.id, model.OrgRoleMember); err != nil {
		t.Fatalf("demote creator: %v", err)
	}

	env.wantNoAccess(t, env.creator.token)

	if err := env.svc.Repo.AddCollaborator(ctx, env.repo.ID, env.creator.name, string(model.RoleReader)); err != nil {
		t.Fatalf("grant reader: %v", err)
	}
	if code, body := env.gitRefs(t, "git-upload-pack", env.creator.token); code != http.StatusOK || !strings.Contains(body, env.head) {
		t.Errorf("reader: want git fetch 200 with the head, got %d", code)
	}
	if code, _ := env.gitRefs(t, "git-receive-pack", env.creator.token); code != http.StatusUnauthorized {
		t.Errorf("reader: want git push 401, got %d", code)
	}
	if rr := requestAPI(env.router, http.MethodPost, "/api/repos"+env.repoPath+"/archive", env.creator.token); rr.Code != http.StatusForbidden {
		t.Errorf("reader: want archive 403, got %d", rr.Code)
	}
}

// The account-wide issue, pull and activity lists filter repos in SQL rather
// than through RepoService, so they must drop the creator too.
func TestOrgRepo_CreatorRemovedFromOrg_LeavesAccountLists(t *testing.T) {
	env := newOrgRepoEnv(t)
	ctx := context.Background()
	testutil.Exec(t, env.db, `INSERT INTO issues (repo_id, number, author_id, title) VALUES ($1, 1, $2, 'vault issue')`, env.repo.ID, env.creator.id)
	testutil.Exec(t, env.db, `INSERT INTO pull_requests (repo_id, number, author_id, title, head_branch) VALUES ($1, 2, $2, 'vault pull', 'topic')`, env.repo.ID, env.creator.id)
	testutil.Exec(t, env.db, `INSERT INTO issues (repo_id, number, author_id, title) VALUES ($1, 3, $2, 'assigned vault issue')`, env.repo.ID, env.owner.id)
	testutil.Exec(t, env.db, `INSERT INTO issue_assignees (issue_id, user_id) SELECT id, $2 FROM issues WHERE repo_id = $1 AND number = 3`, env.repo.ID, env.creator.id)
	testutil.Exec(t, env.db, `INSERT INTO pull_assignees (pull_id, user_id) SELECT id, $2 FROM pull_requests WHERE repo_id = $1`, env.repo.ID, env.creator.id)
	repoID := env.repo.ID
	env.svc.Event.Record(ctx, env.owner.id, env.owner.name, &repoID, "vault", env.org.Name, model.EventPush, map[string]any{"branch": "main"})
	t.Cleanup(func() {
		testutil.Exec(t, env.db, `DELETE FROM events WHERE repo_id = $1`, repoID)
		testutil.Exec(t, env.db, `DELETE FROM issues WHERE repo_id = $1`, repoID)
		testutil.Exec(t, env.db, `DELETE FROM pull_assignees WHERE pull_id IN (SELECT id FROM pull_requests WHERE repo_id = $1)`, repoID)
		testutil.Exec(t, env.db, `DELETE FROM pull_requests WHERE repo_id = $1`, repoID)
	})

	lists := func(userID int64) (issues, pulls, events, attention int) {
		t.Helper()
		is, err := env.svc.Issue.ListForUser(ctx, userID, "created", "open")
		if err != nil {
			t.Fatalf("list issues: %v", err)
		}
		for _, it := range is {
			if it.RepoFullName != env.org.Name+"/vault" {
				t.Errorf("issue listed under %q, want %s/vault", it.RepoFullName, env.org.Name)
			}
		}
		ps, err := env.svc.Pull.ListForUser(ctx, userID, "created", "open")
		if err != nil {
			t.Fatalf("list pulls: %v", err)
		}
		evs, err := env.svc.Event.Feed(ctx, int(userID), "all", 1, 100)
		if err != nil {
			t.Fatalf("feed: %v", err)
		}
		for _, ev := range evs {
			if ev.RepoID != nil && *ev.RepoID == repoID {
				events++
			}
		}
		items, err := env.svc.Attention.ForUser(ctx, userID)
		if err != nil {
			t.Fatalf("attention: %v", err)
		}
		for _, it := range items {
			if it.RepoName == env.org.Name+"/vault" {
				attention++
			}
		}
		return len(is), len(ps), events, attention
	}
	assignedCounts := func(userID int64) (issues, pulls int) {
		t.Helper()
		issues, err := env.svc.Issue.CountOpenAssignedTo(ctx, userID)
		if err != nil {
			t.Fatalf("count assigned issues: %v", err)
		}
		pulls, err = env.svc.Pull.CountOpenAssignedTo(ctx, userID)
		if err != nil {
			t.Fatalf("count assigned pulls: %v", err)
		}
		return issues, pulls
	}
	if issues, pulls, events, attention := lists(env.creator.id); issues != 1 || pulls != 1 || events != 1 || attention != 1 {
		t.Fatalf("creator as org owner: want 1 issue, pull, event and assigned issue listed, got %d, %d, %d and %d", issues, pulls, events, attention)
	}
	if issues, pulls := assignedCounts(env.creator.id); issues != 1 || pulls != 1 {
		t.Fatalf("creator as org owner: want 1 assigned issue and pull counted, got %d and %d", issues, pulls)
	}

	if err := env.svc.Org.RemoveMember(ctx, env.org.ID, env.owner.id, env.creator.id); err != nil {
		t.Fatalf("remove creator: %v", err)
	}

	if issues, pulls, events, attention := lists(env.creator.id); issues != 0 || pulls != 0 || events != 0 || attention != 0 {
		t.Errorf("creator outside the org still lists %d issues, %d pulls, %d events, %d attention items from the private org repo", issues, pulls, events, attention)
	}
	if issues, pulls := assignedCounts(env.creator.id); issues != 0 || pulls != 0 {
		t.Errorf("creator outside the org still counts %d assigned issues and %d assigned pulls", issues, pulls)
	}
	issueCounts, err := env.svc.Issue.CountsForUser(ctx, env.creator.id)
	if err != nil {
		t.Fatalf("issue counts: %v", err)
	}
	pullCounts, err := env.svc.Pull.CountsForUser(ctx, env.creator.id)
	if err != nil {
		t.Fatalf("pull counts: %v", err)
	}
	for kind, counts := range map[string]map[string]int{"issue": issueCounts, "pull": pullCounts} {
		for key, n := range counts {
			if n != 0 {
				t.Errorf("%s count %s = %d after leaving the org", kind, key, n)
			}
		}
	}
}
