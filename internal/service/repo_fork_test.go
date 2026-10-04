package service

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"testing"

	gogit "github.com/go-git/go-git/v5"
	"github.com/mkappworks-dev/cloudzilla-app/internal/config"
	"github.com/mkappworks-dev/cloudzilla-app/internal/store"
	"github.com/mkappworks-dev/cloudzilla-app/internal/testutil"
)

func seedOwner(t *testing.T, db *sql.DB, username string) int64 {
	t.Helper()
	var id int64
	if err := db.QueryRowContext(context.Background(),
		`INSERT INTO users (username, email, password_hash) VALUES ($1, $2, 'x') RETURNING id`,
		username, "owner_"+testutil.UniqueSuffix(t)+"@test.invalid",
	).Scan(&id); err != nil {
		t.Fatalf("seed owner %q: %v", username, err)
	}
	t.Cleanup(func() { testutil.DeleteUsers(t, db, id) })
	return id
}

func seedRepoRow(t *testing.T, db *sql.DB, ownerID int64, ownerName, name string) int64 {
	t.Helper()
	var id int64
	if err := db.QueryRowContext(context.Background(),
		`INSERT INTO repositories (owner_id, owner_name, name, description, private, default_branch)
		 VALUES ($1, $2, $3, '', false, 'main') RETURNING id`,
		ownerID, ownerName, name,
	).Scan(&id); err != nil {
		t.Fatalf("seed repo %s/%s: %v", ownerName, name, err)
	}
	return id
}

func newDiskRepoService(db *sql.DB, root string) *RepoService {
	return NewRepoService(store.NewRepoStore(db), store.NewUserStore(db), store.NewOrgStore(db), nil, nil, config.GitConfig{ReposRoot: root})
}

func TestFork_DiskFailureLeavesNoForkRow(t *testing.T) {
	cases := map[string]func(root, forker string) string{
		"owner dir is a file": func(root, forker string) string { return filepath.Join(root, forker) },
	}
	for name, blockedPath := range cases {
		t.Run(name, func(t *testing.T) {
			db := testutil.OpenTestDB(t)
			root := t.TempDir()
			suffix := testutil.UniqueSuffix(t)
			owner, forker := "forked_"+suffix, "forker_"+suffix
			ownerID := seedOwner(t, db, owner)
			forkerID := seedOwner(t, db, forker)
			origID := seedRepoRow(t, db, ownerID, owner, "x")
			testutil.Exec(t, db, `UPDATE repositories SET fork_count = 2 WHERE id = $1`, origID)
			if _, err := gogit.PlainInit(filepath.Join(root, owner, "x.git"), true); err != nil {
				t.Fatal(err)
			}
			blocker := blockedPath(root, forker)
			if err := os.MkdirAll(filepath.Dir(blocker), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(blocker, nil, 0o644); err != nil {
				t.Fatal(err)
			}

			_, err := newDiskRepoService(db, root).Fork(context.Background(), owner, "x", forkerID, forker, ForkOptions{})

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
