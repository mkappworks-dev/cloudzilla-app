package service_test

import (
	"context"
	"errors"
	"testing"

	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/service"
	"github.com/mkappworks-dev/cloudzilla-app/internal/testutil"
)

// A card shows its issue's title on a board anyone may read, so it must not be
// able to point at an issue or pull request in a repository the writer can't see.
func TestProjectService_CreateCard_RefusesAnotherReposIssueOrPull(t *testing.T) {
	e := newProjBoardEnv(t)
	ctx := context.Background()
	p := e.project(t, "Board")
	col := e.column(t, p.ID, "Todo")

	db := testutil.OpenTestDB(t)
	suffix := testutil.UniqueSuffix(t) + "f"
	foreignOwner := testutil.SeedUser(t, db, suffix)
	foreignRepo := testutil.SeedRepo(t, db, foreignOwner, "testuser_"+suffix, suffix)
	secret := &model.Issue{RepoID: foreignRepo, AuthorID: foreignOwner, Title: "private roadmap", State: model.IssueStateOpen, Visibility: "public"}
	if err := e.issues.Create(ctx, secret); err != nil {
		t.Fatal(err)
	}
	foreignPull := &model.PullRequest{RepoID: foreignRepo, AuthorID: foreignOwner, Title: "secret fix", State: model.PRStateOpen, HeadBranch: "x", BaseBranch: "main"}
	if err := e.pulls.Create(ctx, foreignPull); err != nil {
		t.Fatal(err)
	}
	own := &model.Issue{RepoID: e.repoID, AuthorID: e.ownerID, Title: "ours", State: model.IssueStateOpen, Visibility: "public"}
	if err := e.issues.Create(ctx, own); err != nil {
		t.Fatal(err)
	}
	missing := int64(1 << 40)

	for name, tc := range map[string]struct {
		issue, pull *int64
	}{
		"issue in another repo": {issue: &secret.ID},
		"pull in another repo":  {pull: &foreignPull.ID},
		"nonexistent issue":     {issue: &missing},
		"nonexistent pull":      {pull: &missing},
	} {
		if _, err := e.svc.CreateCard(ctx, p.ID, col.ID, e.writerID, model.CardDetails{IssueID: tc.issue, PullID: tc.pull}); !errors.Is(err, service.ErrCardTargetNotFound) {
			t.Errorf("%s: got %v, want ErrCardTargetNotFound", name, err)
		}
	}

	if _, err := e.svc.CreateCard(ctx, p.ID, col.ID, e.writerID, model.CardDetails{IssueID: &own.ID, PullID: &foreignPull.ID}); !errors.Is(err, service.ErrInvalidCard) {
		t.Errorf("issue and pull together: got %v, want ErrInvalidCard", err)
	}

	cols, err := e.svc.ListColumnsWithCards(ctx, p.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range cols {
		if len(c.Cards) != 0 {
			t.Errorf("refused requests still created %d card(s) in %q", len(c.Cards), c.Column.Name)
		}
	}

	if _, err := e.svc.CreateCard(ctx, p.ID, col.ID, e.writerID, model.CardDetails{IssueID: &own.ID}); err != nil {
		t.Errorf("an issue from the project's own repo: %v", err)
	}
}
