package service_test

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	gogit "github.com/go-git/go-git/v5"
	"github.com/mkappworks-dev/cloudzilla-app/internal/config"
	"github.com/mkappworks-dev/cloudzilla-app/internal/service"
	"github.com/mkappworks-dev/cloudzilla-app/internal/store"
	"github.com/mkappworks-dev/cloudzilla-app/internal/testutil"
)

func TestUserService_Create_KeepsEmailPrivateByDefault(t *testing.T) {
	db := testutil.OpenTestDB(t)
	suffix := testutil.UniqueSuffix(t)
	svc := service.NewUserService(store.NewUserStore(db), config.AuthConfig{})

	u, err := svc.Create(context.Background(), "kepcreate_"+suffix, "kepcreate_"+suffix+"@test.invalid", "pass")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	t.Cleanup(func() { _, _ = db.ExecContext(context.Background(), `DELETE FROM users WHERE id = $1`, u.ID) })
	if !u.KeepEmailPrivate {
		t.Error("Create must return keep_email_private = true for a new user")
	}
}

func TestRepoService_TopContributors_MergesAUsersAuthorEmails(t *testing.T) {
	db := testutil.OpenTestDB(t)
	ctx := context.Background()
	suffix := testutil.UniqueSuffix(t)
	userID := testutil.SeedUser(t, db, suffix)
	username := "testuser_" + suffix
	repoName := "testrepo_" + suffix

	gitCfg := config.GitConfig{ReposRoot: t.TempDir()}
	if _, err := gogit.PlainInit(filepath.Join(gitCfg.ReposRoot, username, repoName+".git"), true); err != nil {
		t.Fatalf("init bare repo: %v", err)
	}
	code := service.NewCodeService(gitCfg)
	commits := []struct{ name, email, path string }{
		{username, fmt.Sprintf("%d+%s@users.noreply.localhost", userID, username), "a.txt"},
		{"Test User", username + "@test.invalid", "b.txt"},
		{"Test User", username + "@test.invalid", "c.txt"},
		{"Stranger", "stranger_" + suffix + "@test.invalid", "d.txt"},
	}
	for _, c := range commits {
		if err := code.CommitFile(username, repoName, "main", c.path, []byte(c.path), c.name, c.email, "Add "+c.path); err != nil {
			t.Fatalf("commit %s: %v", c.path, err)
		}
	}

	repoSvc := service.NewRepoService(store.NewRepoStore(db), store.NewUserStore(db), store.NewOrgStore(db), nil, code, gitCfg)
	got, err := repoSvc.TopContributors(ctx, username, repoName, "main", 10)
	if err != nil {
		t.Fatalf("TopContributors: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("want 2 contributors, got %d: %+v", len(got), got)
	}
	if got[0].Name != username || got[0].Commits != 3 {
		t.Errorf("first = %q with %d commits, want %q with 3", got[0].Name, got[0].Commits, username)
	}
	if got[1].Name != "Stranger" || got[1].Commits != 1 {
		t.Errorf("second = %q with %d commits, want \"Stranger\" with 1", got[1].Name, got[1].Commits)
	}
}

func TestUserService_CommitAuthor_FollowsKeepEmailPrivate(t *testing.T) {
	db := testutil.OpenTestDB(t)
	ctx := context.Background()
	suffix := testutil.UniqueSuffix(t)
	userID := testutil.SeedUser(t, db, suffix)
	username := "testuser_" + suffix

	svc := service.NewUserService(store.NewUserStore(db), config.AuthConfig{}).
		WithNoreplyHostFrom("https://git.example.com:8443")

	u, err := svc.GetByID(ctx, userID)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if !u.KeepEmailPrivate {
		t.Fatal("keep_email_private must default to true")
	}

	name, email, err := svc.CommitAuthor(ctx, userID)
	if err != nil {
		t.Fatalf("CommitAuthor: %v", err)
	}
	wantNoreply := fmt.Sprintf("%d+%s@users.noreply.git.example.com", userID, username)
	if name != username || email != wantNoreply {
		t.Errorf("private: got %q <%s>, want %q <%s>", name, email, username, wantNoreply)
	}

	if err := svc.UpdateKeepEmailPrivate(ctx, userID, false); err != nil {
		t.Fatalf("UpdateKeepEmailPrivate: %v", err)
	}
	_, email, err = svc.CommitAuthor(ctx, userID)
	if err != nil {
		t.Fatalf("CommitAuthor: %v", err)
	}
	if want := username + "@test.invalid"; email != want {
		t.Errorf("public: got <%s>, want <%s>", email, want)
	}
}

func TestCommitStatsService_Ingest_AttributesNoreplyEmail(t *testing.T) {
	db := testutil.OpenTestDB(t)
	ctx := context.Background()
	suffix := testutil.UniqueSuffix(t)
	userID := testutil.SeedUser(t, db, suffix)
	username := "testuser_" + suffix
	repoID := testutil.SeedRepo(t, db, userID, username, suffix)

	statsStore := store.NewCommitStatsStore(db)
	svc := service.NewCommitStatsService(statsStore, store.NewUserStore(db))

	when := time.Now().UTC()
	samples := []service.CommitSample{
		{AuthorEmail: fmt.Sprintf("%d+%s@users.noreply.localhost", userID, username), Time: when},
		// Base URL changed since this commit was made.
		{AuthorEmail: fmt.Sprintf("%d+%s@users.noreply.git.example.com", userID, username), Time: when},
		// The id belongs to this user but the username does not.
		{AuthorEmail: fmt.Sprintf("%d+someone_else@users.noreply.localhost", userID), Time: when},
		{AuthorEmail: username + "@localhost", Time: when},
	}
	if err := svc.Ingest(ctx, repoID, samples); err != nil {
		t.Fatalf("Ingest: %v", err)
	}

	got, err := svc.CommitsForUserSince(ctx, userID, 2)
	if err != nil {
		t.Fatalf("CommitsForUserSince: %v", err)
	}
	if got != 2 {
		t.Errorf("attributed commits = %d, want 2", got)
	}
}
