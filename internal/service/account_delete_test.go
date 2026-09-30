package service_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/mkappworks-dev/cloudzilla-app/internal/config"
	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/service"
	"github.com/mkappworks-dev/cloudzilla-app/internal/store"
	"github.com/mkappworks-dev/cloudzilla-app/internal/testutil"
)

func (e repoDirsEnv) users() *service.UserService {
	return service.NewUserService(store.NewUserStore(e.db), config.AuthConfig{JWTSecret: "test-secret-32bytes-minimum-len!"}).
		WithRepoService(e.repos)
}

func (e repoDirsEnv) userExists(t *testing.T, id int64) bool {
	t.Helper()
	var n int
	if err := e.db.QueryRow(`SELECT COUNT(*) FROM users WHERE id = $1`, id).Scan(&n); err != nil {
		t.Fatalf("count users: %v", err)
	}
	return n == 1
}

func (e repoDirsEnv) insertID(t *testing.T, query string, args ...any) int64 {
	t.Helper()
	var id int64
	if err := e.db.QueryRow(query, args...).Scan(&id); err != nil {
		t.Fatalf("%s: %v", query, err)
	}
	return id
}

func (e repoDirsEnv) ghost(t *testing.T) (int64, string) {
	t.Helper()
	var id int64
	var username string
	if err := e.db.QueryRow(`SELECT id, username FROM users WHERE id = ghost_user_id()`).Scan(&id, &username); err != nil {
		t.Fatalf("load ghost: %v", err)
	}
	return id, username
}

func (e repoDirsEnv) seedPull(t *testing.T, repoID, authorID int64) int64 {
	t.Helper()
	return e.insertID(t, `INSERT INTO pull_requests (repo_id, number, author_id, title, head_branch) VALUES ($1, 1, $2, 'fix', 'fix') RETURNING id`, repoID, authorID)
}

func TestUserService_DeleteUser_RemovesRepoDirs(t *testing.T) {
	env := newRepoDirsEnv(t)
	ctx := context.Background()
	userID, user := env.seedUser(t)
	env.createWithWiki(t, user, "live")
	gone := env.createWithWiki(t, user, "gone")
	if err := env.repos.Delete(ctx, gone, userID); err != nil {
		t.Fatalf("soft delete: %v", err)
	}

	if err := env.users().DeleteUser(ctx, userID); err != nil {
		t.Fatalf("DeleteUser: %v", err)
	}

	if env.userExists(t, userID) {
		t.Fatal("user row survived")
	}
	entries, err := os.ReadDir(filepath.Join(env.root, user))
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("read owner dir: %v", err)
	}
	for _, e := range entries {
		t.Errorf("%s/%s survived the account", user, e.Name())
	}
}

func TestUserService_DeleteUser_RemovesAStrandedWiki(t *testing.T) {
	env := newRepoDirsEnv(t)
	userID, user := env.seedUser(t)
	repoID := env.createWithWiki(t, user, "old")
	env.strandWiki(t, repoID, user, "old", time.Hour)

	if err := env.users().DeleteUser(context.Background(), userID); err != nil {
		t.Fatalf("DeleteUser: %v", err)
	}
	if _, wikiDir := env.dirs(user, "old"); pathExists(wikiDir) {
		t.Error("stranded wiki survived the account")
	}
}

func TestRepoService_DeleteWithOwner_FailedOwnerDeleteKeepsRepos(t *testing.T) {
	env := newRepoDirsEnv(t)
	ctx := context.Background()
	userID, user := env.seedUser(t)
	env.createWithWiki(t, user, "kept")
	gitDir, wikiDir := env.dirs(user, "kept")
	head := headOf(t, gitDir)

	err := env.repos.DeleteWithOwner(ctx, userID, func([]int64) error { return errors.New("row delete failed") })
	if err == nil {
		t.Fatal("DeleteWithOwner succeeded although the owner delete failed")
	}

	if !pathExists(gitDir) || headOf(t, gitDir) != head {
		t.Error("repo not back at its path after the failed delete")
	}
	if _, found, _ := env.code.WikiPageGet(user, "kept", "Home"); !found {
		t.Errorf("wiki not back at %s after the failed delete", wikiDir)
	}
	if matches, _ := filepath.Glob(filepath.Join(env.root, user, "*.deleted.*")); len(matches) != 0 {
		t.Errorf("moved-aside copies left after the failed delete: %v", matches)
	}
}

func TestUserService_DeleteUser_KeepsOrgReposItCreated(t *testing.T) {
	env := newRepoDirsEnv(t)
	ctx := context.Background()
	userID, user := env.seedUser(t)
	coOwnerID, _ := env.seedUser(t)
	org := env.coOwnedOrg(t, coOwnerID, userID)
	env.createWithWiki(t, user, "mine")
	kept, err := env.orgs.CreateRepo(ctx, org.ID, userID, "kept", "", true, service.RepoInitOptions{AddREADME: true})
	if err != nil {
		t.Fatalf("CreateRepo: %v", err)
	}
	if err := env.code.WikiPageSave(org.Name, "kept", "Home", "org wiki", dirsTestAuthor, ""); err != nil {
		t.Fatalf("save org wiki: %v", err)
	}
	keptGit, _ := env.dirs(org.Name, "kept")
	keptHead := headOf(t, keptGit)
	binned, err := env.orgs.CreateRepo(ctx, org.ID, userID, "binned", "", true, service.RepoInitOptions{AddREADME: true})
	if err != nil {
		t.Fatalf("CreateRepo: %v", err)
	}
	binnedGit, _ := env.dirs(org.Name, "binned")
	binnedHead := headOf(t, binnedGit)
	if err := env.repos.Delete(ctx, binned.ID, userID); err != nil {
		t.Fatalf("soft delete org repo: %v", err)
	}
	if got, err := env.repos.GetByID(ctx, kept.ID); err != nil || got.CreatedBy != userID || got.OwnerID != 0 {
		t.Fatalf("new org repo: created_by %d, owner_id %d (err %v); want created_by %d and no owner_id", got.CreatedBy, got.OwnerID, err, userID)
	}

	if err := env.users().DeleteUser(ctx, userID); err != nil {
		t.Fatalf("DeleteUser: %v", err)
	}

	if env.userExists(t, userID) {
		t.Fatal("user row survived")
	}
	if matches, _ := filepath.Glob(filepath.Join(env.root, user, "mine*")); len(matches) != 0 {
		t.Errorf("personal repo dirs survived the account: %v", matches)
	}
	got, err := env.repos.Get(ctx, org.Name, "kept")
	if err != nil {
		t.Fatalf("org repo row gone: %v", err)
	}
	if got.OrgID != org.ID || got.CreatedBy != 0 {
		t.Errorf("org repo after its creator's deletion: org_id %d created_by %d, want %d and 0", got.OrgID, got.CreatedBy, org.ID)
	}
	env.wantLive(t, org.Name, "kept", keptHead, "org wiki")
	if err := env.repos.Restore(ctx, binned.ID, coOwnerID, false); err != nil {
		t.Fatalf("co-owner restoring the org repo its deleted creator binned: %v", err)
	}
	if headOf(t, binnedGit) != binnedHead {
		t.Error("restored org repo lost its history")
	}
}

func TestUserService_DeleteUser_RefusedWhileSoleOrgOwner(t *testing.T) {
	env := newRepoDirsEnv(t)
	ctx := context.Background()
	userID, user := env.seedUser(t)
	memberID, _ := env.seedUser(t)
	org := env.createOrg(t, userID)
	if err := env.orgs.AddMember(ctx, org.ID, userID, memberID, model.OrgRoleMember); err != nil {
		t.Fatalf("AddMember: %v", err)
	}
	env.createWithWiki(t, user, "mine")
	mineGit, _ := env.dirs(user, "mine")
	if _, err := env.orgs.CreateRepo(ctx, org.ID, userID, "orgrepo", "", true, service.RepoInitOptions{}); err != nil {
		t.Fatalf("CreateRepo: %v", err)
	}

	if err := env.users().DeleteUser(ctx, userID); !errors.Is(err, service.ErrSoleOrgOwner) {
		t.Fatalf("want ErrSoleOrgOwner, got %v", err)
	}
	if !env.userExists(t, userID) {
		t.Fatal("sole org owner deleted")
	}
	if !pathExists(mineGit) {
		t.Error("personal repo moved by a refused delete")
	}
	// A plain member is not an owner the org depends on.
	if err := env.users().DeleteUser(ctx, memberID); err != nil {
		t.Errorf("DeleteUser of an org member: %v", err)
	}

	coOwnerID, _ := env.seedUser(t)
	if err := env.orgs.AddMember(ctx, org.ID, userID, coOwnerID, model.OrgRoleOwner); err != nil {
		t.Fatalf("AddMember: %v", err)
	}
	if err := env.users().DeleteUser(ctx, userID); err != nil {
		t.Fatalf("DeleteUser with a second owner: %v", err)
	}
	if !env.orgs.IsOwner(ctx, org.ID, coOwnerID) {
		t.Error("remaining owner lost the org")
	}
	if _, err := env.repos.Get(ctx, org.Name, "orgrepo"); err != nil {
		t.Errorf("org repo gone with its creator: %v", err)
	}
}

// The pre-check saw a second owner; the locked re-check must see them leave.
func TestUserService_DeleteUser_LastCoOwnerLeavingMidDeleteAbortsIt(t *testing.T) {
	env := newRepoDirsEnv(t)
	ctx := context.Background()
	users := store.NewUserStore(env.db)
	userID, user := env.seedUser(t)
	coOwnerID, _ := env.seedUser(t)
	org := env.coOwnedOrg(t, userID, coOwnerID)
	env.createWithWiki(t, user, "mine")

	err := env.repos.DeleteWithOwner(ctx, userID, func(live []int64) error {
		if err := env.orgs.RemoveMember(ctx, org.ID, coOwnerID, coOwnerID); err != nil {
			t.Fatalf("co-owner leaving: %v", err)
		}
		return users.DeleteWithOwnedRepos(ctx, userID, live)
	})
	if !errors.Is(err, store.ErrLastOrgOwner) {
		t.Fatalf("want ErrLastOrgOwner, got %v", err)
	}
	if !env.userExists(t, userID) || !env.orgs.IsOwner(ctx, org.ID, userID) {
		t.Fatal("org left without an owner")
	}
	if gitDir, _ := env.dirs(user, "mine"); !pathExists(gitDir) {
		t.Error("personal repo not restored after the refused delete")
	}
}

// Deleting a co-owner must neither remove nor strand the org's copies of a
// name, whoever created or deleted them.
func TestUserService_DeleteUser_KeepsDeletedOrgReposRestorable(t *testing.T) {
	env := newRepoDirsEnv(t)
	ctx := context.Background()
	c := env.twoDeletedCopies(t, time.Hour)

	if err := env.users().DeleteUser(ctx, c.firstID); err != nil {
		t.Fatalf("DeleteUser: %v", err)
	}

	gitDir, _ := env.dirs(c.org.Name, "x")
	if matches, _ := filepath.Glob(gitDir + ".deleted.*"); len(matches) != 2 {
		t.Errorf("want both copies kept, got %v", matches)
	}
	if err := env.repos.Restore(ctx, c.first, c.secondID, false); err != nil {
		t.Fatalf("Restore of the deleted user's copy: %v", err)
	}
	env.wantLive(t, c.org.Name, "x", c.firstHead, "wiki of copy 0")
}

func TestUserService_DeleteUser_OwnIssuesAndPullsDoNotBlock(t *testing.T) {
	env := newRepoDirsEnv(t)
	userID, user := env.seedUser(t)
	repoID := env.createWithWiki(t, user, "tracked")
	testutil.Exec(t, env.db, `INSERT INTO issues (repo_id, number, title, author_id) VALUES ($1, 1, 'bug', $2)`, repoID, userID)
	testutil.Exec(t, env.db, `INSERT INTO pull_requests (repo_id, number, title, author_id, head_branch) VALUES ($1, 2, 'fix', $2, 'fix')`, repoID, userID)

	if err := env.users().DeleteUser(context.Background(), userID); err != nil {
		t.Fatalf("DeleteUser: %v", err)
	}
	if env.userExists(t, userID) {
		t.Fatal("user row survived")
	}
}

func TestUserService_DeleteUser_HandsContentInOthersReposToTheGhost(t *testing.T) {
	env := newRepoDirsEnv(t)
	userID, user := env.seedUser(t)
	env.createWithWiki(t, user, "mine")
	otherID, other := env.seedUser(t)
	repoID := env.createWithWiki(t, other, "theirs")
	gone := env.createWithWiki(t, other, "gone")
	testutil.Exec(t, env.db, `UPDATE repositories SET deleted_at = NOW(), deleted_by = $1 WHERE id = $2`, userID, gone)
	issueID := env.insertID(t, `INSERT INTO issues (repo_id, number, author_id, title) VALUES ($1, 1, $2, 'report') RETURNING id`, repoID, userID)
	pullID := env.insertID(t, `INSERT INTO pull_requests (repo_id, number, author_id, title, head_branch) VALUES ($1, 2, $2, 'fix', 'fix') RETURNING id`, repoID, userID)
	discussionID := env.insertID(t,
		`INSERT INTO discussions (repo_id, category_id, number, title, author_id, author_name)
		 VALUES ($1, (SELECT MIN(id) FROM discussion_categories), 3, 'idea', $2, $3) RETURNING id`,
		repoID, userID, user)
	invitationID := env.insertID(t,
		`INSERT INTO invitations (token, email, invited_by_id, expires_at) VALUES ($1, $2, $3, NOW() + INTERVAL '1 day') RETURNING id`,
		"tok_"+user, "invitee_"+user+"@test.invalid", userID)
	t.Cleanup(func() { testutil.Exec(t, env.db, `DELETE FROM invitations WHERE id = $1`, invitationID) })

	authored := []struct {
		table, idCol, nameCol string
		id                    int64
	}{
		{"issues", "author_id", "", issueID},
		{"pull_requests", "author_id", "", pullID},
		{"comments", "author_id", "author_name", env.insertID(t,
			`INSERT INTO comments (repo_id, issue_id, author_id, author_name, body) VALUES ($1, $2, $3, $4, 'me too') RETURNING id`,
			repoID, issueID, userID, user)},
		{"notifications", "actor_id", "actor_name", env.insertID(t,
			`INSERT INTO notifications (user_id, actor_id, actor_name, type, repo_id, subject_id) VALUES ($1, $2, $3, 'issue_comment', $4, 1) RETURNING id`,
			otherID, userID, user, repoID)},
		{"invitations", "invited_by_id", "", invitationID},
		{"releases", "author_id", "", env.insertID(t,
			`INSERT INTO releases (repo_id, tag_name, author_id) VALUES ($1, 'v1', $2) RETURNING id`, repoID, userID)},
		{"commit_statuses", "creator_id", "", env.insertID(t,
			`INSERT INTO commit_statuses (repo_id, sha, state, creator_id) VALUES ($1, 'abc', 'success', $2) RETURNING id`, repoID, userID)},
		{"pull_reviews", "author_id", "author_name", env.insertID(t,
			`INSERT INTO pull_reviews (pull_id, repo_id, author_id, author_name, state, body) VALUES ($1, $2, $3, $4, 'commented', 'looks off') RETURNING id`,
			pullID, repoID, userID, user)},
		{"pull_line_comments", "author_id", "author_name", env.insertID(t,
			`INSERT INTO pull_line_comments (pull_id, repo_id, author_id, author_name, path, line, body) VALUES ($1, $2, $3, $4, 'a.go', 1, 'nit') RETURNING id`,
			pullID, repoID, userID, user)},
		{"discussions", "author_id", "author_name", discussionID},
		{"discussion_replies", "author_id", "author_name", env.insertID(t,
			`INSERT INTO discussion_replies (discussion_id, author_id, author_name, body) VALUES ($1, $2, $3, '+1') RETURNING id`,
			discussionID, userID, user)},
		{"pull_events", "actor_id", "actor_name", env.insertID(t,
			`INSERT INTO pull_events (pull_id, actor_id, actor_name, event_type) VALUES ($1, $2, $3, 'closed') RETURNING id`,
			pullID, userID, user)},
		{"repositories", "deleted_by", "", gone},
	}

	if err := env.users().DeleteUser(context.Background(), userID); err != nil {
		t.Fatalf("DeleteUser: %v", err)
	}

	if env.userExists(t, userID) {
		t.Fatal("user row survived")
	}
	ghostID, ghostName := env.ghost(t)
	for _, a := range authored {
		cols, gotID, gotName := a.idCol, int64(0), ghostName
		dest := []any{&gotID}
		if a.nameCol != "" {
			cols += ", " + a.nameCol
			dest = append(dest, &gotName)
		}
		if err := env.db.QueryRow(`SELECT `+cols+` FROM `+a.table+` WHERE id = $1`, a.id).Scan(dest...); err != nil {
			t.Errorf("%s row %d: %v", a.table, a.id, err)
			continue
		}
		if gotID != ghostID || gotName != ghostName {
			t.Errorf("%s row has %s = %d, name %q; want the ghost (%d, %q)", a.table, a.idCol, gotID, gotName, ghostID, ghostName)
		}
	}
}

// A review request is addressed to the user, not written by them, and the
// ghost can never answer it.
func TestUserService_DeleteUser_DropsItsPendingReviewRequests(t *testing.T) {
	env := newRepoDirsEnv(t)
	userID, user := env.seedUser(t)
	otherID, other := env.seedUser(t)
	repoID := env.createWithWiki(t, other, "theirs")
	pullID := env.seedPull(t, repoID, otherID)
	requestID := env.insertID(t,
		`INSERT INTO pull_reviews (pull_id, repo_id, author_id, author_name, state) VALUES ($1, $2, $3, $4, 'pending') RETURNING id`,
		pullID, repoID, userID, user)

	if err := env.users().DeleteUser(context.Background(), userID); err != nil {
		t.Fatalf("DeleteUser: %v", err)
	}

	var n int
	if err := env.db.QueryRow(`SELECT COUNT(*) FROM pull_reviews WHERE id = $1`, requestID).Scan(&n); err != nil || n != 0 {
		t.Errorf("pending review request count = %d, %v; want it dropped", n, err)
	}
}

func TestUserService_DeleteUser_GhostKeepsEveryDeletedReviewersReview(t *testing.T) {
	env := newRepoDirsEnv(t)
	otherID, other := env.seedUser(t)
	repoID := env.createWithWiki(t, other, "theirs")
	pullID := env.seedPull(t, repoID, otherID)

	for _, state := range []string{"approved", "changes_requested"} {
		reviewerID, reviewer := env.seedUser(t)
		testutil.Exec(t, env.db,
			`INSERT INTO pull_reviews (pull_id, repo_id, author_id, author_name, state) VALUES ($1, $2, $3, $4, $5)`,
			pullID, repoID, reviewerID, reviewer, state)
		if err := env.users().DeleteUser(context.Background(), reviewerID); err != nil {
			t.Fatalf("DeleteUser of the %s reviewer: %v", state, err)
		}
	}

	ghostID, _ := env.ghost(t)
	var n int
	if err := env.db.QueryRow(`SELECT COUNT(*) FROM pull_reviews WHERE pull_id = $1 AND author_id = $2`, pullID, ghostID).Scan(&n); err != nil || n != 2 {
		t.Errorf("ghost reviews on the pull = %d, %v; want both", n, err)
	}
}

func TestUserService_DeleteUser_ReviewsNoLongerGateThePull(t *testing.T) {
	env := newRepoDirsEnv(t)
	ctx := context.Background()
	otherID, other := env.seedUser(t)
	repoID := env.createWithWiki(t, other, "theirs")
	pullID := env.seedPull(t, repoID, otherID)
	protections := store.NewBranchProtectionStore(env.db)
	if err := protections.Create(ctx, &model.BranchProtection{RepoID: repoID, Pattern: "main", RequireReviewCount: 1}); err != nil {
		t.Fatalf("protect main: %v", err)
	}
	for _, state := range []string{"approved", "changes_requested"} {
		reviewerID, reviewer := env.seedUser(t)
		testutil.Exec(t, env.db,
			`INSERT INTO pull_reviews (pull_id, repo_id, author_id, author_name, state) VALUES ($1, $2, $3, $4, $5)`,
			pullID, repoID, reviewerID, reviewer, state)
		if err := env.users().DeleteUser(ctx, reviewerID); err != nil {
			t.Fatalf("DeleteUser of the %s reviewer: %v", state, err)
		}
	}
	reviews := service.NewPullReviewService(store.NewPullReviewStore(env.db), store.NewPullStore(env.db), store.NewRepoStore(env.db), protections)

	if ok, reason, err := reviews.CanMerge(ctx, pullID); err != nil || !ok {
		t.Errorf("CanMerge = %v, %q, %v; nobody is left to withdraw the request for changes", ok, reason, err)
	}
	if required, approved, err := reviews.Counts(ctx, pullID); err != nil || required != 1 || approved != 0 {
		t.Errorf("Counts = %d required, %d approved, %v; want 1, 0 to match CheckMerge", required, approved, err)
	}
}

func TestUserService_DeleteUser_RepoCreatedMidDeleteAbortsIt(t *testing.T) {
	env := newRepoDirsEnv(t)
	ctx := context.Background()
	users := store.NewUserStore(env.db)

	for _, tc := range []struct {
		name   string
		create func(userID int64, user string) error
	}{
		{"personal repo", func(userID int64, user string) error {
			_, err := env.repos.Create(ctx, userID, user, "late", "", false, service.RepoInitOptions{})
			return err
		}},
	} {
		userID, user := env.seedUser(t)
		env.createWithWiki(t, user, "early")
		err := env.repos.DeleteWithOwner(ctx, userID, func(live []int64) error {
			if err := tc.create(userID, user); err != nil {
				t.Fatalf("%s: create mid-delete: %v", tc.name, err)
			}
			return users.DeleteWithOwnedRepos(ctx, userID, live)
		})
		if err == nil {
			t.Fatalf("%s: delete succeeded although a repo appeared after the move-aside", tc.name)
		}
		if !env.userExists(t, userID) {
			t.Fatalf("%s: user row deleted", tc.name)
		}
		if _, err := os.Stat(filepath.Join(env.root, user, "early.git")); err != nil {
			t.Fatalf("%s: early repo not restored: %v", tc.name, err)
		}
		var n int
		if err := env.db.QueryRow(`SELECT COUNT(*) FROM repositories WHERE owner_id = $1 AND name = 'late'`, userID).Scan(&n); err != nil || n != 1 {
			t.Fatalf("%s: repo created mid-delete: count = %d, %v; want it kept", tc.name, n, err)
		}
	}
}
