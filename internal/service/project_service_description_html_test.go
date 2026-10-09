package service_test

import (
	"context"
	"strings"
	"testing"

	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/store"
	"github.com/mkappworks-dev/cloudzilla-app/internal/testutil"
)

func TestListColumnsWithCardsExpanded_DescriptionHTMLLinksRefs(t *testing.T) {
	e := newProjBoardEnv(t)
	ctx := context.Background()
	p := e.project(t, "board")
	col := e.column(t, p.ID, "todo")
	db := testutil.OpenTestDB(t)
	var issueID int64
	if err := db.QueryRowContext(ctx,
		`INSERT INTO issues (repo_id, number, author_id, title, state) VALUES ($1, 7, $2, 't', 'open') RETURNING id`,
		e.repoID, e.ownerID).Scan(&issueID); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { testutil.Exec(t, db, `DELETE FROM issues WHERE id = $1`, issueID) })

	card := e.note(t, p.ID, col.ID, "x")
	if err := store.NewProjectStore(db).SetCardDetails(ctx, card.ID, p.ID,
		model.CardDetails{Title: "x", Description: "fixes #7 but not #8"}); err != nil {
		t.Fatal(err)
	}
	empty := e.note(t, p.ID, col.ID, "y")
	if err := store.NewProjectStore(db).SetCardDetails(ctx, empty.ID, p.ID, model.CardDetails{Title: "y"}); err != nil {
		t.Fatal(err)
	}

	cols, err := e.svc.ListColumnsWithCardsExpanded(ctx, p.ID)
	if err != nil {
		t.Fatal(err)
	}
	want := `href="/` + e.owner + "/" + e.repoName + `/issues/7"`
	got := cols[0].Cards[0].DescriptionHTML
	if !strings.Contains(got, want) {
		t.Errorf("DescriptionHTML = %q, missing %q", got, want)
	}
	if strings.Contains(got, "/issues/8") || strings.Contains(got, "/pulls/8") {
		t.Errorf("DescriptionHTML = %q, linked an unknown number", got)
	}
	if cols[0].Cards[1].DescriptionHTML != "" {
		t.Errorf("empty description rendered %q", cols[0].Cards[1].DescriptionHTML)
	}
}
