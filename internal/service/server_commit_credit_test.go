package service

import (
	"context"
	"path/filepath"
	"testing"

	gogit "github.com/go-git/go-git/v5"
	"github.com/mkappworks-dev/cloudzilla-app/internal/config"
	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/store"
	"github.com/mkappworks-dev/cloudzilla-app/internal/testutil"
)

// A server-made commit names the instance as committer; every credit reader must still follow the author.
func TestServerCommits_CreditTheAuthorNotTheInstance(t *testing.T) {
	db := testutil.OpenTestDB(t)
	ctx := context.Background()
	suffix := testutil.UniqueSuffix(t)
	owner, repoName := "alice_"+suffix, "proj_"+suffix

	var userID int64
	if err := db.QueryRowContext(ctx,
		`INSERT INTO users (username, email, password_hash, is_superadmin) VALUES ($1, $2, 'x', false) RETURNING id`,
		owner, owner+"@test.invalid",
	).Scan(&userID); err != nil {
		t.Fatalf("insert user: %v", err)
	}
	t.Cleanup(func() { testutil.DeleteUsers(t, db, userID) })
	var repoID int64
	if err := db.QueryRowContext(ctx,
		`INSERT INTO repositories (owner_id, owner_name, name, description, private, default_branch) VALUES ($1, $2, $3, '', false, 'main') RETURNING id`,
		userID, owner, repoName,
	).Scan(&repoID); err != nil {
		t.Fatalf("insert repo: %v", err)
	}

	root := t.TempDir()
	if _, err := gogit.PlainInit(filepath.Join(root, owner, repoName+".git"), true); err != nil {
		t.Fatal(err)
	}
	code := NewCodeService(config.GitConfig{ReposRoot: root}).WithServerCommits(newServerCommitter(NewInstanceSigner(nil, nil), serverCommitBaseURL))
	author := GitAuthor{Name: owner, Email: owner + "@test.invalid"}
	if _, err := code.CommitFile(owner, repoName, "main", "a.txt", []byte("a\n"), author, "Add a.txt"); err != nil {
		t.Fatal(err)
	}

	users := store.NewUserStore(db)
	stats := store.NewContributorStatsStore(db)
	repo := model.Repository{ID: repoID, OwnerName: owner, Name: repoName, DefaultBranch: "main"}

	report, err := BackfillRepoStats(ctx, stats, code, users, repo, true)
	if err != nil {
		t.Fatalf("BackfillRepoStats: %v", err)
	}
	if report.CommitsWalked != 1 || report.CommitsWithKnownUser != 1 {
		t.Errorf("backfill walked %d, credited %d commits; want 1 and 1", report.CommitsWalked, report.CommitsWithKnownUser)
	}
	rows, err := stats.ListForRepo(ctx, repoID)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].UserID != userID {
		t.Errorf("contributor rows = %+v, want one credited to user %d", rows, userID)
	}

	walked, err := code.WalkAllRefCommits(owner, repoName)
	if err != nil || len(walked) != 1 {
		t.Fatalf("WalkAllRefCommits: %v, %d commits", err, len(walked))
	}
	if walked[0].AuthorEmail != author.Email {
		t.Errorf("walked author email = %q, want %q", walked[0].AuthorEmail, author.Email)
	}

	resolved, err := NewUserService(users, config.AuthConfig{}).ResolveCommitAuthors(ctx, []string{walked[0].AuthorEmail, "noreply@git.example.test"})
	if err != nil {
		t.Fatal(err)
	}
	if resolved[author.Email] != owner {
		t.Errorf("ResolveCommitAuthors(author) = %q, want %q", resolved[author.Email], owner)
	}
	if name, ok := resolved["noreply@git.example.test"]; ok {
		t.Errorf("the instance committer address resolves to %q; it must resolve to nobody", name)
	}
}
