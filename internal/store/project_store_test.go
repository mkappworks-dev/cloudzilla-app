package store_test

// MoveCard reorder + IDOR integration tests. Require TEST_DATABASE_DSN; skip otherwise.

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/store"
	"github.com/mkappworks-dev/cloudzilla-app/internal/testutil"
)

// seedProjectWithColumns inserts a project and N columns, returns (projectID, columnIDs).
func seedProjectWithColumns(t *testing.T, db *sql.DB, repoID int64, colNames []string) (int64, []int64) {
	t.Helper()
	ctx := context.Background()
	var projectID int64
	if err := db.QueryRowContext(ctx,
		`INSERT INTO projects (repo_id, name, description) VALUES ($1, $2, '') RETURNING id`,
		repoID, "kanban_test",
	).Scan(&projectID); err != nil {
		t.Fatalf("seed project: %v", err)
	}
	colIDs := make([]int64, len(colNames))
	for i, n := range colNames {
		if err := db.QueryRowContext(ctx,
			`INSERT INTO project_columns (project_id, name, position) VALUES ($1, $2, $3) RETURNING id`,
			projectID, n, i,
		).Scan(&colIDs[i]); err != nil {
			t.Fatalf("seed column %s: %v", n, err)
		}
	}
	t.Cleanup(func() {
		db.ExecContext(context.Background(), `DELETE FROM project_cards WHERE column_id IN (SELECT id FROM project_columns WHERE project_id = $1)`, projectID)
		db.ExecContext(context.Background(), `DELETE FROM project_columns WHERE project_id = $1`, projectID)
		db.ExecContext(context.Background(), `DELETE FROM projects WHERE id = $1`, projectID)
	})
	return projectID, colIDs
}

// seedCard inserts a note-only card into the given column at MAX+1.
func seedCard(t *testing.T, s *store.ProjectStore, columnID int64, note string) *model.ProjectCard {
	t.Helper()
	card := &model.ProjectCard{ColumnID: columnID, Note: note}
	if err := s.CreateCard(context.Background(), card); err != nil {
		t.Fatalf("seed card %q: %v", note, err)
	}
	return card
}

// readPositions returns (id, position) tuples for a column ordered by position.
func readPositions(t *testing.T, db *sql.DB, columnID int64) []struct {
	ID  int64
	Pos int
} {
	t.Helper()
	rows, err := db.QueryContext(context.Background(),
		`SELECT id, position FROM project_cards WHERE column_id = $1 ORDER BY position ASC, id ASC`,
		columnID,
	)
	if err != nil {
		t.Fatalf("read positions: %v", err)
	}
	defer rows.Close()
	var out []struct {
		ID  int64
		Pos int
	}
	for rows.Next() {
		var r struct {
			ID  int64
			Pos int
		}
		if err := rows.Scan(&r.ID, &r.Pos); err != nil {
			t.Fatalf("scan: %v", err)
		}
		out = append(out, r)
	}
	return out
}

// TestProjectStore_MoveCard_SameColumn_Down verifies moving a card to a later
// position within the same column shifts intermediate cards up by one.
func TestProjectStore_MoveCard_SameColumn_Down(t *testing.T) {
	db := openStoreDB(t)
	suffix := testutil.UniqueSuffix(t)
	ownerID := testutil.SeedUser(t, db, suffix)
	repoID := testutil.SeedRepo(t, db, ownerID, "testuser_"+suffix, suffix)
	projectID, cols := seedProjectWithColumns(t, db, repoID, []string{"todo"})

	s := store.NewProjectStore(db)
	a := seedCard(t, s, cols[0], "A")
	b := seedCard(t, s, cols[0], "B")
	c := seedCard(t, s, cols[0], "C")
	d := seedCard(t, s, cols[0], "D")
	// Initial: [A=0, B=1, C=2, D=3]

	// Move A from 0 to 2: expected order [B=0, C=1, A=2, D=3]
	if err := s.MoveCard(context.Background(), projectID, a.ID, cols[0], 2); err != nil {
		t.Fatalf("MoveCard: %v", err)
	}
	got := readPositions(t, db, cols[0])
	want := []struct {
		ID  int64
		Pos int
	}{
		{b.ID, 0}, {c.ID, 1}, {a.ID, 2}, {d.ID, 3},
	}
	for i, w := range want {
		if got[i] != w {
			t.Errorf("row %d: want %+v got %+v", i, w, got[i])
		}
	}
}

// TestProjectStore_MoveCard_SameColumn_Up verifies moving a card to an earlier
// position within the same column shifts intermediate cards down by one.
func TestProjectStore_MoveCard_SameColumn_Up(t *testing.T) {
	db := openStoreDB(t)
	suffix := testutil.UniqueSuffix(t)
	ownerID := testutil.SeedUser(t, db, suffix)
	repoID := testutil.SeedRepo(t, db, ownerID, "testuser_"+suffix, suffix)
	projectID, cols := seedProjectWithColumns(t, db, repoID, []string{"todo"})

	s := store.NewProjectStore(db)
	a := seedCard(t, s, cols[0], "A")
	b := seedCard(t, s, cols[0], "B")
	c := seedCard(t, s, cols[0], "C")
	d := seedCard(t, s, cols[0], "D")
	// Initial: [A=0, B=1, C=2, D=3]

	// Move D from 3 to 1: expected order [A=0, D=1, B=2, C=3]
	if err := s.MoveCard(context.Background(), projectID, d.ID, cols[0], 1); err != nil {
		t.Fatalf("MoveCard: %v", err)
	}
	got := readPositions(t, db, cols[0])
	want := []struct {
		ID  int64
		Pos int
	}{
		{a.ID, 0}, {d.ID, 1}, {b.ID, 2}, {c.ID, 3},
	}
	for i, w := range want {
		if got[i] != w {
			t.Errorf("row %d: want %+v got %+v", i, w, got[i])
		}
	}
}

// TestProjectStore_MoveCard_CrossColumn verifies moving a card between columns
// closes the gap in the source column and opens a slot in the destination.
func TestProjectStore_MoveCard_CrossColumn(t *testing.T) {
	db := openStoreDB(t)
	suffix := testutil.UniqueSuffix(t)
	ownerID := testutil.SeedUser(t, db, suffix)
	repoID := testutil.SeedRepo(t, db, ownerID, "testuser_"+suffix, suffix)
	projectID, cols := seedProjectWithColumns(t, db, repoID, []string{"todo", "doing"})

	s := store.NewProjectStore(db)
	a := seedCard(t, s, cols[0], "A")
	b := seedCard(t, s, cols[0], "B")
	c := seedCard(t, s, cols[0], "C")
	x := seedCard(t, s, cols[1], "X")
	y := seedCard(t, s, cols[1], "Y")
	// todo: [A=0, B=1, C=2]
	// doing: [X=0, Y=1]

	// Move B (todo:1) to doing position 1.
	// Expected todo: [A=0, C=1], doing: [X=0, B=1, Y=2].
	if err := s.MoveCard(context.Background(), projectID, b.ID, cols[1], 1); err != nil {
		t.Fatalf("MoveCard: %v", err)
	}
	gotTodo := readPositions(t, db, cols[0])
	wantTodo := []struct {
		ID  int64
		Pos int
	}{{a.ID, 0}, {c.ID, 1}}
	for i, w := range wantTodo {
		if gotTodo[i] != w {
			t.Errorf("todo row %d: want %+v got %+v", i, w, gotTodo[i])
		}
	}
	gotDoing := readPositions(t, db, cols[1])
	wantDoing := []struct {
		ID  int64
		Pos int
	}{{x.ID, 0}, {b.ID, 1}, {y.ID, 2}}
	for i, w := range wantDoing {
		if gotDoing[i] != w {
			t.Errorf("doing row %d: want %+v got %+v", i, w, gotDoing[i])
		}
	}
}

// TestProjectStore_MoveCard_CrossColumn_AppendToEnd verifies moving a card to
// a destination column at newPosition == len(dest) (the production path: the
// kanban JS always appends to the end of the list).
func TestProjectStore_MoveCard_CrossColumn_AppendToEnd(t *testing.T) {
	db := openStoreDB(t)
	suffix := testutil.UniqueSuffix(t)
	ownerID := testutil.SeedUser(t, db, suffix)
	repoID := testutil.SeedRepo(t, db, ownerID, "testuser_"+suffix, suffix)
	projectID, cols := seedProjectWithColumns(t, db, repoID, []string{"todo", "doing"})

	s := store.NewProjectStore(db)
	c1 := seedCard(t, s, cols[0], "c1")
	c2 := seedCard(t, s, cols[0], "c2")
	c3 := seedCard(t, s, cols[1], "c3")
	c4 := seedCard(t, s, cols[1], "c4")
	// todo: [c1=0, c2=1]
	// doing: [c3=0, c4=1]

	// Move c1 from todo to doing at position 2 (= current length of doing).
	// Expected todo: [c2=0], doing: [c3=0, c4=1, c1=2].
	if err := s.MoveCard(context.Background(), projectID, c1.ID, cols[1], 2); err != nil {
		t.Fatalf("MoveCard: %v", err)
	}
	gotTodo := readPositions(t, db, cols[0])
	wantTodo := []struct {
		ID  int64
		Pos int
	}{{c2.ID, 0}}
	if len(gotTodo) != len(wantTodo) {
		t.Fatalf("todo: want %d rows, got %d (%+v)", len(wantTodo), len(gotTodo), gotTodo)
	}
	for i, w := range wantTodo {
		if gotTodo[i] != w {
			t.Errorf("todo row %d: want %+v got %+v", i, w, gotTodo[i])
		}
	}
	gotDoing := readPositions(t, db, cols[1])
	wantDoing := []struct {
		ID  int64
		Pos int
	}{{c3.ID, 0}, {c4.ID, 1}, {c1.ID, 2}}
	if len(gotDoing) != len(wantDoing) {
		t.Fatalf("doing: want %d rows, got %d (%+v)", len(wantDoing), len(gotDoing), gotDoing)
	}
	for i, w := range wantDoing {
		if gotDoing[i] != w {
			t.Errorf("doing row %d: want %+v got %+v", i, w, gotDoing[i])
		}
	}
}

// TestProjectStore_MoveCard_SamePosition_NoChange verifies moving a card to
// its current position does not corrupt positions.
func TestProjectStore_MoveCard_SamePosition_NoChange(t *testing.T) {
	db := openStoreDB(t)
	suffix := testutil.UniqueSuffix(t)
	ownerID := testutil.SeedUser(t, db, suffix)
	repoID := testutil.SeedRepo(t, db, ownerID, "testuser_"+suffix, suffix)
	projectID, cols := seedProjectWithColumns(t, db, repoID, []string{"todo"})

	s := store.NewProjectStore(db)
	a := seedCard(t, s, cols[0], "A")
	b := seedCard(t, s, cols[0], "B")
	c := seedCard(t, s, cols[0], "C")

	if err := s.MoveCard(context.Background(), projectID, b.ID, cols[0], 1); err != nil {
		t.Fatalf("MoveCard: %v", err)
	}
	got := readPositions(t, db, cols[0])
	want := []struct {
		ID  int64
		Pos int
	}{{a.ID, 0}, {b.ID, 1}, {c.ID, 2}}
	for i, w := range want {
		if got[i] != w {
			t.Errorf("row %d: want %+v got %+v", i, w, got[i])
		}
	}
}

// TestProjectStore_MoveCard_CrossProject_Rejected: attacker presents the
// victim's cardID with their own projectID + column. Source-side IDOR guard.
func TestProjectStore_MoveCard_CrossProject_Rejected(t *testing.T) {
	db := openStoreDB(t)
	suffix := testutil.UniqueSuffix(t)
	ownerID := testutil.SeedUser(t, db, suffix)
	repoID := testutil.SeedRepo(t, db, ownerID, "testuser_"+suffix, suffix)
	_, victimCols := seedProjectWithColumns(t, db, repoID, []string{"victim_todo"})
	attackerID, attackerCols := seedProjectWithColumns(t, db, repoID, []string{"attacker_todo"})

	s := store.NewProjectStore(db)
	victimCard := seedCard(t, s, victimCols[0], "secret")

	err := s.MoveCard(context.Background(), attackerID, victimCard.ID, attackerCols[0], 0)
	if !errors.Is(err, store.ErrCardNotInProject) {
		t.Fatalf("MoveCard: want ErrCardNotInProject, got %v", err)
	}

	got := readPositions(t, db, victimCols[0])
	if len(got) != 1 || got[0].ID != victimCard.ID || got[0].Pos != 0 {
		t.Errorf("victim column tampered: got %+v", got)
	}
}

// TestProjectStore_MoveCard_AttackerCardToVictimColumn_Rejected: attacker
// uses their own cardID but targets a column in another project. Destination
// IDOR guard (defense in depth — service-layer also checks this).
func TestProjectStore_MoveCard_AttackerCardToVictimColumn_Rejected(t *testing.T) {
	db := openStoreDB(t)
	suffix := testutil.UniqueSuffix(t)
	ownerID := testutil.SeedUser(t, db, suffix)
	repoID := testutil.SeedRepo(t, db, ownerID, "testuser_"+suffix, suffix)
	_, victimCols := seedProjectWithColumns(t, db, repoID, []string{"victim_todo"})
	attackerID, attackerCols := seedProjectWithColumns(t, db, repoID, []string{"attacker_todo"})

	s := store.NewProjectStore(db)
	attackerCard := seedCard(t, s, attackerCols[0], "mine")
	_ = seedCard(t, s, victimCols[0], "existing")

	err := s.MoveCard(context.Background(), attackerID, attackerCard.ID, victimCols[0], 0)
	if !errors.Is(err, store.ErrCardNotInProject) {
		t.Fatalf("MoveCard: want ErrCardNotInProject, got %v", err)
	}

	// Victim column must be untouched.
	if got := readPositions(t, db, victimCols[0]); len(got) != 1 || got[0].Pos != 0 {
		t.Errorf("victim column tampered: got %+v", got)
	}
	// Attacker card must remain in its original column.
	if got := readPositions(t, db, attackerCols[0]); len(got) != 1 || got[0].ID != attackerCard.ID {
		t.Errorf("attacker card moved: got %+v", got)
	}
}

func TestProjectStore_MoveCard_PositionOutOfRange_Rejected(t *testing.T) {
	db := openStoreDB(t)
	suffix := testutil.UniqueSuffix(t)
	ownerID := testutil.SeedUser(t, db, suffix)
	repoID := testutil.SeedRepo(t, db, ownerID, "testuser_"+suffix, suffix)
	projectID, cols := seedProjectWithColumns(t, db, repoID, []string{"todo", "doing"})

	s := store.NewProjectStore(db)
	a := seedCard(t, s, cols[0], "A")
	_ = seedCard(t, s, cols[0], "B")
	_ = seedCard(t, s, cols[1], "X")

	if err := s.MoveCard(context.Background(), projectID, a.ID, cols[1], 999); !errors.Is(err, store.ErrInvalidPosition) {
		t.Fatalf("MoveCard cross-column: want ErrInvalidPosition, got %v", err)
	}
	if err := s.MoveCard(context.Background(), projectID, a.ID, cols[0], 999); !errors.Is(err, store.ErrInvalidPosition) {
		t.Fatalf("MoveCard same-column: want ErrInvalidPosition, got %v", err)
	}
}

// seedNamedProject inserts a project with a specific name and returns its ID.
func seedNamedProject(t *testing.T, db *sql.DB, repoID int64, name string) int64 {
	t.Helper()
	var id int64
	if err := db.QueryRowContext(context.Background(),
		`INSERT INTO projects (repo_id, name, description) VALUES ($1, $2, '') RETURNING id`,
		repoID, name,
	).Scan(&id); err != nil {
		t.Fatalf("seed project %q: %v", name, err)
	}
	t.Cleanup(func() { db.ExecContext(context.Background(), `DELETE FROM projects WHERE id = $1`, id) })
	return id
}

// seedIssueRow inserts an issue and returns its ID.
func seedIssueRow(t *testing.T, db *sql.DB, repoID, authorID int64, number int, state string) int64 {
	t.Helper()
	var id int64
	if err := db.QueryRowContext(context.Background(),
		`INSERT INTO issues (repo_id, number, author_id, title, state) VALUES ($1, $2, $3, 'issue', $4) RETURNING id`,
		repoID, number, authorID, state,
	).Scan(&id); err != nil {
		t.Fatalf("seed issue: %v", err)
	}
	t.Cleanup(func() { db.ExecContext(context.Background(), `DELETE FROM issues WHERE id = $1`, id) })
	return id
}

// seedPullRow inserts a pull request and returns its ID.
func seedPullRow(t *testing.T, db *sql.DB, repoID, authorID int64, number int, state string) int64 {
	t.Helper()
	var id int64
	if err := db.QueryRowContext(context.Background(),
		`INSERT INTO pull_requests (repo_id, number, author_id, title, state, head_branch)
		 VALUES ($1, $2, $3, 'pr', $4, 'feature') RETURNING id`,
		repoID, number, authorID, state,
	).Scan(&id); err != nil {
		t.Fatalf("seed pull: %v", err)
	}
	t.Cleanup(func() { db.ExecContext(context.Background(), `DELETE FROM pull_requests WHERE id = $1`, id) })
	return id
}

// seedLinkedCard inserts a card linked to an issue or PR (one of issueID/pullID set).
func seedLinkedCard(t *testing.T, s *store.ProjectStore, columnID int64, issueID, pullID *int64) {
	t.Helper()
	if err := s.CreateCard(context.Background(), &model.ProjectCard{ColumnID: columnID, IssueID: issueID, PullID: pullID}); err != nil {
		t.Fatalf("seed linked card: %v", err)
	}
}

// TestProjectStore_ListByRepoWithStats_Counts verifies the aggregate counts:
// every card counts toward CardCount, only issue/PR cards toward LinkedCount,
// and only resolved (closed issue / merged or closed PR) ones toward DoneCount.
func TestProjectStore_ListByRepoWithStats_Counts(t *testing.T) {
	db := openStoreDB(t)
	suffix := testutil.UniqueSuffix(t)
	ownerID := testutil.SeedUser(t, db, suffix)
	repoID := testutil.SeedRepo(t, db, ownerID, "testuser_"+suffix, suffix)
	projectID, cols := seedProjectWithColumns(t, db, repoID, []string{"todo", "done"})

	s := store.NewProjectStore(db)
	seedCard(t, s, cols[0], "a free-form note")
	openIssue := seedIssueRow(t, db, repoID, ownerID, 1, "open")
	closedIssue := seedIssueRow(t, db, repoID, ownerID, 2, "closed")
	mergedPull := seedPullRow(t, db, repoID, ownerID, 3, "merged")
	closedPull := seedPullRow(t, db, repoID, ownerID, 4, "closed")
	seedLinkedCard(t, s, cols[0], &openIssue, nil)
	seedLinkedCard(t, s, cols[1], &closedIssue, nil)
	seedLinkedCard(t, s, cols[1], nil, &mergedPull)
	seedLinkedCard(t, s, cols[1], nil, &closedPull)

	rows, err := s.ListByRepoWithStats(context.Background(), repoID, "", "")
	if err != nil {
		t.Fatalf("ListByRepoWithStats: %v", err)
	}
	var got *store.ProjectWithCounts
	for i := range rows {
		if rows[i].ID == projectID {
			got = &rows[i]
		}
	}
	if got == nil {
		t.Fatalf("project %d not in results", projectID)
	}
	if got.CardCount != 5 {
		t.Errorf("CardCount = %d, want 5", got.CardCount)
	}
	if got.LinkedCount != 4 {
		t.Errorf("LinkedCount = %d, want 4", got.LinkedCount)
	}
	if got.DoneCount != 3 {
		t.Errorf("DoneCount = %d, want 3 (closed issue + merged PR + closed PR)", got.DoneCount)
	}
}

// TestProjectStore_SetProjectClosed_AndStatusFilter verifies close/reopen and
// that the status filter and CountByStatus track closed_at.
func TestProjectStore_SetProjectClosed_AndStatusFilter(t *testing.T) {
	db := openStoreDB(t)
	suffix := testutil.UniqueSuffix(t)
	ownerID := testutil.SeedUser(t, db, suffix)
	repoID := testutil.SeedRepo(t, db, ownerID, "testuser_"+suffix, suffix)
	projectID, _ := seedProjectWithColumns(t, db, repoID, []string{"todo"})

	s := store.NewProjectStore(db)
	ctx := context.Background()

	hasProject := func(status string) bool {
		rows, err := s.ListByRepoWithStats(ctx, repoID, "", status)
		if err != nil {
			t.Fatalf("ListByRepoWithStats(%q): %v", status, err)
		}
		for _, r := range rows {
			if r.ID == projectID {
				return true
			}
		}
		return false
	}

	if !hasProject("open") {
		t.Error("new project should appear under the open filter")
	}
	if hasProject("closed") {
		t.Error("new project should not appear under the closed filter")
	}

	if err := s.SetProjectClosed(ctx, projectID, true); err != nil {
		t.Fatalf("SetProjectClosed(true): %v", err)
	}
	if hasProject("open") {
		t.Error("closed project should not appear under the open filter")
	}
	if !hasProject("closed") {
		t.Error("closed project should appear under the closed filter")
	}

	open, closed, err := s.CountByStatus(ctx, repoID, "")
	if err != nil {
		t.Fatalf("CountByStatus: %v", err)
	}
	if open != 0 || closed != 1 {
		t.Errorf("CountByStatus = (open %d, closed %d), want (0, 1)", open, closed)
	}

	if err := s.SetProjectClosed(ctx, projectID, false); err != nil {
		t.Fatalf("SetProjectClosed(false): %v", err)
	}
	p, err := s.GetProject(ctx, projectID)
	if err != nil {
		t.Fatalf("GetProject: %v", err)
	}
	if p.ClosedAt != nil {
		t.Errorf("reopened project ClosedAt = %v, want nil", p.ClosedAt)
	}
}

// TestProjectStore_ListByRepoWithStats_SearchFilter verifies the name filter is
// a case-insensitive substring match.
func TestProjectStore_ListByRepoWithStats_SearchFilter(t *testing.T) {
	db := openStoreDB(t)
	suffix := testutil.UniqueSuffix(t)
	ownerID := testutil.SeedUser(t, db, suffix)
	repoID := testutil.SeedRepo(t, db, ownerID, "testuser_"+suffix, suffix)

	seedNamedProject(t, db, repoID, "Alpha Board")
	seedNamedProject(t, db, repoID, "Beta Board")

	s := store.NewProjectStore(db)
	ctx := context.Background()

	names := func(query string) []string {
		rows, err := s.ListByRepoWithStats(ctx, repoID, query, "")
		if err != nil {
			t.Fatalf("ListByRepoWithStats(%q): %v", query, err)
		}
		var out []string
		for _, r := range rows {
			out = append(out, r.Name)
		}
		return out
	}

	if got := names("alpha"); len(got) != 1 || got[0] != "Alpha Board" {
		t.Errorf("search %q = %v, want [Alpha Board]", "alpha", got)
	}
	if got := names("BOARD"); len(got) != 2 {
		t.Errorf("case-insensitive search %q = %v, want 2 results", "BOARD", got)
	}
	if got := names("nomatch"); len(got) != 0 {
		t.Errorf("search %q = %v, want no results", "nomatch", got)
	}
}
