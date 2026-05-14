package store_test

// Integration tests for ProjectStore.MoveCard reorder SQL.
// All tests require TEST_DATABASE_DSN and skip otherwise.

import (
	"context"
	"database/sql"
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
	_, cols := seedProjectWithColumns(t, db, repoID, []string{"todo"})

	s := store.NewProjectStore(db)
	a := seedCard(t, s, cols[0], "A")
	b := seedCard(t, s, cols[0], "B")
	c := seedCard(t, s, cols[0], "C")
	d := seedCard(t, s, cols[0], "D")
	// Initial: [A=0, B=1, C=2, D=3]

	// Move A from 0 to 2: expected order [B=0, C=1, A=2, D=3]
	if err := s.MoveCard(context.Background(), a.ID, cols[0], 2); err != nil {
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
	_, cols := seedProjectWithColumns(t, db, repoID, []string{"todo"})

	s := store.NewProjectStore(db)
	a := seedCard(t, s, cols[0], "A")
	b := seedCard(t, s, cols[0], "B")
	c := seedCard(t, s, cols[0], "C")
	d := seedCard(t, s, cols[0], "D")
	// Initial: [A=0, B=1, C=2, D=3]

	// Move D from 3 to 1: expected order [A=0, D=1, B=2, C=3]
	if err := s.MoveCard(context.Background(), d.ID, cols[0], 1); err != nil {
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
	_, cols := seedProjectWithColumns(t, db, repoID, []string{"todo", "doing"})

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
	if err := s.MoveCard(context.Background(), b.ID, cols[1], 1); err != nil {
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

// TestProjectStore_MoveCard_SamePosition_NoChange verifies moving a card to
// its current position does not corrupt positions.
func TestProjectStore_MoveCard_SamePosition_NoChange(t *testing.T) {
	db := openStoreDB(t)
	suffix := testutil.UniqueSuffix(t)
	ownerID := testutil.SeedUser(t, db, suffix)
	repoID := testutil.SeedRepo(t, db, ownerID, "testuser_"+suffix, suffix)
	_, cols := seedProjectWithColumns(t, db, repoID, []string{"todo"})

	s := store.NewProjectStore(db)
	a := seedCard(t, s, cols[0], "A")
	b := seedCard(t, s, cols[0], "B")
	c := seedCard(t, s, cols[0], "C")

	if err := s.MoveCard(context.Background(), b.ID, cols[0], 1); err != nil {
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
