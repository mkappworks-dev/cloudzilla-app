package service_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mkappworks-dev/cloudzilla-app/internal/config"
	"github.com/mkappworks-dev/cloudzilla-app/internal/service"
	"github.com/mkappworks-dev/cloudzilla-app/internal/store"
	"github.com/mkappworks-dev/cloudzilla-app/internal/testutil"
)

// The owner is seeded directly: a name like "../x" predates the owner-name rule
// or bypasses it, and must still never reach outside the repos root.
func TestRepoService_Create_OwnerOutsideRoot_CreatesNothing(t *testing.T) {
	db := testutil.OpenTestDB(t)
	root := filepath.Join(t.TempDir(), "repos")
	owner := "../dd_" + testutil.UniqueSuffix(t)
	escaped := filepath.Join(root, owner, "escape.git")
	if rel, err := filepath.Rel(root, escaped); err != nil || !strings.HasPrefix(rel, "..") {
		t.Fatalf("owner %q must point outside the root for this test; rel = %q, %v", owner, rel, err)
	}
	var ownerID int64
	if err := db.QueryRowContext(context.Background(),
		`INSERT INTO users (username, email, password_hash) VALUES ($1, $2, 'x') RETURNING id`,
		owner, "dotdot_"+testutil.UniqueSuffix(t)+"@test.invalid",
	).Scan(&ownerID); err != nil {
		t.Fatalf("seed user: %v", err)
	}
	t.Cleanup(func() { testutil.DeleteUsers(t, db, ownerID) })
	svc := service.NewRepoService(store.NewRepoStore(db), store.NewUserStore(db), store.NewOrgStore(db), nil, nil, config.GitConfig{ReposRoot: root})

	_, err := svc.Create(context.Background(), ownerID, owner, "escape", "", false, service.RepoInitOptions{})

	if !errors.Is(err, service.ErrInvalidRepoPath) {
		t.Errorf("want ErrInvalidRepoPath, got %v", err)
	}
	if _, statErr := os.Stat(escaped); !os.IsNotExist(statErr) {
		t.Errorf("nothing may be created outside the repos root; stat: %v", statErr)
	}
	var rows int
	if err := db.QueryRowContext(context.Background(), `SELECT COUNT(*) FROM repositories WHERE owner_id = $1`, ownerID).Scan(&rows); err != nil {
		t.Fatal(err)
	}
	if rows != 0 {
		t.Errorf("no repository row may be created, found %d", rows)
	}
}

func TestRepoDir_RejectsAnythingButOnePathElement(t *testing.T) {
	root := t.TempDir()
	for _, seg := range []string{"..", "a/b", `a\b`, "a\x00", ".", "", "*", "?", "[a]"} {
		if _, err := service.RepoDir(root, seg, "x.git"); !errors.Is(err, service.ErrInvalidRepoPath) {
			t.Errorf("owner %q: want ErrInvalidRepoPath, got %v", seg, err)
		}
		if _, err := service.RepoDir(root, "alice", seg); !errors.Is(err, service.ErrInvalidRepoPath) {
			t.Errorf("name %q: want ErrInvalidRepoPath, got %v", seg, err)
		}
	}

	got, err := service.RepoDir(root, "alice", "x.wiki.git")
	if want := filepath.Join(root, "alice", "x.wiki.git"); err != nil || got != want {
		t.Errorf("RepoDir = %q, %v; want %q", got, err, want)
	}
}

// A file where the repo directory belongs makes git init fail after the row is
// inserted; the row must not outlive the failure.
func TestCreate_InitFails_LeavesNoRow(t *testing.T) {
	db := testutil.OpenTestDB(t)
	root := t.TempDir()
	suffix := testutil.UniqueSuffix(t)
	userID := testutil.SeedUser(t, db, suffix)
	user := "testuser_" + suffix
	orgs := service.NewOrgService(store.NewOrgStore(db), store.NewRepoStore(db), store.NewUserStore(db), config.GitConfig{ReposRoot: root})
	org, err := orgs.Create(context.Background(), userID, "initorg_"+suffix, "", "")
	if err != nil {
		t.Fatalf("create org: %v", err)
	}
	t.Cleanup(func() { testutil.Exec(t, db, `DELETE FROM organizations WHERE id = $1`, org.ID) })
	repos := service.NewRepoService(store.NewRepoStore(db), store.NewUserStore(db), store.NewOrgStore(db), nil, nil, config.GitConfig{ReposRoot: root})

	creates := []struct {
		owner, name string
		create      func(name string) error
	}{
		{user, "blocked_user", func(name string) error {
			_, err := repos.Create(context.Background(), userID, user, name, "", false, service.RepoInitOptions{})
			return err
		}},
		{org.Name, "blocked_org", func(name string) error {
			_, err := orgs.CreateRepo(context.Background(), org.ID, userID, name, "", false, service.RepoInitOptions{})
			return err
		}},
	}
	for _, c := range creates {
		if err := os.MkdirAll(filepath.Join(root, c.owner), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, c.owner, c.name+".git"), nil, 0o644); err != nil {
			t.Fatal(err)
		}

		if err := c.create(c.name); err == nil {
			t.Errorf("%s/%s: want the git init error, got nil", c.owner, c.name)
		}
		var rows int
		if err := db.QueryRowContext(context.Background(),
			`SELECT COUNT(*) FROM repositories WHERE owner_name = $1 AND name = $2`, c.owner, c.name,
		).Scan(&rows); err != nil {
			t.Fatal(err)
		}
		if rows != 0 {
			t.Errorf("%s/%s: a failed git init must leave no repository row, found %d", c.owner, c.name, rows)
		}
	}
}
