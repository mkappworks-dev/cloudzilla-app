package store_test

import (
	"context"
	"testing"

	"github.com/mkappworks-dev/cloudzilla-app/internal/store"
	"github.com/mkappworks-dev/cloudzilla-app/internal/testutil"
)

func TestRepoStore_OwnerUsage_CountsLiveReposAndTheirSizes(t *testing.T) {
	db := openStoreDB(t)
	ctx := context.Background()
	suffix := testutil.UniqueSuffix(t)
	ownerID := testutil.SeedUser(t, db, suffix)
	owner := "testuser_" + suffix
	var orgID int64
	if err := db.QueryRow(`INSERT INTO organizations (name) VALUES ($1) RETURNING id`, "org_"+suffix).Scan(&orgID); err != nil {
		t.Fatal(err)
	}
	testutil.DeleteOrgOnCleanup(t, db, orgID)
	s := store.NewRepoStore(db)

	sized := testutil.SeedRepo(t, db, ownerID, owner, suffix)
	testutil.Exec(t, db, `UPDATE repositories SET size_bytes = 700 WHERE id = $1`, sized)
	unmeasured := testutil.SeedRepo(t, db, ownerID, owner, suffix+"b")
	deleted := testutil.SeedRepo(t, db, ownerID, owner, suffix+"c")
	testutil.Exec(t, db, `UPDATE repositories SET size_bytes = 5000, deleted_at = NOW() WHERE id = $1`, deleted)
	testutil.Exec(t, db, `INSERT INTO repositories (org_id, owner_name, name, size_bytes) VALUES ($1, $2, 'one', 40), ($1, $2, 'two', 2)`, orgID, "org_"+suffix)

	repos, bytes, err := s.OwnerUsage(ctx, ownerID, 0)
	if err != nil {
		t.Fatalf("OwnerUsage(user): %v", err)
	}
	if repos != 2 || bytes != 700 {
		t.Errorf("user: want 2 repos and 700 bytes (an unmeasured repo counts 0, a deleted one not at all), got %d and %d", repos, bytes)
	}
	repos, bytes, err = s.OwnerUsage(ctx, 0, orgID)
	if err != nil {
		t.Fatalf("OwnerUsage(org): %v", err)
	}
	if repos != 2 || bytes != 42 {
		t.Errorf("org: want 2 repos and 42 bytes, got %d and %d", repos, bytes)
	}

	if err := s.SetSize(ctx, unmeasured, 300); err != nil {
		t.Fatalf("SetSize: %v", err)
	}
	if _, bytes, _ = s.OwnerUsage(ctx, ownerID, 0); bytes != 1000 {
		t.Errorf("after SetSize: want 1000 bytes, got %d", bytes)
	}
}

func TestRepoStore_ListUnmeasured_SkipsMeasuredAndDeletedRepos(t *testing.T) {
	db := openStoreDB(t)
	ctx := context.Background()
	suffix := testutil.UniqueSuffix(t)
	ownerID := testutil.SeedUser(t, db, suffix)
	owner := "testuser_" + suffix
	s := store.NewRepoStore(db)

	pending := testutil.SeedRepo(t, db, ownerID, owner, suffix)
	measured := testutil.SeedRepo(t, db, ownerID, owner, suffix+"b")
	testutil.Exec(t, db, `UPDATE repositories SET size_bytes = 0 WHERE id = $1`, measured)
	deleted := testutil.SeedRepo(t, db, ownerID, owner, suffix+"c")
	testutil.Exec(t, db, `UPDATE repositories SET deleted_at = NOW() WHERE id = $1`, deleted)

	repos, err := s.ListUnmeasured(ctx)
	if err != nil {
		t.Fatalf("ListUnmeasured: %v", err)
	}
	var got []int64
	for _, r := range repos {
		if r.OwnerName == owner {
			got = append(got, r.ID)
		}
	}
	if len(got) != 1 || got[0] != pending {
		t.Errorf("want only repo %d, got %v", pending, got)
	}
}
