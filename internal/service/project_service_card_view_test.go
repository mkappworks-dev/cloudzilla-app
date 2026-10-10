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

	cols, err := e.svc.ListColumnsWithCardsExpanded(ctx, p.ID, &e.ownerID)
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

	cols, err := e.svc.ListColumnsWithCardsExpanded(ctx, p.ID, &e.ownerID)
	if err != nil {
		t.Fatal(err)
	}
	c := cols[0].Cards[0]
	if c.Kind != "note" || c.Title != "custom" || c.LinkKind != "issue" || c.LinkNumber != 7 || c.LinkState != "open" ||
		c.LinkID != issueID || c.LinkTitle != "issue title" {
		t.Errorf("card = %+v", c)
	}
}

func TestListColumnsWithCardsExpanded_PlainLinkedCardsShowTheLinkedItemsPeople(t *testing.T) {
	e := newProjBoardEnv(t)
	ctx := context.Background()
	p := e.project(t, "board")
	col := e.column(t, p.ID, "todo")
	db := testutil.OpenTestDB(t)
	scan := func(q string, args ...any) int64 {
		t.Helper()
		var id int64
		if err := db.QueryRowContext(ctx, q, args...).Scan(&id); err != nil {
			t.Fatal(err)
		}
		return id
	}
	bug := scan(`INSERT INTO labels (repo_id, name) VALUES ($1, 'bug') RETURNING id`, e.repoID)
	feature := scan(`INSERT INTO labels (repo_id, name) VALUES ($1, 'feature') RETURNING id`, e.repoID)
	issueID := scan(`INSERT INTO issues (repo_id, number, author_id, title, state) VALUES ($1, 7, $2, 'an issue', 'open') RETURNING id`, e.repoID, e.ownerID)
	pullID := scan(`INSERT INTO pull_requests (repo_id, number, author_id, title, state, head_branch, base_branch) VALUES ($1, 8, $2, 'a pull', 'open', 'f', 'main') RETURNING id`, e.repoID, e.ownerID)
	t.Cleanup(func() {
		testutil.Exec(t, db, `DELETE FROM issues WHERE id = $1`, issueID)
		testutil.Exec(t, db, `DELETE FROM pull_requests WHERE id = $1`, pullID)
		testutil.Exec(t, db, `DELETE FROM labels WHERE id IN ($1, $2)`, bug, feature)
	})
	testutil.Exec(t, db, `INSERT INTO issue_labels (issue_id, label_id) VALUES ($1, $2)`, issueID, bug)
	testutil.Exec(t, db, `INSERT INTO issue_assignees (issue_id, user_id) VALUES ($1, $2)`, issueID, e.writerID)
	testutil.Exec(t, db, `INSERT INTO pull_labels (pull_id, label_id) VALUES ($1, $2)`, pullID, bug)
	testutil.Exec(t, db, `INSERT INTO pull_assignees (pull_id, user_id) VALUES ($1, $2)`, pullID, e.ownerID)

	if _, err := e.svc.CreateCard(ctx, p.ID, col.ID, e.ownerID, model.CardDetails{IssueID: &issueID}); err != nil {
		t.Fatal(err)
	}
	if _, err := e.svc.CreateCard(ctx, p.ID, col.ID, e.ownerID, model.CardDetails{PullID: &pullID}); err != nil {
		t.Fatal(err)
	}
	if _, err := e.svc.CreateCard(ctx, p.ID, col.ID, e.ownerID, model.CardDetails{Title: "mine", IssueID: &issueID, LabelIDs: []int64{feature}}); err != nil {
		t.Fatal(err)
	}

	cols, err := e.svc.ListColumnsWithCardsExpanded(ctx, p.ID, &e.ownerID)
	if err != nil {
		t.Fatal(err)
	}
	issueCard, pullCard, noteCard := cols[0].Cards[0], cols[0].Cards[1], cols[0].Cards[2]
	if len(issueCard.Labels) != 1 || issueCard.Labels[0].ID != bug || len(issueCard.Assignees) != 1 || issueCard.Assignees[0].ID != e.writerID {
		t.Errorf("issue card labels/assignees = %v / %v", issueCard.Labels, issueCard.Assignees)
	}
	if len(pullCard.Labels) != 1 || pullCard.Labels[0].ID != bug || len(pullCard.Assignees) != 1 || pullCard.Assignees[0].ID != e.ownerID {
		t.Errorf("pull card labels/assignees = %v / %v", pullCard.Labels, pullCard.Assignees)
	}
	if len(noteCard.Labels) != 1 || noteCard.Labels[0].ID != feature || len(noteCard.Assignees) != 0 {
		t.Errorf("titled card labels/assignees = %v / %v, want only its own label", noteCard.Labels, noteCard.Assignees)
	}
}

func TestListColumnsWithCardsExpanded_ConvertedCardKeepsLabelsAndAssignees(t *testing.T) {
	e := newProjBoardEnv(t)
	ctx := context.Background()
	p := e.project(t, "board")
	col := e.column(t, p.ID, "todo")
	db := testutil.OpenTestDB(t)
	var labelID int64
	if err := db.QueryRowContext(ctx, `INSERT INTO labels (repo_id, name) VALUES ($1, 'bug') RETURNING id`, e.repoID).Scan(&labelID); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { testutil.Exec(t, db, `DELETE FROM labels WHERE id = $1`, labelID) })
	card, err := e.svc.CreateCard(ctx, p.ID, col.ID, e.ownerID, model.CardDetails{Title: "convert me", AssigneeIDs: []int64{e.writerID}, LabelIDs: []int64{labelID}})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := e.svc.ConvertCardToIssue(ctx, p.ID, card.ID, e.ownerID); err != nil {
		t.Fatal(err)
	}

	cols, err := e.svc.ListColumnsWithCardsExpanded(ctx, p.ID, &e.ownerID)
	if err != nil {
		t.Fatal(err)
	}
	c := cols[0].Cards[0]
	if c.Kind != "note" || c.Title != "convert me" || c.LinkKind != "issue" || c.LinkNumber == 0 {
		t.Errorf("converted card = %+v, want a titled card linked to the new issue", c)
	}
	if len(c.Labels) != 1 || c.Labels[0].ID != labelID || len(c.Assignees) != 1 || c.Assignees[0].ID != e.writerID {
		t.Errorf("converted card = %+v, want its own label and assignee", c)
	}
}
