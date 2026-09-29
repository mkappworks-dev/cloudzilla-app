package service

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	gogit "github.com/go-git/go-git/v5"
	"github.com/mkappworks-dev/cloudzilla-app/internal/testutil"
)

func TestFork_DiskFailureLeavesNoForkRow(t *testing.T) {
	cases := map[string]func(root, forker string) string{
		"owner dir is a file": func(root, forker string) string { return filepath.Join(root, forker) },
		"repo dir is a file":  func(root, forker string) string { return filepath.Join(root, forker, "x.git") },
	}
	for name, blockedPath := range cases {
		t.Run(name, func(t *testing.T) {
			db := testutil.OpenTestDB(t)
			root := t.TempDir()
			suffix := testutil.UniqueSuffix(t)
			owner, forker := "forked_"+suffix, "forker_"+suffix
			ownerID := seedOwner(t, db, owner)
			forkerID := seedOwner(t, db, forker)
			origID := seedRepoRow(t, db, ownerID, owner, "x", "NULL")
			testutil.Exec(t, db, `UPDATE repositories SET fork_count = 2 WHERE id = $1`, origID)
			if _, err := gogit.PlainInit(filepath.Join(root, owner, "x.git"), true); err != nil {
				t.Fatal(err)
			}
			blocker := blockedPath(root, forker)
			mkdirs(t, filepath.Dir(blocker))
			if err := os.WriteFile(blocker, nil, 0o644); err != nil {
				t.Fatal(err)
			}

			_, err := newDiskRepoService(db, root).Fork(context.Background(), owner, "x", forkerID, forker)

			if err == nil {
				t.Fatal("want an error when the repository can't be copied")
			}
			var forks, forkCount int
			if err := db.QueryRowContext(context.Background(),
				`SELECT COUNT(*) FROM repositories WHERE fork_of_id = $1`, origID,
			).Scan(&forks); err != nil {
				t.Fatal(err)
			}
			if forks != 0 {
				t.Errorf("no fork row may remain, found %d", forks)
			}
			if err := db.QueryRowContext(context.Background(),
				`SELECT fork_count FROM repositories WHERE id = $1`, origID,
			).Scan(&forkCount); err != nil {
				t.Fatal(err)
			}
			if forkCount != 2 {
				t.Errorf("fork count must stay 2, got %d", forkCount)
			}
		})
	}
}
