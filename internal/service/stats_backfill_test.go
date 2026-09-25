package service_test

import (
	"context"
	"testing"

	"github.com/mkappworks-dev/cloudzilla-app/internal/config"
	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/service"
	"github.com/mkappworks-dev/cloudzilla-app/internal/store"
	"github.com/mkappworks-dev/cloudzilla-app/internal/testutil"
)

func TestBackfillRepoStats_DryRunThenApply(t *testing.T) {
	db := testutil.OpenTestDB(t)
	ctx := context.Background()
	suffix := testutil.UniqueSuffix(t)

	// buildMultiBranchRepo authors every commit as t@test.invalid.
	var userID int64
	if err := db.QueryRowContext(ctx,
		`INSERT INTO users (username, email, password_hash, is_superadmin) VALUES ($1, 't@test.invalid', 'x', false) RETURNING id`,
		"u_"+suffix,
	).Scan(&userID); err != nil {
		t.Fatalf("insert user: %v", err)
	}
	t.Cleanup(func() { _, _ = db.ExecContext(context.Background(), `DELETE FROM users WHERE id = $1`, userID) })
	var repoID int64
	if err := db.QueryRowContext(ctx,
		`INSERT INTO repositories (owner_id, owner_name, name, description, private, default_branch) VALUES ($1, $2, $3, '', false, 'main') RETURNING id`,
		userID, "alice_"+suffix, "proj_"+suffix,
	).Scan(&repoID); err != nil {
		t.Fatalf("insert repo: %v", err)
	}

	reposRoot := buildMultiBranchRepo(t, "alice_"+suffix, "proj_"+suffix)
	code := service.NewCodeService(config.GitConfig{ReposRoot: reposRoot})
	statsStore := store.NewContributorStatsStore(db)
	userStore := store.NewUserStore(db)
	repo := model.Repository{ID: repoID, OwnerName: "alice_" + suffix, Name: "proj_" + suffix, DefaultBranch: "main"}

	dry, err := service.BackfillRepoStats(ctx, statsStore, code, userStore, repo, false)
	if err != nil {
		t.Fatalf("dry-run: %v", err)
	}
	if dry.CommitsWithKnownUser != 3 || dry.WeekCommitsBefore != 0 || dry.Applied {
		t.Fatalf("dry-run report wrong: %+v", dry)
	}
	weekRows, _ := statsStore.ListForRepo(ctx, repoID)
	if len(weekRows) != 0 {
		t.Fatalf("dry-run must not write: got %d week rows", len(weekRows))
	}

	applied, err := service.BackfillRepoStats(ctx, statsStore, code, userStore, repo, true)
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	if !applied.Applied || applied.WeekCommitsAfter != 3 {
		t.Fatalf("apply report wrong: %+v", applied)
	}
	weekRows, _ = statsStore.ListForRepo(ctx, repoID)
	total := 0
	for _, r := range weekRows {
		total += r.Commits
	}
	if total != 3 {
		t.Fatalf("after apply: want 3 week commits, got %d", total)
	}

	again, err := service.BackfillRepoStats(ctx, statsStore, code, userStore, repo, true)
	if err != nil {
		t.Fatalf("re-apply: %v", err)
	}
	if again.WeekCommitsBefore != 3 || again.WeekCommitsAfter != 3 {
		t.Fatalf("re-apply must be idempotent: %+v", again)
	}
}
