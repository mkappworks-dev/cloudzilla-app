package service_test

import (
	"context"
	"testing"
	"time"

	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/store"
	"github.com/mkappworks-dev/cloudzilla-app/internal/testutil"
)

func TestListColumnsWithCardsExpanded_CardDetails(t *testing.T) {
	e := newProjBoardEnv(t)
	ctx := context.Background()
	p := e.project(t, "board")
	col := e.column(t, p.ID, "todo")
	plain := e.note(t, p.ID, col.ID, "head\nbody")
	linked := e.note(t, p.ID, col.ID, "second")

	db := testutil.OpenTestDB(t)
	ps := store.NewProjectStore(db)
	var labelID int64
	if err := db.QueryRowContext(ctx, `INSERT INTO labels (repo_id, name) VALUES ($1, 'bug') RETURNING id`, e.repoID).Scan(&labelID); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { testutil.Exec(t, db, `DELETE FROM labels WHERE id = $1`, labelID) })

	past := time.Now().UTC().AddDate(0, 0, -3)
	if err := ps.SetCardDetails(ctx, plain.ID, p.ID, model.CardDetails{
		Title: "head", Description: "body", DueDate: &past,
		AssigneeIDs: []int64{e.writerID}, LabelIDs: []int64{labelID},
	}); err != nil {
		t.Fatal(err)
	}
	future := time.Now().UTC().AddDate(0, 0, 3)
	if err := ps.SetCardDetails(ctx, linked.ID, p.ID, model.CardDetails{Title: "second", DueDate: &future}); err != nil {
		t.Fatal(err)
	}

	cols, err := e.svc.ListColumnsWithCardsExpanded(ctx, p.ID)
	if err != nil {
		t.Fatal(err)
	}
	cards := cols[0].Cards
	if len(cards) != 2 {
		t.Fatalf("cards = %d, want 2", len(cards))
	}
	a, b := cards[0], cards[1]
	if a.Kind != "note" || a.Title != "head" || a.Description != "body" {
		t.Errorf("first card = %+v", a)
	}
	if !a.Overdue || a.DueDate != past.Format("2006-01-02") {
		t.Errorf("first card due = %q overdue = %v, want past and overdue", a.DueDate, a.Overdue)
	}
	if len(a.Assignees) != 1 || a.Assignees[0].ID != e.writerID || len(a.Labels) != 1 || a.Labels[0].ID != labelID {
		t.Errorf("first card assignees/labels = %v / %v", a.Assignees, a.Labels)
	}
	if b.Overdue || len(b.Assignees) != 0 || len(b.Labels) != 0 {
		t.Errorf("second card = %+v, want not overdue and no assignees or labels", b)
	}
}

func TestListColumnsWithCardsExpanded_LinkedCardWithTitle(t *testing.T) {
	e := newProjBoardEnv(t)
	ctx := context.Background()
	p := e.project(t, "board")
	col := e.column(t, p.ID, "todo")
	db := testutil.OpenTestDB(t)
	var issueID int64
	if err := db.QueryRowContext(ctx,
		`INSERT INTO issues (repo_id, number, author_id, title, state) VALUES ($1, 7, $2, 'issue title', 'open') RETURNING id`,
		e.repoID, e.ownerID).Scan(&issueID); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { testutil.Exec(t, db, `DELETE FROM issues WHERE id = $1`, issueID) })
	plainLinked, err := e.svc.CreateCard(ctx, p.ID, col.ID, e.ownerID, model.CardDetails{IssueID: &issueID})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.NewProjectStore(db).SetCardDetails(ctx, plainLinked.ID, p.ID,
		model.CardDetails{Title: "custom", IssueID: &issueID}); err != nil {
		t.Fatal(err)
	}

	cols, err := e.svc.ListColumnsWithCardsExpanded(ctx, p.ID)
	if err != nil {
		t.Fatal(err)
	}
	c := cols[0].Cards[0]
	if c.Kind != "note" || c.Title != "custom" || c.LinkKind != "issue" || c.LinkNumber != 7 || c.LinkState != "open" ||
		c.LinkID != issueID || c.LinkTitle != "issue title" {
		t.Errorf("card = %+v", c)
	}
}
