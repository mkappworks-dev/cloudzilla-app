package store_test

// Integration tests for RepoStore. All tests require TEST_DATABASE_DSN and skip otherwise.

import (
	"context"
	"testing"

	"github.com/mkappworks/cloudzilla/internal/model"
	"github.com/mkappworks/cloudzilla/internal/store"
	"github.com/mkappworks/cloudzilla/internal/testutil"
)

// TestRepoStore_GetByOwnerName_ReturnsRepo verifies that GetByOwnerName finds a repository
// by its owner username and repository name.
func TestRepoStore_GetByOwnerName_ReturnsRepo(t *testing.T) {
	db := openStoreDB(t)
	suffix := testutil.UniqueSuffix(t)
	ownerID := testutil.SeedUser(t, db, suffix)
	ownerName := "testuser_" + suffix
	repoID := testutil.SeedRepo(t, db, ownerID, ownerName, suffix)

	s := store.NewRepoStore(db)
	repo, err := s.GetByOwnerName(context.Background(), ownerName, "testrepo_"+suffix)
	if err != nil {
		t.Fatalf("GetByOwnerName: %v", err)
	}
	if repo.ID != repoID {
		t.Errorf("want repo ID %d, got %d", repoID, repo.ID)
	}
	if repo.OwnerID != ownerID {
		t.Errorf("want OwnerID %d, got %d", ownerID, repo.OwnerID)
	}
}

// TestRepoStore_GetByOwnerName_UnknownRepo_Error verifies that GetByOwnerName returns
// an error when no repository with that owner/name pair exists.
func TestRepoStore_GetByOwnerName_UnknownRepo_Error(t *testing.T) {
	db := openStoreDB(t)
	s := store.NewRepoStore(db)
	_, err := s.GetByOwnerName(context.Background(), "nobody", "nonexistent")
	if err == nil {
		t.Error("GetByOwnerName for unknown repo must return an error")
	}
}

// TestRepoStore_GetPermission_OwnerRole verifies that GetPermission returns the role
// stored in the permissions table for a user/repo pair.
func TestRepoStore_GetPermission_OwnerRole(t *testing.T) {
	// SeedRepo inserts an 'owner' permission row for ownerID automatically.
	db := openStoreDB(t)
	suffix := testutil.UniqueSuffix(t)
	ownerID := testutil.SeedUser(t, db, suffix)
	ownerName := "testuser_" + suffix
	repoID := testutil.SeedRepo(t, db, ownerID, ownerName, suffix)

	s := store.NewRepoStore(db)
	role, err := s.GetPermission(context.Background(), repoID, ownerID)
	if err != nil {
		t.Fatalf("GetPermission: %v", err)
	}
	if role != "owner" {
		t.Errorf("want role %q, got %q", "owner", role)
	}
}

// TestRepoStore_GetPermission_NoRow_Error verifies that GetPermission returns an error
// when no permission row exists for the given user/repo pair.
func TestRepoStore_GetPermission_NoRow_Error(t *testing.T) {
	db := openStoreDB(t)
	suffix := testutil.UniqueSuffix(t)
	ownerID := testutil.SeedUser(t, db, suffix)
	ownerName := "testuser_" + suffix
	repoID := testutil.SeedRepo(t, db, ownerID, ownerName, suffix)
	strangerID := testutil.SeedUser(t, db, "str_"+suffix)

	s := store.NewRepoStore(db)
	_, err := s.GetPermission(context.Background(), repoID, strangerID)
	if err == nil {
		t.Error("GetPermission must return an error when no permission row exists")
	}
}

// TestRepoStore_AddPermission_ThenGet verifies that AddPermission upserts the role and
// GetPermission reads it back correctly.
func TestRepoStore_AddPermission_ThenGet(t *testing.T) {
	db := openStoreDB(t)
	suffix := testutil.UniqueSuffix(t)
	ownerID := testutil.SeedUser(t, db, suffix)
	ownerName := "testuser_" + suffix
	repoID := testutil.SeedRepo(t, db, ownerID, ownerName, suffix)
	collaboratorID := testutil.SeedUser(t, db, "collab_"+suffix)
	t.Cleanup(func() {
		db.ExecContext(context.Background(),
			`DELETE FROM permissions WHERE user_id = $1 AND repo_id = $2`, collaboratorID, repoID)
	})

	s := store.NewRepoStore(db)
	if err := s.AddPermission(context.Background(), repoID, collaboratorID, "writer"); err != nil {
		t.Fatalf("AddPermission: %v", err)
	}

	role, err := s.GetPermission(context.Background(), repoID, collaboratorID)
	if err != nil {
		t.Fatalf("GetPermission after add: %v", err)
	}
	if role != "writer" {
		t.Errorf("want role %q, got %q", "writer", role)
	}
}

// TestRepoStore_AddPermission_Upsert verifies that AddPermission updates the role
// when a permission row already exists (ON CONFLICT DO UPDATE).
func TestRepoStore_AddPermission_Upsert(t *testing.T) {
	db := openStoreDB(t)
	suffix := testutil.UniqueSuffix(t)
	ownerID := testutil.SeedUser(t, db, suffix)
	ownerName := "testuser_" + suffix
	repoID := testutil.SeedRepo(t, db, ownerID, ownerName, suffix)
	collaboratorID := testutil.SeedUser(t, db, "upsert_"+suffix)
	t.Cleanup(func() {
		db.ExecContext(context.Background(),
			`DELETE FROM permissions WHERE user_id = $1 AND repo_id = $2`, collaboratorID, repoID)
	})

	s := store.NewRepoStore(db)
	// Insert "reader" first, then upsert to "admin".
	_ = s.AddPermission(context.Background(), repoID, collaboratorID, "reader")
	if err := s.AddPermission(context.Background(), repoID, collaboratorID, "admin"); err != nil {
		t.Fatalf("AddPermission upsert: %v", err)
	}

	role, err := s.GetPermission(context.Background(), repoID, collaboratorID)
	if err != nil {
		t.Fatalf("GetPermission after upsert: %v", err)
	}
	if role != "admin" {
		t.Errorf("want role %q after upsert, got %q", "admin", role)
	}
}

// TestRepoStore_CreateWithOwnerName_AssignsID verifies that CreateWithOwnerName inserts
// a repository and populates its ID from the RETURNING clause.
func TestRepoStore_CreateWithOwnerName_AssignsID(t *testing.T) {
	db := openStoreDB(t)
	suffix := testutil.UniqueSuffix(t)
	ownerID := testutil.SeedUser(t, db, suffix)
	ownerName := "testuser_" + suffix

	s := store.NewRepoStore(db)
	r := &model.Repository{
		OwnerID:       ownerID,
		OwnerName:     ownerName,
		Name:          "newrepo_" + suffix,
		DefaultBranch: "main",
	}
	if err := s.CreateWithOwnerName(context.Background(), r); err != nil {
		t.Fatalf("CreateWithOwnerName: %v", err)
	}
	t.Cleanup(func() {
		db.ExecContext(context.Background(), `DELETE FROM repositories WHERE id = $1`, r.ID)
	})

	if r.ID == 0 {
		t.Error("CreateWithOwnerName must populate a non-zero ID")
	}
	if r.CreatedAt.IsZero() {
		t.Error("CreateWithOwnerName must populate CreatedAt")
	}
}
