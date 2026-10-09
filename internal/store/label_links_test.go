package store_test

import (
	"context"
	"database/sql"
	"errors"
	"slices"
	"testing"

	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/store"
	"github.com/mkappworks-dev/cloudzilla-app/internal/testutil"
)

type labelFixture struct {
	db      *sql.DB
	ls      *store.LabelStore
	repoID  int64
	ownerID int64
}

func newLabelFixture(t *testing.T) labelFixture {
	t.Helper()
	db := testutil.OpenTestDB(t)
	suffix := testutil.UniqueSuffix(t)
	ownerID := testutil.SeedUser(t, db, suffix)
	repoID := testutil.SeedRepo(t, db, ownerID, "testuser_"+suffix, suffix)
	return labelFixture{db: db, ls: store.NewLabelStore(db), repoID: repoID, ownerID: ownerID}
}

func (f labelFixture) label(t *testing.T, name string) *model.Label {
	t.Helper()
	l := &model.Label{RepoID: f.repoID, Name: name, Color: "#ffffff", Description: "d " + name}
	if err := f.ls.Create(context.Background(), l); err != nil {
		t.Fatalf("Create %s: %v", name, err)
	}
	return l
}

func labelNames(ls []model.Label) []string {
	names := make([]string, len(ls))
	for i, l := range ls {
		names[i] = l.Name
	}
	return names
}

func TestLabelStore_Create_DuplicateNameInRepoRejected(t *testing.T) {
	f := newLabelFixture(t)
	f.label(t, "bug")

	err := f.ls.Create(context.Background(), &model.Label{RepoID: f.repoID, Name: "bug", Color: "#000000"})
	wantUniqueViolation(t, "duplicate label name in one repo", err)
}

func TestLabelStore_Create_SameNameInOtherRepoAllowed(t *testing.T) {
	f := newLabelFixture(t)
	suffix := testutil.UniqueSuffix(t)
	other := testutil.SeedRepo(t, f.db, f.ownerID, "testuser_"+suffix, suffix)
	f.label(t, "bug")

	if err := f.ls.Create(context.Background(), &model.Label{RepoID: other, Name: "bug", Color: "#000000"}); err != nil {
		t.Fatalf("same name in another repo: %v", err)
	}
}

func TestLabelStore_GetByID(t *testing.T) {
	f := newLabelFixture(t)
	ctx := context.Background()
	l := f.label(t, "docs")
	suffix := testutil.UniqueSuffix(t)
	other := testutil.SeedRepo(t, f.db, f.ownerID, "testuser_"+suffix, suffix)

	got, err := f.ls.GetByID(ctx, l.ID, f.repoID)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if got.Name != "docs" || got.Color != "#ffffff" || got.Description != "d docs" || got.RepoID != f.repoID {
		t.Errorf("got %+v", got)
	}

	tests := []struct {
		name       string
		id, repoID int64
	}{
		{"label from another repo", l.ID, other},
		{"unknown id", l.ID + 1_000_000, f.repoID},
	}
	for _, tc := range tests {
		_, err := f.ls.GetByID(ctx, tc.id, tc.repoID)
		if !errors.Is(err, sql.ErrNoRows) {
			t.Errorf("%s: err = %v, want sql.ErrNoRows", tc.name, err)
		}
	}
}

func TestLabelStore_Delete_WrongRepoKeepsLabel(t *testing.T) {
	f := newLabelFixture(t)
	ctx := context.Background()
	l := f.label(t, "keep")

	if err := f.ls.Delete(ctx, l.ID, f.repoID+1_000_000); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := f.ls.GetByID(ctx, l.ID, f.repoID); err != nil {
		t.Errorf("label deleted through another repo's id: %v", err)
	}
}

func TestLabelStore_Delete_CascadesAssociations(t *testing.T) {
	f := newLabelFixture(t)
	ctx := context.Background()
	l := f.label(t, "gone")
	pullID := seedPullRow(t, f.db, f.repoID, f.ownerID, 1, "open")
	if err := f.ls.AddToPull(ctx, pullID, l.ID); err != nil {
		t.Fatalf("AddToPull: %v", err)
	}
	if err := f.ls.Delete(ctx, l.ID, f.repoID); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	got, err := f.ls.ListByPull(ctx, pullID)
	if err != nil || len(got) != 0 {
		t.Errorf("ListByPull after label delete = %v, %v; want empty", got, err)
	}
}

func TestLabelStore_ListByRepo_OrderedByNameAndScoped(t *testing.T) {
	f := newLabelFixture(t)
	ctx := context.Background()
	f.label(t, "zeta")
	f.label(t, "alpha")
	f.label(t, "mid")

	got, err := f.ls.ListByRepo(ctx, f.repoID)
	if err != nil {
		t.Fatalf("ListByRepo: %v", err)
	}
	if want := []string{"alpha", "mid", "zeta"}; !slices.Equal(labelNames(got), want) {
		t.Errorf("names = %v, want %v", labelNames(got), want)
	}

	empty, err := f.ls.ListByRepo(ctx, f.repoID+1_000_000)
	if err != nil || len(empty) != 0 {
		t.Errorf("unknown repo = %v, %v; want empty", empty, err)
	}
}

func TestLabelStore_PullLabels(t *testing.T) {
	f := newLabelFixture(t)
	ctx := context.Background()
	b, a := f.label(t, "b"), f.label(t, "a")
	pullID := seedPullRow(t, f.db, f.repoID, f.ownerID, 1, "open")

	for _, l := range []*model.Label{b, a, b} {
		if err := f.ls.AddToPull(ctx, pullID, l.ID); err != nil {
			t.Fatalf("AddToPull (idempotent): %v", err)
		}
	}
	got, err := f.ls.ListByPull(ctx, pullID)
	if err != nil {
		t.Fatalf("ListByPull: %v", err)
	}
	if want := []string{"a", "b"}; !slices.Equal(labelNames(got), want) {
		t.Errorf("names = %v, want %v", labelNames(got), want)
	}

	if err := f.ls.RemoveFromPull(ctx, pullID, a.ID); err != nil {
		t.Fatalf("RemoveFromPull: %v", err)
	}
	if err := f.ls.RemoveFromPull(ctx, pullID, a.ID); err != nil {
		t.Fatalf("RemoveFromPull on absent link: %v", err)
	}
	got, _ = f.ls.ListByPull(ctx, pullID)
	if want := []string{"b"}; !slices.Equal(labelNames(got), want) {
		t.Errorf("after remove names = %v, want %v", labelNames(got), want)
	}
}

func TestLabelStore_DiscussionLabels(t *testing.T) {
	f := newLabelFixture(t)
	ctx := context.Background()
	b, a := f.label(t, "b"), f.label(t, "a")

	var discussionID int64
	err := f.db.QueryRowContext(ctx,
		`INSERT INTO discussions (repo_id, category_id, number, title, author_id)
		 VALUES ($1, (SELECT id FROM discussion_categories ORDER BY id LIMIT 1), 1, 't', $2) RETURNING id`,
		f.repoID, f.ownerID,
	).Scan(&discussionID)
	if err != nil {
		t.Fatalf("seed discussion: %v", err)
	}

	for _, l := range []*model.Label{b, a, a} {
		if err := f.ls.AddToDiscussion(ctx, discussionID, l.ID); err != nil {
			t.Fatalf("AddToDiscussion (idempotent): %v", err)
		}
	}
	got, err := f.ls.ListByDiscussion(ctx, discussionID)
	if err != nil {
		t.Fatalf("ListByDiscussion: %v", err)
	}
	if want := []string{"a", "b"}; !slices.Equal(labelNames(got), want) {
		t.Errorf("names = %v, want %v", labelNames(got), want)
	}

	if err := f.ls.RemoveFromDiscussion(ctx, discussionID, b.ID); err != nil {
		t.Fatalf("RemoveFromDiscussion: %v", err)
	}
	got, _ = f.ls.ListByDiscussion(ctx, discussionID)
	if want := []string{"a"}; !slices.Equal(labelNames(got), want) {
		t.Errorf("after remove names = %v, want %v", labelNames(got), want)
	}
}

func TestLabelStore_ListByIssueIDs(t *testing.T) {
	f := newLabelFixture(t)
	ctx := context.Background()
	x, y := f.label(t, "x"), f.label(t, "y")
	issue1 := seedIssueRow(t, f.db, f.repoID, f.ownerID, 1, "open")
	issue2 := seedIssueRow(t, f.db, f.repoID, f.ownerID, 2, "open")
	unlabelled := seedIssueRow(t, f.db, f.repoID, f.ownerID, 3, "open")
	for _, link := range []struct{ issue, label int64 }{{issue1, y.ID}, {issue1, x.ID}, {issue2, y.ID}} {
		if err := f.ls.AddToIssue(ctx, link.issue, link.label); err != nil {
			t.Fatalf("AddToIssue: %v", err)
		}
	}

	t.Run("empty input", func(t *testing.T) {
		got, err := f.ls.ListByIssueIDs(ctx, nil)
		if err != nil || got == nil || len(got) != 0 {
			t.Errorf("got %v, %v; want empty non-nil map", got, err)
		}
	})

	t.Run("groups by issue, ordered by name, omits unlabelled", func(t *testing.T) {
		got, err := f.ls.ListByIssueIDs(ctx, []int64{issue1, issue2, unlabelled})
		if err != nil {
			t.Fatalf("ListByIssueIDs: %v", err)
		}
		if want := []string{"x", "y"}; !slices.Equal(labelNames(got[issue1]), want) {
			t.Errorf("issue1 = %v, want %v", labelNames(got[issue1]), want)
		}
		if want := []string{"y"}; !slices.Equal(labelNames(got[issue2]), want) {
			t.Errorf("issue2 = %v, want %v", labelNames(got[issue2]), want)
		}
		if _, ok := got[unlabelled]; ok {
			t.Error("unlabelled issue must not appear in the map")
		}
	})
}

func TestLabelStore_ListByPullIDs(t *testing.T) {
	f := newLabelFixture(t)
	ctx := context.Background()
	x, y := f.label(t, "x"), f.label(t, "y")
	pull1 := seedPullRow(t, f.db, f.repoID, f.ownerID, 1, "open")
	pull2 := seedPullRow(t, f.db, f.repoID, f.ownerID, 2, "open")
	unlabelled := seedPullRow(t, f.db, f.repoID, f.ownerID, 3, "open")
	for _, link := range []struct{ pull, label int64 }{{pull1, y.ID}, {pull1, x.ID}, {pull2, y.ID}} {
		if err := f.ls.AddToPull(ctx, link.pull, link.label); err != nil {
			t.Fatalf("AddToPull: %v", err)
		}
	}

	t.Run("empty input", func(t *testing.T) {
		got, err := f.ls.ListByPullIDs(ctx, []int64{})
		if err != nil || got == nil || len(got) != 0 {
			t.Errorf("got %v, %v; want empty non-nil map", got, err)
		}
	})

	t.Run("groups by pull, ordered by name, omits unlabelled", func(t *testing.T) {
		got, err := f.ls.ListByPullIDs(ctx, []int64{pull1, pull2, unlabelled})
		if err != nil {
			t.Fatalf("ListByPullIDs: %v", err)
		}
		if want := []string{"x", "y"}; !slices.Equal(labelNames(got[pull1]), want) {
			t.Errorf("pull1 = %v, want %v", labelNames(got[pull1]), want)
		}
		if want := []string{"y"}; !slices.Equal(labelNames(got[pull2]), want) {
			t.Errorf("pull2 = %v, want %v", labelNames(got[pull2]), want)
		}
		if _, ok := got[unlabelled]; ok {
			t.Error("unlabelled pull must not appear in the map")
		}
	})
}
