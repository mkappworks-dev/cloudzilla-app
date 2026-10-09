package store_test

import (
	"context"
	"testing"

	"github.com/mkappworks-dev/cloudzilla-app/internal/store"
	"github.com/mkappworks-dev/cloudzilla-app/internal/testutil"
)

func TestProjectStore_RefKinds(t *testing.T) {
	db := openStoreDB(t)
	suffix := testutil.UniqueSuffix(t)
	ownerID := testutil.SeedUser(t, db, suffix)
	repoID := testutil.SeedRepo(t, db, ownerID, "testuser_"+suffix, suffix)
	s := store.NewProjectStore(db)

	seedIssueRow(t, db, repoID, ownerID, 1, "open")
	seedPullRow(t, db, repoID, ownerID, 2, "open")
	seedIssueRow(t, db, repoID, ownerID, 3, "open")
	seedPullRow(t, db, repoID, ownerID, 3, "open")

	got, err := s.RefKinds(context.Background(), repoID, []int{1, 2, 3, 4})
	if err != nil {
		t.Fatalf("RefKinds: %v", err)
	}
	want := map[int]string{1: "issues", 2: "pulls", 3: "issues"}
	if len(got) != len(want) {
		t.Fatalf("RefKinds = %v, want %v", got, want)
	}
	for n, k := range want {
		if got[n] != k {
			t.Errorf("RefKinds[%d] = %q, want %q", n, got[n], k)
		}
	}

	if g, err := s.RefKinds(context.Background(), repoID, []int{3000000000, -1, 0, 1}); err != nil || len(g) != 1 || g[1] != "issues" {
		t.Fatalf("RefKinds with out-of-range = %v, %v", g, err)
	}

	empty, err := s.RefKinds(context.Background(), repoID, nil)
	if err != nil || len(empty) != 0 {
		t.Fatalf("RefKinds(nil) = %v, %v", empty, err)
	}
}
