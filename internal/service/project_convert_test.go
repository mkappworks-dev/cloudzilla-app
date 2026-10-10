package service_test

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/service"
	"github.com/mkappworks-dev/cloudzilla-app/internal/testutil"
)

type cardSnapshot struct {
	title, note, due, assignees, labels string
	issueID                             sql.NullInt64
}

func snapshotCard(t *testing.T, db *sql.DB, cardID int64) cardSnapshot {
	t.Helper()
	var s cardSnapshot
	err := db.QueryRow(`SELECT c.title, c.note, COALESCE(c.due_date::text, ''), c.issue_id,
		COALESCE((SELECT string_agg(user_id::text, ',' ORDER BY user_id) FROM card_assignees WHERE card_id = c.id), ''),
		COALESCE((SELECT string_agg(label_id::text, ',' ORDER BY label_id) FROM card_labels WHERE card_id = c.id), '')
		FROM project_cards c WHERE c.id = $1`, cardID).Scan(&s.title, &s.note, &s.due, &s.issueID, &s.assignees, &s.labels)
	if err != nil {
		t.Fatalf("snapshot card %d: %v", cardID, err)
	}
	return s
}

// convertableCard is a titled card with a description, due date, assignee and label.
func (e *projBoardEnv) convertableCard(t *testing.T, db *sql.DB) (projectID, cardID int64) {
	t.Helper()
	p := e.project(t, "board")
	col := e.column(t, p.ID, "todo")
	var labelID int64
	if err := db.QueryRow(`INSERT INTO labels (repo_id, name) VALUES ($1, 'bug') RETURNING id`, e.repoID).Scan(&labelID); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { testutil.Exec(t, db, `DELETE FROM labels WHERE id = $1`, labelID) })
	due := time.Date(2030, 1, 2, 0, 0, 0, 0, time.UTC)
	card, err := e.svc.CreateCard(context.Background(), p.ID, col.ID, e.ownerID, model.CardDetails{
		Title: "convert me", Description: "body", DueDate: &due, AssigneeIDs: []int64{e.writerID}, LabelIDs: []int64{labelID},
	})
	if err != nil {
		t.Fatal(err)
	}
	return p.ID, card.ID
}

func TestProjectService_ConvertCardToIssue_KeepsTheCardsOwnFields(t *testing.T) {
	e := newProjBoardEnv(t)
	ctx := context.Background()
	db := testutil.OpenTestDB(t)
	projectID, cardID := e.convertableCard(t, db)
	before := snapshotCard(t, db, cardID)

	_, issue, err := e.svc.ConvertCardToIssue(ctx, projectID, cardID, e.ownerID)
	if err != nil {
		t.Fatalf("ConvertCardToIssue: %v", err)
	}
	want := before
	want.issueID = sql.NullInt64{Int64: issue.ID, Valid: true}
	if after := snapshotCard(t, db, cardID); after != want {
		t.Errorf("card after convert = %+v, want %+v", after, want)
	}

	var title, body, labels, assignees string
	err = db.QueryRowContext(ctx, `SELECT i.title, i.body,
		COALESCE((SELECT string_agg(label_id::text, ',' ORDER BY label_id) FROM issue_labels WHERE issue_id = i.id), ''),
		COALESCE((SELECT string_agg(user_id::text, ',' ORDER BY user_id) FROM issue_assignees WHERE issue_id = i.id), '')
		FROM issues i WHERE i.id = $1`, issue.ID).Scan(&title, &body, &labels, &assignees)
	if err != nil {
		t.Fatal(err)
	}
	if title != before.title || body != before.note || labels != before.labels || assignees != before.assignees {
		t.Errorf("issue = %q %q labels %q assignees %q, want the card's %q %q %q %q",
			title, body, labels, assignees, before.title, before.note, before.labels, before.assignees)
	}
}

func TestProjectService_ConvertCardToIssue_RollsBackWhenLinkFails(t *testing.T) {
	e := newProjBoardEnv(t)
	ctx := context.Background()
	db := testutil.OpenTestDB(t)
	projectID, cardID := e.convertableCard(t, db)
	before := snapshotCard(t, db, cardID)
	if before.title != "convert me" || before.note != "body" || before.due != "2030-01-02" || before.assignees == "" || before.labels == "" || before.issueID.Valid {
		t.Fatalf("setup card = %+v", before)
	}

	fn := fmt.Sprintf("reject_card_%d", cardID)
	testutil.Exec(t, db, fmt.Sprintf(`CREATE FUNCTION %s() RETURNS trigger AS $$ BEGIN RAISE EXCEPTION 'link rejected'; END $$ LANGUAGE plpgsql`, fn))
	testutil.Exec(t, db, fmt.Sprintf(`CREATE TRIGGER %s BEFORE UPDATE ON project_cards FOR EACH ROW WHEN (NEW.id = %d) EXECUTE FUNCTION %s()`, fn, cardID, fn))
	t.Cleanup(func() {
		testutil.Exec(t, db, fmt.Sprintf(`DROP TRIGGER IF EXISTS %s ON project_cards`, fn))
		testutil.Exec(t, db, fmt.Sprintf(`DROP FUNCTION IF EXISTS %s()`, fn))
	})

	if _, _, err := e.svc.ConvertCardToIssue(ctx, projectID, cardID, e.ownerID); err == nil {
		t.Fatal("ConvertCardToIssue succeeded, want the link failure")
	}
	var n int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM issues WHERE repo_id = $1`, e.repoID).Scan(&n); err != nil || n != 0 {
		t.Errorf("issues after failed convert = %d (%v), want 0", n, err)
	}
	if after := snapshotCard(t, db, cardID); after != before {
		t.Errorf("card after rollback = %+v, want %+v", after, before)
	}
}

func TestProjectService_ConvertCardToIssue_KeepsIssueWhenCommitFails(t *testing.T) {
	e := newProjBoardEnv(t)
	ctx := context.Background()
	db := testutil.OpenTestDB(t)
	projectID, cardID := e.convertableCard(t, db)

	// A deferred constraint trigger fails at COMMIT, the one step whose outcome a caller can't know.
	fn := fmt.Sprintf("reject_commit_%d", cardID)
	testutil.Exec(t, db, fmt.Sprintf(`CREATE FUNCTION %s() RETURNS trigger AS $$ BEGIN RAISE EXCEPTION 'commit rejected'; END $$ LANGUAGE plpgsql`, fn))
	testutil.Exec(t, db, fmt.Sprintf(`CREATE CONSTRAINT TRIGGER %s AFTER UPDATE ON project_cards DEFERRABLE INITIALLY DEFERRED FOR EACH ROW WHEN (NEW.id = %d) EXECUTE FUNCTION %s()`, fn, cardID, fn))
	t.Cleanup(func() {
		testutil.Exec(t, db, fmt.Sprintf(`DROP TRIGGER IF EXISTS %s ON project_cards`, fn))
		testutil.Exec(t, db, fmt.Sprintf(`DROP FUNCTION IF EXISTS %s()`, fn))
	})

	if _, _, err := e.svc.ConvertCardToIssue(ctx, projectID, cardID, e.ownerID); err == nil {
		t.Fatal("ConvertCardToIssue succeeded, want the commit failure")
	}
	var n int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM issues WHERE repo_id = $1`, e.repoID).Scan(&n); err != nil || n != 1 {
		t.Errorf("issues after a failed commit = %d (%v), want the issue kept", n, err)
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
			_, _, errs[i] = e.svc.ConvertCardToIssue(ctx, p.ID, card.ID, e.ownerID)
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
	if got := snapshotCard(t, db, card.ID); got.title != "race me" || !got.issueID.Valid {
		t.Errorf("card after the race = %+v, want it titled and linked", got)
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

	got, _, err := e.svc.ConvertCardToIssue(ctx, p.ID, card.ID, e.ownerID)
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

	if _, _, err := e.svc.ConvertCardToIssue(ctx, p.ID, card.ID, e.ownerID); err == nil {
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

	if _, _, err := e.svc.ConvertCardToIssue(ctx, p.ID, card.ID, e.ownerID); !errors.Is(err, service.ErrTitleTooLong) {
		t.Fatalf("err = %v, want ErrTitleTooLong", err)
	}
	var n int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM issues WHERE repo_id = $1`, e.repoID).Scan(&n); err != nil || n != 0 {
		t.Errorf("issues = %d (%v), want 0", n, err)
	}
}
