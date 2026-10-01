package db_test

import (
	"os"
	"testing"

	"github.com/mkappworks-dev/cloudzilla-app/internal/testutil"
)

const patScopeBackfillMigration = "migrations/090_backfill_access_token_scopes.sql"

func TestPATScopeBackfill_GivesTokensWithoutAKnownScopeEveryScope(t *testing.T) {
	db := testutil.OpenFreshTestDB(t)
	var userID int64
	if err := db.QueryRow(`INSERT INTO users (username, email, password_hash) VALUES ('tok', 'tok@test.invalid', 'x') RETURNING id`).Scan(&userID); err != nil {
		t.Fatalf("seed user: %v", err)
	}
	const every = "repo:read,repo:write,issues:write,pulls:write"
	want := map[string]string{ // stored scopes -> scopes after the migration
		"":                   every,
		"admin":              every,
		"repo:read":          "repo:read",
		"admin,issues:write": "admin,issues:write",
	}
	for scopes := range want {
		testutil.Exec(t, db,
			`INSERT INTO access_tokens (user_id, name, token_hash, last_eight, scopes) VALUES ($1, $2, $2, 'x', $2)`,
			userID, scopes)
	}
	sql, err := os.ReadFile(patScopeBackfillMigration)
	if err != nil {
		t.Fatalf("read migration: %v", err)
	}

	for run := 1; run <= 2; run++ {
		if _, err := db.Exec(string(sql)); err != nil {
			t.Fatalf("run %d: %v", run, err)
		}
		for before, after := range want {
			var got string
			if err := db.QueryRow(`SELECT scopes FROM access_tokens WHERE token_hash = $1`, before).Scan(&got); err != nil {
				t.Fatalf("read token %q: %v", before, err)
			}
			if got != after {
				t.Errorf("run %d: token stored with %q has scopes %q, want %q", run, before, got, after)
			}
		}
	}
}
