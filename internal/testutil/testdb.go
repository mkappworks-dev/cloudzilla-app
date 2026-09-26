package testutil

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"sync"
	"testing"

	_ "github.com/jackc/pgx/v5/stdlib" // registers "pgx" driver
	"golang.org/x/crypto/bcrypt"
)

// OpenTestDB opens a connection to the test database using TEST_DATABASE_DSN.
// The test is skipped if the env var is not set. The returned DB is closed via t.Cleanup.
func OpenTestDB(t *testing.T) *sql.DB {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_DSN")
	if dsn == "" {
		t.Skip("TEST_DATABASE_DSN not set; skipping integration test")
	}
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open test db: %v", err)
	}
	if err := db.Ping(); err != nil {
		t.Fatalf("ping test db: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

// Exec runs a statement against the test database, failing the test if it errors.
func Exec(t *testing.T, db *sql.DB, query string, args ...any) {
	t.Helper()
	if _, err := db.ExecContext(context.Background(), query, args...); err != nil {
		t.Errorf("exec %q: %v", query, err)
	}
}

// DeleteUsers deletes users along with the repositories they own. The repositories
// go first, in their own statement: Postgres runs the NO ACTION checks on issue,
// pull and comment authors before the owner cascade would have removed those rows.
func DeleteUsers(t *testing.T, db *sql.DB, ids ...int64) {
	t.Helper()
	Exec(t, db, `DELETE FROM repositories WHERE owner_id = ANY($1)`, ids)
	Exec(t, db, `DELETE FROM users WHERE id = ANY($1)`, ids)
}

var seededUsers sync.Map // *testing.T -> *userSweep

type userSweep struct {
	mu  sync.Mutex
	ids []int64
}

// deleteLast deletes a seeded user after t's other cleanups: users often author
// rows in repositories seeded after them, which have to go first. Only the first
// user seeded in t registers a cleanup, and cleanups run last-in first-out.
func deleteLast(t *testing.T, db *sql.DB, id int64) {
	v, loaded := seededUsers.LoadOrStore(t, &userSweep{})
	sweep := v.(*userSweep)
	sweep.mu.Lock()
	sweep.ids = append(sweep.ids, id)
	sweep.mu.Unlock()
	if loaded {
		return
	}
	t.Cleanup(func() {
		seededUsers.Delete(t)
		sweep.mu.Lock()
		defer sweep.mu.Unlock()
		DeleteUsers(t, db, sweep.ids...)
	})
}

// SeedUser inserts a test user with the given suffix and returns the user's ID.
// The user is deleted automatically when the test ends.
func SeedUser(t *testing.T, db *sql.DB, suffix string) int64 {
	t.Helper()
	ctx := context.Background()
	var id int64
	err := db.QueryRowContext(ctx,
		`INSERT INTO users (username, email, password_hash, is_superadmin)
		 VALUES ($1, $2, 'x', false) RETURNING id`,
		"testuser_"+suffix,
		"testuser_"+suffix+"@test.invalid",
	).Scan(&id)
	if err != nil {
		t.Fatalf("SeedUser: %v", err)
	}
	deleteLast(t, db, id)
	return id
}

// SeedUserWithPassword inserts a test user with the given plaintext password and
// returns the user's ID and email. The user is deleted when the test ends.
func SeedUserWithPassword(t *testing.T, db *sql.DB, suffix, password string) (id int64, email string) {
	t.Helper()
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.MinCost)
	if err != nil {
		t.Fatalf("SeedUserWithPassword hash: %v", err)
	}
	email = "testpw_" + suffix + "@test.invalid"
	ctx := context.Background()
	err = db.QueryRowContext(ctx,
		`INSERT INTO users (username, email, password_hash, is_superadmin, is_invited)
		 VALUES ($1, $2, $3, false, true) RETURNING id`,
		"testpw_"+suffix, email, string(hash),
	).Scan(&id)
	if err != nil {
		t.Fatalf("SeedUserWithPassword: %v", err)
	}
	deleteLast(t, db, id)
	return id, email
}

// SeedSuperadmin inserts a test superadmin user and returns the user's ID.
func SeedSuperadmin(t *testing.T, db *sql.DB, suffix string) int64 {
	t.Helper()
	ctx := context.Background()
	var id int64
	err := db.QueryRowContext(ctx,
		`INSERT INTO users (username, email, password_hash, is_superadmin)
		 VALUES ($1, $2, 'x', true) RETURNING id`,
		"testadmin_"+suffix,
		"testadmin_"+suffix+"@test.invalid",
	).Scan(&id)
	if err != nil {
		t.Fatalf("SeedSuperadmin: %v", err)
	}
	deleteLast(t, db, id)
	return id
}

// SeedRepo inserts a test repository owned by ownerID and returns the repo ID.
// An owner-role permission row is inserted automatically.
// Both are deleted when the test ends.
func SeedRepo(t *testing.T, db *sql.DB, ownerID int64, ownerName, suffix string) int64 {
	t.Helper()
	ctx := context.Background()
	var repoID int64
	err := db.QueryRowContext(ctx,
		`INSERT INTO repositories (owner_id, owner_name, name, description, private)
		 VALUES ($1, $2, $3, '', false) RETURNING id`,
		ownerID, ownerName, "testrepo_"+suffix,
	).Scan(&repoID)
	if err != nil {
		t.Fatalf("SeedRepo: %v", err)
	}
	_, err = db.ExecContext(ctx,
		`INSERT INTO permissions (user_id, repo_id, role) VALUES ($1, $2, 'owner')`,
		ownerID, repoID,
	)
	if err != nil {
		t.Fatalf("SeedRepo permission: %v", err)
	}
	t.Cleanup(func() {
		Exec(t, db, `DELETE FROM permissions WHERE repo_id = $1`, repoID)
		Exec(t, db, `DELETE FROM repositories WHERE id = $1`, repoID)
	})
	return repoID
}

// The counter keeps names unique across tests and -count iterations within one
// process; the PID separates the concurrent per-package test processes.
func UniqueSuffix(t *testing.T) string {
	t.Helper()
	return fmt.Sprintf("%d_%d", os.Getpid(), suffixCounter.Add(1))
}
