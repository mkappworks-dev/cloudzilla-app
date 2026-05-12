package service_test

// Integration tests for UserService. All tests require TEST_DATABASE_DSN and skip otherwise.

import (
	"context"
	"strings"
	"testing"

	"golang.org/x/crypto/bcrypt"

	"github.com/mkappworks-dev/cloudzilla-app/internal/config"
	"github.com/mkappworks-dev/cloudzilla-app/internal/service"
	"github.com/mkappworks-dev/cloudzilla-app/internal/store"
	"github.com/mkappworks-dev/cloudzilla-app/internal/testutil"
)

// newUserSvc builds a UserService backed by the test database.
func newUserSvc(t *testing.T) *service.UserService {
	t.Helper()
	db := testutil.OpenTestDB(t)
	return service.NewUserService(store.NewUserStore(db), config.AuthConfig{
		JWTSecret: "test-secret-32bytes-minimum-len!",
		JWTExpiry: 0, // zero → token never expires for tests
	})
}

// TestUserService_Create_HashesPassword verifies that Create stores a bcrypt hash of
// the password rather than the plaintext, so credentials cannot be recovered from the DB.
func TestUserService_Create_HashesPassword(t *testing.T) {
	db := testutil.OpenTestDB(t)
	suffix := testutil.UniqueSuffix(t)
	svc := service.NewUserService(store.NewUserStore(db), config.AuthConfig{JWTSecret: "test-secret-32bytes-minimum-len!"})

	const plaintext = "supersecretpassword"
	u, err := svc.Create(context.Background(), "testuser_"+suffix, "test_"+suffix+"@example.com", plaintext)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	// Stored hash must differ from the plaintext.
	if u.PasswordHash == plaintext {
		t.Error("Create must not store plaintext password")
	}
	// Stored hash must be a valid bcrypt hash for the plaintext.
	if err := bcrypt.CompareHashAndPassword([]byte(u.PasswordHash), []byte(plaintext)); err != nil {
		t.Errorf("stored hash does not match password: %v", err)
	}
}

// TestUserService_Create_AssignsID verifies that Create returns a user with a non-zero ID,
// confirming the row was inserted and the database-assigned ID was returned.
func TestUserService_Create_AssignsID(t *testing.T) {
	db := testutil.OpenTestDB(t)
	suffix := testutil.UniqueSuffix(t)
	svc := service.NewUserService(store.NewUserStore(db), config.AuthConfig{JWTSecret: "test-secret-32bytes-minimum-len!"})

	u, err := svc.Create(context.Background(), "testuser_"+suffix, "testid_"+suffix+"@example.com", "pass")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if u.ID == 0 {
		t.Error("Create must return a user with non-zero ID")
	}
}

// TestUserService_Authenticate_CorrectPassword_ReturnsToken verifies that Authenticate
// returns a non-empty JWT and the correct user when given valid credentials.
func TestUserService_Authenticate_CorrectPassword_ReturnsToken(t *testing.T) {
	db := testutil.OpenTestDB(t)
	suffix := testutil.UniqueSuffix(t)
	_, email := testutil.SeedUserWithPassword(t, db, suffix, "correctpassword")
	svc := service.NewUserService(store.NewUserStore(db), config.AuthConfig{JWTSecret: "test-secret-32bytes-minimum-len!"})

	u, token, err := svc.Authenticate(context.Background(), email, "correctpassword")
	if err != nil {
		t.Fatalf("Authenticate: %v", err)
	}
	if u == nil {
		t.Fatal("Authenticate must return a non-nil user")
	}
	if token == "" {
		t.Error("Authenticate must return a non-empty JWT token")
	}
}

// TestUserService_Authenticate_WrongPassword_ReturnsGenericError verifies that a wrong
// password returns "invalid credentials" without leaking whether the email exists.
// This prevents user enumeration through differing error messages.
func TestUserService_Authenticate_WrongPassword_ReturnsGenericError(t *testing.T) {
	db := testutil.OpenTestDB(t)
	suffix := testutil.UniqueSuffix(t)
	_, email := testutil.SeedUserWithPassword(t, db, suffix, "correctpassword")
	svc := service.NewUserService(store.NewUserStore(db), config.AuthConfig{JWTSecret: "test-secret-32bytes-minimum-len!"})

	_, _, err := svc.Authenticate(context.Background(), email, "wrongpassword")
	if err == nil {
		t.Fatal("Authenticate must fail with wrong password")
	}
	if !strings.Contains(err.Error(), "invalid credentials") {
		t.Errorf("error must be generic 'invalid credentials', got %q", err.Error())
	}
}

// TestUserService_Authenticate_UnknownEmail_SameErrorAsWrongPassword verifies that an
// unknown email returns the same "invalid credentials" error as a wrong password,
// preventing attackers from determining whether an email is registered.
func TestUserService_Authenticate_UnknownEmail_SameErrorAsWrongPassword(t *testing.T) {
	svc := newUserSvc(t)

	_, _, err := svc.Authenticate(context.Background(), "nobody@example.invalid", "anypassword")
	if err == nil {
		t.Fatal("Authenticate must fail for unknown email")
	}
	if !strings.Contains(err.Error(), "invalid credentials") {
		t.Errorf("error must be generic 'invalid credentials', got %q", err.Error())
	}
}

// TestUserService_CreateSuperadmin_SetsSuperadminFlag verifies that CreateSuperadmin
// creates a user with IsSuperadmin=true and a valid bcrypt password hash.
func TestUserService_CreateSuperadmin_SetsSuperadminFlag(t *testing.T) {
	db := testutil.OpenTestDB(t)
	suffix := testutil.UniqueSuffix(t)
	svc := service.NewUserService(store.NewUserStore(db), config.AuthConfig{JWTSecret: "test-secret-32bytes-minimum-len!"})

	u, err := svc.CreateSuperadmin(context.Background(), "admin_"+suffix, "admin_"+suffix+"@example.com", "adminpass")
	if err != nil {
		t.Fatalf("CreateSuperadmin: %v", err)
	}
	if !u.IsSuperadmin {
		t.Error("CreateSuperadmin must set IsSuperadmin=true")
	}
	if u.ID == 0 {
		t.Error("CreateSuperadmin must return a user with non-zero ID")
	}
	// Password must be hashed, not stored in plaintext.
	if u.PasswordHash == "adminpass" {
		t.Error("CreateSuperadmin must not store plaintext password")
	}
}

// TestUserService_GetByUsername_ReturnsUser verifies that GetByUsername finds a previously
// created user by their exact username.
func TestUserService_GetByUsername_ReturnsUser(t *testing.T) {
	db := testutil.OpenTestDB(t)
	suffix := testutil.UniqueSuffix(t)
	svc := service.NewUserService(store.NewUserStore(db), config.AuthConfig{JWTSecret: "test-secret-32bytes-minimum-len!"})

	u, err := svc.Create(context.Background(), "testuser_"+suffix, "getusr_"+suffix+"@example.com", "pass")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	found, err := svc.GetByUsername(context.Background(), "testuser_"+suffix)
	if err != nil {
		t.Fatalf("GetByUsername: %v", err)
	}
	if found.ID != u.ID {
		t.Errorf("want user ID %d, got %d", u.ID, found.ID)
	}
}

// TestUserService_GenerateTokenForUser_ReturnsNonEmptyToken verifies that
// GenerateTokenForUser produces a JWT for an existing user without requiring
// their password (used by the TOTP 2FA completion flow).
func TestUserService_GenerateTokenForUser_ReturnsNonEmptyToken(t *testing.T) {
	db := testutil.OpenTestDB(t)
	suffix := testutil.UniqueSuffix(t)
	svc := service.NewUserService(store.NewUserStore(db), config.AuthConfig{JWTSecret: "test-secret-32bytes-minimum-len!"})

	u, err := svc.Create(context.Background(), "testuser_"+suffix, "gentoken_"+suffix+"@example.com", "pass")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	token, err := svc.GenerateTokenForUser(context.Background(), u.ID)
	if err != nil {
		t.Fatalf("GenerateTokenForUser: %v", err)
	}
	if token == "" {
		t.Error("GenerateTokenForUser must return a non-empty JWT token")
	}
}
