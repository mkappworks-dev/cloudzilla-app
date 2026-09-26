package service_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/mkappworks-dev/cloudzilla-app/internal/config"
	"github.com/mkappworks-dev/cloudzilla-app/internal/service"
	"github.com/mkappworks-dev/cloudzilla-app/internal/store"
	"github.com/mkappworks-dev/cloudzilla-app/internal/testutil"
)

func TestUserService_CommitAuthor_FollowsKeepEmailPrivate(t *testing.T) {
	db := testutil.OpenTestDB(t)
	ctx := context.Background()
	suffix := testutil.UniqueSuffix(t)
	userID := testutil.SeedUser(t, db, suffix)
	username := "testuser_" + suffix

	svc := service.NewUserService(store.NewUserStore(db), config.AuthConfig{}).
		WithBaseURL("https://git.example.com:8443")

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

	if err := svc.SetKeepEmailPrivate(ctx, userID, false); err != nil {
		t.Fatalf("SetKeepEmailPrivate: %v", err)
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
