package service_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/mkappworks-dev/cloudzilla-app/internal/service"
	"github.com/mkappworks-dev/cloudzilla-app/internal/testutil"
)

func TestProjectService_ConvertCardToIssue_RollsBackWhenLinkFails(t *testing.T) {
	e := newProjBoardEnv(t)
	ctx := context.Background()
	db := testutil.OpenTestDB(t)
	p := e.project(t, "board")
	col := e.column(t, p.ID, "todo")
	card := e.note(t, p.ID, col.ID, "convert me\nbody")

	fn := fmt.Sprintf("reject_card_%d", card.ID)
	testutil.Exec(t, db, fmt.Sprintf(`CREATE FUNCTION %s() RETURNS trigger AS $$ BEGIN RAISE EXCEPTION 'link rejected'; END $$ LANGUAGE plpgsql`, fn))
	testutil.Exec(t, db, fmt.Sprintf(`CREATE TRIGGER %s BEFORE UPDATE ON project_cards FOR EACH ROW WHEN (NEW.id = %d) EXECUTE FUNCTION %s()`, fn, card.ID, fn))
	t.Cleanup(func() {
		testutil.Exec(t, db, fmt.Sprintf(`DROP TRIGGER IF EXISTS %s ON project_cards`, fn))
		testutil.Exec(t, db, fmt.Sprintf(`DROP FUNCTION IF EXISTS %s()`, fn))
	})

	if _, err := e.svc.ConvertCardToIssue(ctx, p.ID, card.ID, e.ownerID); err == nil {
		t.Fatal("ConvertCardToIssue succeeded, want the link failure")
	}
	var n int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM issues WHERE repo_id = $1`, e.repoID).Scan(&n); err != nil || n != 0 {
		t.Errorf("issues after failed convert = %d (%v), want 0", n, err)
	}
}

func TestProjectService_ConvertCardToIssue_TitleTooLong(t *testing.T) {
	e := newProjBoardEnv(t)
	ctx := context.Background()
	db := testutil.OpenTestDB(t)
	p := e.project(t, "board")
	col := e.column(t, p.ID, "todo")
	card := e.note(t, p.ID, col.ID, strings.Repeat("é", 200))

	if _, err := e.svc.ConvertCardToIssue(ctx, p.ID, card.ID, e.ownerID); !errors.Is(err, service.ErrTitleTooLong) {
		t.Fatalf("err = %v, want ErrTitleTooLong", err)
	}
	var n int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM issues WHERE repo_id = $1`, e.repoID).Scan(&n); err != nil || n != 0 {
		t.Errorf("issues = %d (%v), want 0", n, err)
	}
}
