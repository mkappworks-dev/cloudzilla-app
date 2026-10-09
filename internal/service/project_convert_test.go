package service_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
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

func TestProjectService_ConvertCardToIssue_ConcurrentConvertsCreateOneIssue(t *testing.T) {
	e := newProjBoardEnv(t)
	ctx := context.Background()
	db := testutil.OpenTestDB(t)
	p := e.project(t, "board")
	col := e.column(t, p.ID, "todo")
	card := e.note(t, p.ID, col.ID, "race me\nbody")

	errs := make([]error, 2)
	start := make(chan struct{})
	var wg sync.WaitGroup
	for i := range errs {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			_, errs[i] = e.svc.ConvertCardToIssue(ctx, p.ID, card.ID, e.ownerID)
		}()
	}
	close(start)
	wg.Wait()

	wins, losses := 0, 0
	for _, err := range errs {
		switch {
		case err == nil:
			wins++
		case errors.Is(err, service.ErrNotConvertible):
			losses++
		default:
			t.Errorf("unexpected error: %v", err)
		}
	}
	if wins != 1 || losses != 1 {
		t.Fatalf("wins = %d, losses = %d, want 1 and 1", wins, losses)
	}
	var n int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM issues WHERE repo_id = $1`, e.repoID).Scan(&n); err != nil || n != 1 {
		t.Errorf("issues = %d (%v), want 1", n, err)
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
