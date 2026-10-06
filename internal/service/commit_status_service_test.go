package service_test

// Integration test for CommitStatusService.ListRecentCommits. Requires TEST_DATABASE_DSN and skips otherwise.

import (
	"context"
	"strings"
	"testing"

	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/service"
	"github.com/mkappworks-dev/cloudzilla-app/internal/store"
	"github.com/mkappworks-dev/cloudzilla-app/internal/testutil"
)

func TestCommitStatusService_ListRecentCommits(t *testing.T) {
	db := testutil.OpenTestDB(t)
	suffix := testutil.UniqueSuffix(t)
	ownerID := testutil.SeedUser(t, db, suffix)
	ownerName := "testuser_" + suffix
	repoName := "testrepo_" + suffix
	repoID := testutil.SeedRepo(t, db, ownerID, ownerName, suffix)
	codeSvc, headSHA := seedBareRepo(t, t.TempDir(), ownerName, repoName, "feature-"+suffix)

	csStore := store.NewCommitStatusStore(db)
	unknownSHA := strings.Repeat("ab", 20)
	ctx := context.Background()
	for i, s := range []struct {
		sha, context string
		state        model.CommitStatusState
	}{
		{headSHA, "ci/build", model.CommitStatusSuccess},
		{headSHA, "ci/test", model.CommitStatusFailure},
		{unknownSHA, "ci/build", model.CommitStatusSuccess},
		{"main", "ci/build", model.CommitStatusPending},
	} {
		cs := &model.CommitStatus{RepoID: repoID, SHA: s.sha, Context: s.context, State: s.state, CreatorID: ownerID}
		if err := csStore.Upsert(ctx, cs); err != nil {
			t.Fatalf("Upsert: %v", err)
		}
		testutil.Exec(t, db, `UPDATE commit_statuses SET updated_at = NOW() + make_interval(mins => $2) WHERE id = $1`, cs.ID, i)
	}

	svc := service.NewCommitStatusService(csStore, store.NewRepoStore(db), nil, nil, codeSvc)

	commits, hasMore, err := svc.ListRecentCommits(ctx, repoID, ownerName, repoName, 1, 2)
	if err != nil {
		t.Fatalf("ListRecentCommits: %v", err)
	}
	if !hasMore {
		t.Error("page 1 of 2: hasMore = false, want true")
	}
	if len(commits) != 2 || commits[0].SHA != "main" || commits[1].SHA != unknownSHA {
		t.Fatalf("page 1 = %+v, want main then %s", commits, unknownSHA)
	}
	if commits[0].Subject != "" {
		t.Errorf("a status on a branch name must not show the branch tip's subject, got %q", commits[0].Subject)
	}
	if commits[1].Subject != "" {
		t.Errorf("unknown SHA subject = %q, want empty", commits[1].Subject)
	}

	commits, hasMore, err = svc.ListRecentCommits(ctx, repoID, ownerName, repoName, 2, 2)
	if err != nil {
		t.Fatalf("ListRecentCommits page 2: %v", err)
	}
	if hasMore {
		t.Error("page 2 of 2: hasMore = true, want false")
	}
	if len(commits) != 1 {
		t.Fatalf("page 2 = %+v, want one commit", commits)
	}
	c := commits[0]
	if c.SHA != headSHA || c.Subject != "feat: add feature" {
		t.Errorf("commit = %s %q, want %s %q", c.SHA, c.Subject, headSHA, "feat: add feature")
	}
	if c.State != model.CommitStatusFailure || c.Passed != 1 || len(c.Statuses) != 2 {
		t.Errorf("commit state=%s passed=%d statuses=%d, want failure 1 2", c.State, c.Passed, len(c.Statuses))
	}
	if c.UpdatedAt.IsZero() {
		t.Error("UpdatedAt is zero, want the latest status update")
	}
}
