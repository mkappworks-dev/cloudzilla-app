package db_test

import (
	"os"
	"strings"
	"testing"

	"github.com/mkappworks-dev/cloudzilla-app/internal/testutil"
)

func TestProjectCardDetailsMigration_SplitsNotesIntoTitleAndDescription(t *testing.T) {
	db := testutil.OpenFreshTestDB(t)
	testutil.Exec(t, db, `DROP TABLE card_assignees, card_labels`)
	testutil.Exec(t, db, `ALTER TABLE project_cards DROP CONSTRAINT project_cards_shape, DROP COLUMN title, DROP COLUMN due_date`)
	testutil.Exec(t, db, `ALTER TABLE project_cards ADD CONSTRAINT project_cards_check CHECK (
		(issue_id IS NOT NULL AND pull_id IS NULL AND note = '')
		OR (pull_id IS NOT NULL AND issue_id IS NULL AND note = '')
		OR (issue_id IS NULL AND pull_id IS NULL AND note <> ''))`)

	owner := seedNamedUser(t, db, "alice")
	var repoID, projectID, colID int64
	if err := db.QueryRow(`INSERT INTO repositories (owner_id, owner_name, name) VALUES ($1, 'alice', 'mine') RETURNING id`, owner).Scan(&repoID); err != nil {
		t.Fatalf("seed repo: %v", err)
	}
	if err := db.QueryRow(`INSERT INTO projects (repo_id, name, description) VALUES ($1, 'p', '') RETURNING id`, repoID).Scan(&projectID); err != nil {
		t.Fatalf("seed project: %v", err)
	}
	if err := db.QueryRow(`INSERT INTO project_columns (project_id, name, position) VALUES ($1, 'c', 0) RETURNING id`, projectID).Scan(&colID); err != nil {
		t.Fatalf("seed column: %v", err)
	}

	var awayRepoID, ownIssueID, awayIssueID int64
	if err := db.QueryRow(`INSERT INTO repositories (owner_id, owner_name, name) VALUES ($1, 'alice', 'away') RETURNING id`, owner).Scan(&awayRepoID); err != nil {
		t.Fatalf("seed away repo: %v", err)
	}
	for repo, id := range map[int64]*int64{repoID: &ownIssueID, awayRepoID: &awayIssueID} {
		if err := db.QueryRow(`INSERT INTO issues (repo_id, number, author_id, title) VALUES ($1, 1, $2, 'i') RETURNING id`, repo, owner).Scan(id); err != nil {
			t.Fatalf("seed issue: %v", err)
		}
	}

	long := strings.Repeat("x", 130)
	notes := []string{"one line", "head\nbody line 1\nbody line 2", "\n\n  padded\nrest", long + "\nmore"}
	for _, n := range notes {
		testutil.Exec(t, db, `INSERT INTO project_cards (column_id, note) VALUES ($1, $2)`, colID, n)
	}
	testutil.Exec(t, db, `INSERT INTO project_cards (column_id, issue_id) VALUES ($1, $2), ($1, $3)`, colID, ownIssueID, awayIssueID)

	for _, file := range []string{"migrations/113_drop_cross_repo_project_cards.sql", "migrations/114_project_card_details.sql"} {
		migration, err := os.ReadFile(file)
		if err != nil {
			t.Fatalf("read migration: %v", err)
		}
		if _, err := db.Exec(string(migration)); err != nil {
			t.Fatalf("migrate %s: %v", file, err)
		}
	}

	want := [][2]string{
		{"one line", ""},
		{"head", "body line 1\nbody line 2"},
		{"padded", "rest"},
		{long[:120], long + "\nmore"},
		{"", ""},
	}
	rows, err := db.Query(`SELECT title, note FROM project_cards WHERE column_id = $1 ORDER BY id`, colID)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	i := 0
	for rows.Next() {
		var title, note string
		if err := rows.Scan(&title, &note); err != nil {
			t.Fatal(err)
		}
		if i >= len(want) || title != want[i][0] || note != want[i][1] {
			t.Errorf("card %d = (%q, %q), want %q", i, title, note, want[i])
		}
		i++
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if i != len(want) {
		t.Errorf("got %d cards, want %d", i, len(want))
	}
}
