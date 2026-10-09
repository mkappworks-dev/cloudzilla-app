package main

// Integration test for the size refresh after gc. It requires TEST_DATABASE_DSN and skips otherwise.

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/mkappworks-dev/cloudzilla-app/internal/store"
	"github.com/mkappworks-dev/cloudzilla-app/internal/testutil"
)

func TestRefreshRepoSize_StoresTheGitAndWikiDirSizes(t *testing.T) {
	db := testutil.OpenTestDB(t)
	suffix := testutil.UniqueSuffix(t)
	ownerID := testutil.SeedUser(t, db, suffix)
	owner := "testuser_" + suffix
	repoID := testutil.SeedRepo(t, db, ownerID, owner, suffix)
	name := "testrepo_" + suffix
	root := t.TempDir()
	gitDir := filepath.Join(root, owner, name+".git")
	wikiDir := filepath.Join(root, owner, name+".wiki.git")
	for path, n := range map[string]int{filepath.Join(gitDir, "objects", "a"): 700, filepath.Join(wikiDir, "b"): 50} {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, make([]byte, n), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	if err := refreshRepoSize(context.Background(), store.NewRepoStore(db), gitDir); err != nil {
		t.Fatalf("refreshRepoSize: %v", err)
	}

	var size int64
	if err := db.QueryRow(`SELECT size_bytes FROM repositories WHERE id = $1`, repoID).Scan(&size); err != nil {
		t.Fatalf("read size: %v", err)
	}
	if size != 750 {
		t.Errorf("size_bytes = %d, want 750 (the git dir's 700 plus the wiki's 50)", size)
	}
}
