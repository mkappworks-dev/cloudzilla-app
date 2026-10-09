package service_test

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/store"
	"github.com/mkappworks-dev/cloudzilla-app/internal/testutil"
)

func descriptionHTMLFor(t *testing.T, e *projBoardEnv, issueNumbers []int, description string) string {
	t.Helper()
	ctx := context.Background()
	p := e.project(t, "board")
	col := e.column(t, p.ID, "todo")
	db := testutil.OpenTestDB(t)
	for _, n := range issueNumbers {
		var id int64
		if err := db.QueryRowContext(ctx,
			`INSERT INTO issues (repo_id, number, author_id, title, state) VALUES ($1, $2, $3, 't', 'open') RETURNING id`,
			e.repoID, n, e.ownerID).Scan(&id); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { testutil.Exec(t, db, `DELETE FROM issues WHERE id = $1`, id) })
	}
	card := e.note(t, p.ID, col.ID, "x")
	if err := store.NewProjectStore(db).SetCardDetails(ctx, card.ID, p.ID,
		model.CardDetails{Title: "x", Description: description}); err != nil {
		t.Fatal(err)
	}
	cols, err := e.svc.ListColumnsWithCardsExpanded(ctx, p.ID)
	if err != nil {
		t.Fatalf("board failed to load: %v", err)
	}
	return cols[0].Cards[0].DescriptionHTML
}

func TestListColumnsWithCardsExpanded_HugeRefNumberStillLoads(t *testing.T) {
	e := newProjBoardEnv(t)
	got := descriptionHTMLFor(t, e, []int{7}, "see #3000000000 and #7")
	if !strings.Contains(got, `/issues/7"`) || !strings.Contains(got, "#3000000000") {
		t.Errorf("DescriptionHTML = %q", got)
	}
}

func TestListColumnsWithCardsExpanded_RefCapKeepsFirstRefs(t *testing.T) {
	e := newProjBoardEnv(t)
	var sb strings.Builder
	for n := 1; n <= 250; n++ {
		fmt.Fprintf(&sb, "#%d ", n)
	}
	got := descriptionHTMLFor(t, e, []int{1, 250}, sb.String())
	if !strings.Contains(got, `/issues/1"`) {
		t.Errorf("first ref not linked")
	}
	if strings.Contains(got, `/issues/250"`) {
		t.Errorf("ref past the cap was linked")
	}
}
