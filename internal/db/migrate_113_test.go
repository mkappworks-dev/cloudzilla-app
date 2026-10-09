package db_test

import (
	"database/sql"
	"os"
	"testing"

	"github.com/mkappworks-dev/cloudzilla-app/internal/testutil"
)

const dropCrossRepoCardsMigration = "migrations/113_drop_cross_repo_project_cards.sql"

func TestDropCrossRepoCardsMigration_DeletesOnlyCardsPointingAtAnotherRepo(t *testing.T) {
	db := testutil.OpenFreshTestDB(t)
	alice := seedNamedUser(t, db, "alice")
	var home, away, project, column int64
	for name, id := range map[string]*int64{"home": &home, "away": &away} {
		if err := db.QueryRow(`INSERT INTO repositories (owner_id, owner_name, name) VALUES ($1, 'alice', $2) RETURNING id`, alice, name).Scan(id); err != nil {
			t.Fatalf("seed repo %s: %v", name, err)
		}
	}
	if err := db.QueryRow(`INSERT INTO projects (repo_id, name) VALUES ($1, 'Board') RETURNING id`, home).Scan(&project); err != nil {
		t.Fatalf("seed project: %v", err)
	}
	if err := db.QueryRow(`INSERT INTO project_columns (project_id, name) VALUES ($1, 'Todo') RETURNING id`, project).Scan(&column); err != nil {
		t.Fatalf("seed column: %v", err)
	}

	issueIn := func(repo int64, number int) int64 {
		var id int64
		if err := db.QueryRow(`INSERT INTO issues (repo_id, number, author_id, title) VALUES ($1, $2, $3, 'i') RETURNING id`, repo, number, alice).Scan(&id); err != nil {
			t.Fatalf("seed issue: %v", err)
		}
		return id
	}
	pullIn := func(repo int64, number int) int64 {
		var id int64
		if err := db.QueryRow(`INSERT INTO pull_requests (repo_id, number, author_id, title, head_branch) VALUES ($1, $2, $3, 'p', 'x') RETURNING id`, repo, number, alice).Scan(&id); err != nil {
			t.Fatalf("seed pull: %v", err)
		}
		return id
	}
	card := func(name string, issue, pull any, title string) int64 {
		var id int64
		if err := db.QueryRow(`INSERT INTO project_cards (column_id, issue_id, pull_id, title) VALUES ($1, $2, $3, $4) RETURNING id`, column, issue, pull, title).Scan(&id); err != nil {
			t.Fatalf("seed card %s: %v", name, err)
		}
		return id
	}

	keep := map[string]int64{
		"own issue": card("own issue", issueIn(home, 1), nil, ""),
		"own pull":  card("own pull", nil, pullIn(home, 2), ""),
		"note":      card("note", nil, nil, "remember"),
	}
	drop := map[string]int64{
		"foreign issue": card("foreign issue", issueIn(away, 1), nil, ""),
		"foreign pull":  card("foreign pull", nil, pullIn(away, 2), ""),
	}

	migration, err := os.ReadFile(dropCrossRepoCardsMigration)
	if err != nil {
		t.Fatalf("read migration: %v", err)
	}
	for run := 1; run <= 2; run++ {
		if _, err := db.Exec(string(migration)); err != nil {
			t.Fatalf("run %d: %v", run, err)
		}
		for name, id := range keep {
			if !cardExists(t, db, id) {
				t.Errorf("run %d: %s card was deleted", run, name)
			}
		}
		for name, id := range drop {
			if cardExists(t, db, id) {
				t.Errorf("run %d: %s card survived", run, name)
			}
		}
	}
}

func cardExists(t *testing.T, db *sql.DB, id int64) bool {
	t.Helper()
	var ok bool
	if err := db.QueryRow(`SELECT EXISTS (SELECT 1 FROM project_cards WHERE id = $1)`, id).Scan(&ok); err != nil {
		t.Fatalf("look up card %d: %v", id, err)
	}
	return ok
}
