package store_test

// Integration tests for UserStore. All tests require TEST_DATABASE_DSN and skip otherwise.

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"testing"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/mkappworks/cloudzilla/internal/model"
	"github.com/mkappworks/cloudzilla/internal/store"
	"github.com/mkappworks/cloudzilla/internal/testutil"
)

// openStoreDB opens the test database and registers a cleanup to close it.
// Skips the test if TEST_DATABASE_DSN is not set.
func openStoreDB(t *testing.T) *sql.DB {
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

// TestUserStore_Create_AssignsID verifies that Create inserts a user and populates
// the ID, CreatedAt, and UpdatedAt fields from the database RETURNING clause.
func TestUserStore_Create_AssignsID(t *testing.T) {
	db := openStoreDB(t)
	s := store.NewUserStore(db)
	suffix := fmt.Sprintf("%d_crt", os.Getpid())

	u := &model.User{
		Username:     "storeuser_" + suffix,
		Email:        "storeuser_" + suffix + "@test.invalid",
		PasswordHash: "x",
	}
	if err := s.Create(context.Background(), u); err != nil {
		t.Fatalf("Create: %v", err)
	}
	t.Cleanup(func() {
		db.ExecContext(context.Background(), `DELETE FROM users WHERE id = $1`, u.ID)
	})

	if u.ID == 0 {
		t.Error("Create must populate a non-zero ID")
	}
	if u.CreatedAt.IsZero() {
		t.Error("Create must populate CreatedAt")
	}
}

// TestUserStore_GetByID_ReturnsCorrectUser verifies that GetByID retrieves the correct
// user record given a known ID.
func TestUserStore_GetByID_ReturnsCorrectUser(t *testing.T) {
	db := openStoreDB(t)
	suffix := testutil.UniqueSuffix(t)
	id := testutil.SeedUser(t, db, suffix)

	s := store.NewUserStore(db)
	u, err := s.GetByID(context.Background(), id)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if u.ID != id {
		t.Errorf("want ID %d, got %d", id, u.ID)
	}
	if u.Username != "testuser_"+suffix {
		t.Errorf("want username %q, got %q", "testuser_"+suffix, u.Username)
	}
}

// TestUserStore_GetByID_UnknownID_Error verifies that GetByID returns an error
// for an ID that does not exist in the database.
func TestUserStore_GetByID_UnknownID_Error(t *testing.T) {
	db := openStoreDB(t)
	s := store.NewUserStore(db)
	_, err := s.GetByID(context.Background(), -999)
	if err == nil {
		t.Error("GetByID with unknown ID must return an error")
	}
}

// TestUserStore_GetByUsername_ReturnsCorrectUser verifies that GetByUsername finds
// a user by their exact username.
func TestUserStore_GetByUsername_ReturnsCorrectUser(t *testing.T) {
	db := openStoreDB(t)
	suffix := testutil.UniqueSuffix(t)
	id := testutil.SeedUser(t, db, suffix)

	s := store.NewUserStore(db)
	u, err := s.GetByUsername(context.Background(), "testuser_"+suffix)
	if err != nil {
		t.Fatalf("GetByUsername: %v", err)
	}
	if u.ID != id {
		t.Errorf("want ID %d, got %d", id, u.ID)
	}
}

// TestUserStore_GetByUsername_Unknown_Error verifies that GetByUsername returns an error
// when no user with that username exists.
func TestUserStore_GetByUsername_Unknown_Error(t *testing.T) {
	db := openStoreDB(t)
	s := store.NewUserStore(db)
	_, err := s.GetByUsername(context.Background(), "doesnotexist_xyz_999")
	if err == nil {
		t.Error("GetByUsername with unknown username must return an error")
	}
}

// TestUserStore_GetByEmail_ReturnsCorrectUser verifies that GetByEmail finds a user
// by their email address.
func TestUserStore_GetByEmail_ReturnsCorrectUser(t *testing.T) {
	db := openStoreDB(t)
	suffix := testutil.UniqueSuffix(t)
	id := testutil.SeedUser(t, db, suffix)

	s := store.NewUserStore(db)
	u, err := s.GetByEmail(context.Background(), "testuser_"+suffix+"@test.invalid")
	if err != nil {
		t.Fatalf("GetByEmail: %v", err)
	}
	if u.ID != id {
		t.Errorf("want ID %d, got %d", id, u.ID)
	}
}

// TestUserStore_GetByEmail_Unknown_Error verifies that GetByEmail returns an error
// for an email address not in the database.
func TestUserStore_GetByEmail_Unknown_Error(t *testing.T) {
	db := openStoreDB(t)
	s := store.NewUserStore(db)
	_, err := s.GetByEmail(context.Background(), "nobody@test.invalid")
	if err == nil {
		t.Error("GetByEmail with unknown email must return an error")
	}
}

// TestUserStore_Create_DuplicateUsername_Error verifies that inserting two users with
// the same username violates the unique constraint and returns an error.
func TestUserStore_Create_DuplicateUsername_Error(t *testing.T) {
	db := openStoreDB(t)
	s := store.NewUserStore(db)
	suffix := fmt.Sprintf("%d_dup", os.Getpid())

	u1 := &model.User{
		Username:     "dupuser_" + suffix,
		Email:        "dup1_" + suffix + "@test.invalid",
		PasswordHash: "x",
	}
	if err := s.Create(context.Background(), u1); err != nil {
		t.Fatalf("first Create: %v", err)
	}
	t.Cleanup(func() {
		db.ExecContext(context.Background(), `DELETE FROM users WHERE id = $1`, u1.ID)
	})

	u2 := &model.User{
		Username:     "dupuser_" + suffix, // same username
		Email:        "dup2_" + suffix + "@test.invalid",
		PasswordHash: "x",
	}
	if err := s.Create(context.Background(), u2); err == nil {
		t.Error("Create with duplicate username must return an error")
		db.ExecContext(context.Background(), `DELETE FROM users WHERE id = $1`, u2.ID)
	}
}
