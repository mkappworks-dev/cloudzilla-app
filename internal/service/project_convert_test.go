package service_test

import (
	"context"
	"database/sql"
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

	wins := 0
	for _, err := range errs {
		if err == nil {
			wins++
		}
	}
	if wins != 1 {
		t.Fatalf("wins = %d (errors %v), want 1", wins, errs)
	}
	var n int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM issues WHERE repo_id = $1`, e.repoID).Scan(&n); err != nil || n != 1 {
		t.Errorf("issues = %d (%v), want 1", n, err)
	}
}

// collideIssueNumbers makes the next `collisions` issue inserts in the repo reuse issue
// number 1, which a seeded issue already holds, so they fail on (repo_id, number).
// A sequence counts the inserts because its increments survive the failed statement.
func collideIssueNumbers(t *testing.T, db *sql.DB, repoID, authorID int64, collisions int) {
	t.Helper()
	testutil.Exec(t, db, `INSERT INTO issues (repo_id, number, author_id, title, body, state, visibility) VALUES ($1, 1, $2, 'existing', '', 'open', 'public')`, repoID, authorID)
	name := fmt.Sprintf("collide_issue_%d", repoID)
	testutil.Exec(t, db, fmt.Sprintf(`CREATE SEQUENCE %s`, name))
	testutil.Exec(t, db, fmt.Sprintf(`CREATE FUNCTION %s() RETURNS trigger AS $$ BEGIN
		IF nextval('%s') <= %d THEN NEW.number := 1; END IF;
		RETURN NEW; END $$ LANGUAGE plpgsql`, name, name, collisions))
	testutil.Exec(t, db, fmt.Sprintf(`CREATE TRIGGER %s BEFORE INSERT ON issues FOR EACH ROW WHEN (NEW.repo_id = %d AND NEW.title <> 'existing') EXECUTE FUNCTION %s()`, name, repoID, name))
	t.Cleanup(func() {
		testutil.Exec(t, db, fmt.Sprintf(`DROP TRIGGER IF EXISTS %s ON issues`, name))
		testutil.Exec(t, db, fmt.Sprintf(`DROP FUNCTION IF EXISTS %s()`, name))
		testutil.Exec(t, db, fmt.Sprintf(`DROP SEQUENCE IF EXISTS %s`, name))
	})
}

func TestProjectService_ConvertCardToIssue_RetriesOnIssueNumberCollision(t *testing.T) {
	e := newProjBoardEnv(t)
	ctx := context.Background()
	db := testutil.OpenTestDB(t)
	p := e.project(t, "board")
	col := e.column(t, p.ID, "todo")
	card := e.note(t, p.ID, col.ID, "collide me\nbody")
	collideIssueNumbers(t, db, e.repoID, e.ownerID, 1)

	got, err := e.svc.ConvertCardToIssue(ctx, p.ID, card.ID, e.ownerID)
	if err != nil {
		t.Fatalf("ConvertCardToIssue: %v", err)
	}
	if got.IssueID == nil {
		t.Fatal("card not linked after the retry")
	}
	var n int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM issues WHERE repo_id = $1 AND title = 'collide me'`, e.repoID).Scan(&n); err != nil || n != 1 {
		t.Errorf("converted issues = %d (%v), want 1", n, err)
	}
}

func TestProjectService_ConvertCardToIssue_GivesUpAfterRepeatedCollisions(t *testing.T) {
	e := newProjBoardEnv(t)
	ctx := context.Background()
	db := testutil.OpenTestDB(t)
	p := e.project(t, "board")
	col := e.column(t, p.ID, "todo")
	card := e.note(t, p.ID, col.ID, "collide me\nbody")
	collideIssueNumbers(t, db, e.repoID, e.ownerID, 1000)

	if _, err := e.svc.ConvertCardToIssue(ctx, p.ID, card.ID, e.ownerID); err == nil {
		t.Fatal("ConvertCardToIssue succeeded, want the unique violation")
	}
	var n int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM issues WHERE repo_id = $1 AND title <> 'existing'`, e.repoID).Scan(&n); err != nil || n != 0 {
		t.Errorf("converted issues = %d (%v), want 0", n, err)
	}
	var issueID sql.NullInt64
	if err := db.QueryRowContext(ctx, `SELECT issue_id FROM project_cards WHERE id = $1`, card.ID).Scan(&issueID); err != nil || issueID.Valid {
		t.Errorf("card issue_id = %v (%v), want NULL", issueID, err)
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
