package testutil

import (
	"context"
	"database/sql"
	"fmt"
	"os"
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
	t.Cleanup(func() {
		db.ExecContext(context.Background(), `DELETE FROM users WHERE id = $1`, id)
	})
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
	t.Cleanup(func() {
		db.ExecContext(context.Background(), `DELETE FROM users WHERE id = $1`, id)
	})
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
	t.Cleanup(func() {
		db.ExecContext(context.Background(), `DELETE FROM users WHERE id = $1`, id)
	})
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
		db.ExecContext(context.Background(), `DELETE FROM permissions WHERE repo_id = $1`, repoID)
		db.ExecContext(context.Background(), `DELETE FROM repositories WHERE id = $1`, repoID)
	})
	return repoID
}

// UniqueSuffix returns a unique string suitable for use in test data names,
// derived from the test name and process ID to avoid collisions across parallel runs.
func UniqueSuffix(t *testing.T) string {
	t.Helper()
	return fmt.Sprintf("%d_%d", os.Getpid(), suffixCounter.Add(1))
}
