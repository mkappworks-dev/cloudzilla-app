package service_test

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/service"
	"github.com/mkappworks-dev/cloudzilla-app/internal/testutil"
)

// privateCardEnv is a board whose private issue #embargoNumber, authored by the
// owner, carries a label and an assignee; otherID cannot see it, writerID can.
type privateCardEnv struct {
	*projBoardEnv
	projectID, columnID, issueID int64
}

const embargoNumber = 41

func newPrivateCardEnv(t *testing.T) privateCardEnv {
	t.Helper()
	e := newProjBoardEnv(t)
	ctx := context.Background()
	p := e.project(t, "Board")
	col := e.column(t, p.ID, "Todo")
	db := testutil.OpenTestDB(t)
	var labelID, issueID int64
	if err := db.QueryRowContext(ctx, `INSERT INTO labels (repo_id, name) VALUES ($1, 'security') RETURNING id`, e.repoID).Scan(&labelID); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRowContext(ctx,
		`INSERT INTO issues (repo_id, number, author_id, title, state, visibility) VALUES ($1, $2, $3, 'embargoed fix', 'open', 'private') RETURNING id`,
		e.repoID, embargoNumber, e.ownerID).Scan(&issueID); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		testutil.Exec(t, db, `DELETE FROM issues WHERE id = $1`, issueID)
		testutil.Exec(t, db, `DELETE FROM labels WHERE id = $1`, labelID)
	})
	testutil.Exec(t, db, `INSERT INTO issue_labels (issue_id, label_id) VALUES ($1, $2)`, issueID, labelID)
	testutil.Exec(t, db, `INSERT INTO issue_assignees (issue_id, user_id) VALUES ($1, $2)`, issueID, e.writerID)
	return privateCardEnv{projBoardEnv: e, projectID: p.ID, columnID: col.ID, issueID: issueID}
}

func (e privateCardEnv) card(t *testing.T, d model.CardDetails) {
	t.Helper()
	if _, err := e.svc.CreateCard(context.Background(), e.projectID, e.columnID, e.ownerID, d); err != nil {
		t.Fatal(err)
	}
}

func (e privateCardEnv) onlyCard(t *testing.T, viewer int64) service.KanbanCardView {
	t.Helper()
	cols, err := e.svc.ListColumnsWithCardsExpanded(context.Background(), e.projectID, &viewer)
	if err != nil {
		t.Fatal(err)
	}
	if len(cols[0].Cards) != 1 {
		t.Fatalf("cards = %d, want 1", len(cols[0].Cards))
	}
	return cols[0].Cards[0]
}

func TestBoard_PlainCardOfHiddenIssueCarriesNothingButItsID(t *testing.T) {
	e := newPrivateCardEnv(t)
	e.card(t, model.CardDetails{IssueID: &e.issueID, Description: "the login XSS"})

	if c := e.onlyCard(t, e.writerID); c.Kind != "issue" || len(c.Labels) != 1 || len(c.Assignees) != 1 {
		t.Fatalf("writer sees %+v, want the issue with its label and assignee", c)
	}
	got := e.onlyCard(t, e.otherID)
	want := service.KanbanCardView{ID: got.ID, Kind: "hidden", RepoFullName: got.RepoFullName, ColumnID: e.columnID}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("hidden card = %+v, want only %+v", got, want)
	}
}

func TestBoard_TitledCardKeepsItsOwnFieldsButDropsAHiddenLink(t *testing.T) {
	e := newPrivateCardEnv(t)
	e.card(t, model.CardDetails{Title: "Ship it", Description: "notes", IssueID: &e.issueID})

	if c := e.onlyCard(t, e.writerID); c.LinkKind != "issue" || c.LinkNumber != embargoNumber {
		t.Fatalf("writer sees %+v, want the link to #%d", c, embargoNumber)
	}
	c := e.onlyCard(t, e.otherID)
	if c.Kind != "note" || c.Title != "Ship it" || c.Description != "notes" {
		t.Errorf("titled card = %+v, want its own title and description", c)
	}
	if c.LinkKind != "" || c.LinkID != 0 || c.LinkNumber != 0 || c.LinkState != "" || c.LinkTitle != "" || c.Number != 0 || c.State != "" {
		t.Errorf("titled card leaks its hidden link: %+v", c)
	}
}

func TestBoard_DescriptionDoesNotAutolinkAHiddenIssue(t *testing.T) {
	e := newPrivateCardEnv(t)
	e.card(t, model.CardDetails{Title: "Plan", Description: "blocked on #41"})
	link := "/issues/41"

	if c := e.onlyCard(t, e.writerID); !strings.Contains(c.DescriptionHTML, link) {
		t.Fatalf("writer's description %q does not link #41", c.DescriptionHTML)
	}
	if c := e.onlyCard(t, e.otherID); strings.Contains(c.DescriptionHTML, link) || !strings.Contains(c.DescriptionHTML, "#41") {
		t.Errorf("reader's description = %q, want #41 as plain text", c.DescriptionHTML)
	}
}
