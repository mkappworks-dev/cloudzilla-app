package service_test

// Integration tests for UserService. All tests require TEST_DATABASE_DSN and skip otherwise.

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/bcrypt"

	"github.com/mkappworks-dev/cloudzilla-app/internal/config"
	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
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
	t.Cleanup(func() { testutil.DeleteUsers(t, db, u.ID) })

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
	t.Cleanup(func() { testutil.DeleteUsers(t, db, u.ID) })
	if u.ID == 0 {
		t.Error("Create must return a user with non-zero ID")
	}
}

func TestUserService_Create_DuplicateUsername_ReturnsErrUsernameTaken(t *testing.T) {
	db := testutil.OpenTestDB(t)
	suffix := testutil.UniqueSuffix(t)
	testutil.SeedUser(t, db, suffix)
	svc := service.NewUserService(store.NewUserStore(db), config.AuthConfig{JWTSecret: "test-secret-32bytes-minimum-len!"})

	_, err := svc.Create(context.Background(), "testuser_"+suffix, "dupname_"+suffix+"@example.com", "pass")
	if !errors.Is(err, service.ErrUsernameTaken) {
		t.Errorf("want ErrUsernameTaken, got %v", err)
	}
}

// Covers the submit that loses a race: the invite was usable when the page
// loaded but was claimed before this Create ran.
func TestUserService_CreateFromInvitation_ClaimedInvitation_CreatesNoUser(t *testing.T) {
	db := testutil.OpenTestDB(t)
	suffix := testutil.UniqueSuffix(t)
	email := "claimed_" + suffix + "@test.invalid"
	id, _ := testutil.SeedInvitation(t, db, email, time.Now().UTC().Add(time.Hour))
	if _, err := db.ExecContext(context.Background(), `UPDATE invitations SET accepted_at = NOW() WHERE id = $1`, id); err != nil {
		t.Fatalf("accept invitation: %v", err)
	}
	svc := service.NewUserService(store.NewUserStore(db), config.AuthConfig{JWTSecret: "test-secret-32bytes-minimum-len!"})

	_, err := svc.CreateFromInvitation(context.Background(), &model.Invitation{ID: id, Email: email}, "claimed_"+suffix, "pass")

	if !errors.Is(err, service.ErrInvitationUnusable) {
		t.Errorf("want ErrInvitationUnusable, got %v", err)
	}
	var n int
	if err := db.QueryRowContext(context.Background(), `SELECT COUNT(*) FROM users WHERE email = $1`, email).Scan(&n); err != nil {
		t.Fatalf("count users: %v", err)
	}
	if n != 0 {
		t.Errorf("a claimed invitation must not create a user; found %d", n)
	}
}

func TestUserService_Create_InvalidUsername_ReturnsErrInvalidOwnerNameAndNoUser(t *testing.T) {
	db := testutil.OpenTestDB(t)
	suffix := testutil.UniqueSuffix(t)
	svc := service.NewUserService(store.NewUserStore(db), config.AuthConfig{JWTSecret: "test-secret-32bytes-minimum-len!"})
	cases := []struct {
		name   string
		create func(email string) error
	}{
		{"Create", func(email string) error {
			_, err := svc.Create(context.Background(), "../x", email, "password123")
			return err
		}},
		{"CreateSuperadmin", func(email string) error {
			_, err := svc.CreateSuperadmin(context.Background(), "admin", email, "password123")
			return err
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			email := "badname_" + tc.name + "_" + suffix + "@test.invalid"
			t.Cleanup(func() { testutil.Exec(t, db, `DELETE FROM users WHERE email = $1`, email) })

			if err := tc.create(email); !errors.Is(err, service.ErrInvalidOwnerName) {
				t.Errorf("want ErrInvalidOwnerName, got %v", err)
			}
			var n int
			if err := db.QueryRowContext(context.Background(), `SELECT COUNT(*) FROM users WHERE email = $1`, email).Scan(&n); err != nil {
				t.Fatalf("count users: %v", err)
			}
			if n != 0 {
				t.Errorf("an invalid username must not create a user; found %d", n)
			}
		})
	}
}

func TestUserService_CreateFromInvitation_InvalidUsername_InvitationStaysUsable(t *testing.T) {
	db := testutil.OpenTestDB(t)
	email := "badinvite_" + testutil.UniqueSuffix(t) + "@test.invalid"
	id, _ := testutil.SeedInvitation(t, db, email, time.Now().UTC().Add(time.Hour))
	t.Cleanup(func() { testutil.Exec(t, db, `DELETE FROM users WHERE email = $1`, email) })
	svc := service.NewUserService(store.NewUserStore(db), config.AuthConfig{JWTSecret: "test-secret-32bytes-minimum-len!"})

	_, err := svc.CreateFromInvitation(context.Background(), &model.Invitation{ID: id, Email: email}, "a/b", "password123")

	if !errors.Is(err, service.ErrInvalidOwnerName) {
		t.Errorf("want ErrInvalidOwnerName, got %v", err)
	}
	var accepted bool
	if err := db.QueryRowContext(context.Background(), `SELECT accepted_at IS NOT NULL FROM invitations WHERE id = $1`, id).Scan(&accepted); err != nil {
		t.Fatalf("read invitation: %v", err)
	}
	if accepted {
		t.Error("a rejected username must leave the invitation usable")
	}
}

func TestUserService_Create_DuplicateEmail_ReturnsErrEmailTaken(t *testing.T) {
	db := testutil.OpenTestDB(t)
	suffix := testutil.UniqueSuffix(t)
	testutil.SeedUser(t, db, suffix)
	svc := service.NewUserService(store.NewUserStore(db), config.AuthConfig{JWTSecret: "test-secret-32bytes-minimum-len!"})

	_, err := svc.Create(context.Background(), "dupemail_"+suffix, "testuser_"+suffix+"@test.invalid", "pass")
	if !errors.Is(err, service.ErrEmailTaken) {
		t.Errorf("want ErrEmailTaken, got %v", err)
	}
}

func TestUserService_Create_EmailDiffersOnlyByCase_ReturnsErrEmailTaken(t *testing.T) {
	db := testutil.OpenTestDB(t)
	suffix := testutil.UniqueSuffix(t)
	testutil.SeedUser(t, db, suffix)
	svc := service.NewUserService(store.NewUserStore(db), config.AuthConfig{JWTSecret: "test-secret-32bytes-minimum-len!"})

	t.Cleanup(func() { testutil.Exec(t, db, `DELETE FROM users WHERE username = $1`, "caseemail_"+suffix) })

	_, err := svc.Create(context.Background(), "caseemail_"+suffix, "TestUser_"+suffix+"@Test.Invalid", "pass")
	if !errors.Is(err, service.ErrEmailTaken) {
		t.Errorf("want ErrEmailTaken, got %v", err)
	}
}

func TestUserService_Authenticate_EmailInOtherCase_ReturnsToken(t *testing.T) {
	db := testutil.OpenTestDB(t)
	suffix := testutil.UniqueSuffix(t)
	_, email := testutil.SeedUserWithPassword(t, db, suffix, "correctpassword")
	svc := service.NewUserService(store.NewUserStore(db), config.AuthConfig{JWTSecret: "test-secret-32bytes-minimum-len!"})

	if _, _, err := svc.Authenticate(context.Background(), strings.ToUpper(email), "correctpassword"); err != nil {
		t.Errorf("login must ignore email case: %v", err)
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
	_, email := testutil.SeedUserWithPassword(t, testutil.OpenTestDB(t), testutil.UniqueSuffix(t), "correctpassword")
	svc := newUserSvc(t)

	_, _, wrongErr := svc.Authenticate(context.Background(), email, "wrongpassword")
	_, _, err := svc.Authenticate(context.Background(), "nobody@example.invalid", "anypassword")
	if err == nil || wrongErr == nil {
		t.Fatalf("Authenticate must fail for an unknown email and a wrong password; got %v and %v", err, wrongErr)
	}
	if err.Error() != wrongErr.Error() {
		t.Errorf("unknown email error %q differs from wrong password error %q", err, wrongErr)
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
	t.Cleanup(func() { testutil.DeleteUsers(t, db, u.ID) })
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

func TestUserService_Create_RejectsInvalidUsername(t *testing.T) {
	db := testutil.OpenTestDB(t)
	svc := service.NewUserService(store.NewUserStore(db), config.AuthConfig{JWTSecret: "test-secret-32bytes-minimum-len!"})
	for _, name := range append([]string{"-bob", "bob smith", strings.Repeat("a", 40)}, testutil.HostileNames...) {
		suffix := testutil.UniqueSuffix(t)
		_, err := svc.Create(context.Background(), name, "bad_"+suffix+"@example.com", "password1")
		if !errors.Is(err, service.ErrInvalidOwnerName) {
			t.Errorf("Create(%q) = %v, want ErrInvalidOwnerName", name, err)
		}
		if u, err := svc.GetByUsername(context.Background(), name); err == nil {
			t.Errorf("Create(%q) stored the user", name)
			testutil.DeleteUsers(t, db, u.ID)
		}
	}
}

func TestUserService_CreateSuperadmin_RejectsInvalidUsername(t *testing.T) {
	db := testutil.OpenTestDB(t)
	svc := service.NewUserService(store.NewUserStore(db), config.AuthConfig{JWTSecret: "test-secret-32bytes-minimum-len!"})
	for _, name := range testutil.HostileNames {
		suffix := testutil.UniqueSuffix(t)
		_, err := svc.CreateSuperadmin(context.Background(), name, "badadmin_"+suffix+"@example.com", "password1")
		if !errors.Is(err, service.ErrInvalidOwnerName) {
			t.Errorf("CreateSuperadmin(%q) = %v, want ErrInvalidOwnerName", name, err)
		}
		if u, err := svc.GetByUsername(context.Background(), name); err == nil {
			t.Errorf("CreateSuperadmin(%q) stored the user", name)
			testutil.DeleteUsers(t, db, u.ID)
		}
	}
}

// Google display names are free text; the derived username must still validate.
func TestUserService_AuthenticateOAuth_DerivesValidUsername(t *testing.T) {
	db := testutil.OpenTestDB(t)
	svc := service.NewUserService(store.NewUserStore(db), config.AuthConfig{JWTSecret: "test-secret-32bytes-minimum-len!"})
	for _, name := range append([]string{"-_Bob", strings.Repeat("Long Name ", 10)}, testutil.HostileNames...) {
		suffix := testutil.UniqueSuffix(t)
		u, _, err := svc.AuthenticateOAuth(context.Background(), service.OAuthIdentity{
			Provider: "google", ID: "g_" + suffix, Email: "oauth_" + suffix + "@example.com", EmailVerified: true, Name: name,
		}, true, true)
		if err != nil {
			t.Fatalf("AuthenticateOAuth(%q): %v", name, err)
		}
		testutil.DeleteUsers(t, db, u.ID)
		if err := service.ValidateOwnerName(u.Username); err != nil {
			t.Errorf("display name %q produced invalid username %q", name, u.Username)
		}
	}
}

func linkedOAuthID(t *testing.T, db *sql.DB, userID int64) string {
	t.Helper()
	var oauthID string
	if err := db.QueryRowContext(context.Background(), `SELECT oauth_id FROM users WHERE id = $1`, userID).Scan(&oauthID); err != nil {
		t.Fatalf("read oauth_id: %v", err)
	}
	return oauthID
}

func TestUserService_AuthenticateOAuth_UnverifiedEmailNeitherLinksNorCreates(t *testing.T) {
	db := testutil.OpenTestDB(t)
	svc := service.NewUserService(store.NewUserStore(db), config.AuthConfig{JWTSecret: "test-secret-32bytes-minimum-len!"})
	suffix := testutil.UniqueSuffix(t)
	victimID, victimEmail := testutil.SeedUserWithPassword(t, db, suffix, "password1")

	for _, email := range []string{victimEmail, "unverified_" + suffix + "@example.com"} {
		u, _, err := svc.AuthenticateOAuth(context.Background(), service.OAuthIdentity{
			Provider: "google", ID: "g_unverified_" + suffix, Email: email, Name: "Mallory",
		}, true, true)
		if !errors.Is(err, service.ErrOAuthEmailUnverified) {
			t.Errorf("unverified %s: err = %v, want ErrOAuthEmailUnverified", email, err)
		}
		if u != nil {
			t.Errorf("unverified %s signed in as user %d", email, u.ID)
		}
	}
	if got := linkedOAuthID(t, db, victimID); got != "" {
		t.Errorf("victim's account was linked to Google ID %q", got)
	}
	var created int
	if err := db.QueryRowContext(context.Background(), `SELECT COUNT(*) FROM users WHERE email = $1`, "unverified_"+suffix+"@example.com").Scan(&created); err != nil {
		t.Fatal(err)
	}
	if created != 0 {
		t.Errorf("an account was created for the unverified email")
	}
}

// Local emails are unverified: anyone can register, be invited with, or
// change their profile to someone else's address before that person first
// signs in with Google.
func TestUserService_AuthenticateOAuth_DoesNotLinkByEmail(t *testing.T) {
	db := testutil.OpenTestDB(t)
	svc := service.NewUserService(store.NewUserStore(db), config.AuthConfig{JWTSecret: "test-secret-32bytes-minimum-len!"})
	ctx := context.Background()
	suffix := testutil.UniqueSuffix(t)
	victimEmail := "victim_" + suffix + "@example.com"
	attacker, err := svc.Create(ctx, "atk_"+suffix, victimEmail, "attackerpass1")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	t.Cleanup(func() { testutil.DeleteUsers(t, db, attacker.ID) })

	for _, allowRegistration := range []bool{true, false} {
		u, _, err := svc.AuthenticateOAuth(ctx, service.OAuthIdentity{
			Provider: "google", ID: "g_victim_" + suffix, Email: victimEmail, EmailVerified: true,
		}, allowRegistration, true)
		if !errors.Is(err, service.ErrOAuthAccountExists) {
			t.Errorf("allowRegistration=%v: err = %v, want ErrOAuthAccountExists", allowRegistration, err)
		}
		if u != nil {
			t.Errorf("allowRegistration=%v: the victim's Google login landed in user %d", allowRegistration, u.ID)
		}
	}
	if got := linkedOAuthID(t, db, attacker.ID); got != "" {
		t.Errorf("the attacker's account was linked to Google ID %q", got)
	}
}

func TestUserService_AuthenticateOAuth_LinkedIDSkipsTheEmailChecks(t *testing.T) {
	db := testutil.OpenTestDB(t)
	svc := service.NewUserService(store.NewUserStore(db), config.AuthConfig{JWTSecret: "test-secret-32bytes-minimum-len!"})
	suffix := testutil.UniqueSuffix(t)
	id := service.OAuthIdentity{Provider: "google", ID: "g_linked_" + suffix, Email: "linked_" + suffix + "@example.com", EmailVerified: true}

	u, _, err := svc.AuthenticateOAuth(context.Background(), id, true, true)
	if err != nil {
		t.Fatalf("first login: %v", err)
	}
	t.Cleanup(func() { testutil.DeleteUsers(t, db, u.ID) })
	if got := linkedOAuthID(t, db, u.ID); got != id.ID {
		t.Fatalf("oauth_id = %q, want %q", got, id.ID)
	}

	id.EmailVerified = false
	if again, _, err := svc.AuthenticateOAuth(context.Background(), id, false, true); err != nil || again.ID != u.ID {
		t.Errorf("linked login with an unverified email = (%v, %v), want user %d", again, err, u.ID)
	}
}

// /{owner} resolves a user before an org, so a user holding an org's name
// hides the org and its repos behind the user's.
func TestUserService_AccountCreation_RefusesOrgNames(t *testing.T) {
	db := testutil.OpenTestDB(t)
	ctx := context.Background()
	users := store.NewUserStore(db)
	auth := config.AuthConfig{JWTSecret: "test-secret-32bytes-minimum-len!"}
	svc := service.NewUserService(users, auth)
	suffix := testutil.UniqueSuffix(t)
	orgs := service.NewOrgService(store.NewOrgStore(db), store.NewRepoStore(db), users, config.GitConfig{ReposRoot: t.TempDir()})
	name := "acme" + strings.ReplaceAll(suffix, "_", "")
	org, err := orgs.Create(ctx, testutil.SeedUser(t, db, suffix), name, "", "")
	if err != nil {
		t.Fatalf("create org: %v", err)
	}
	t.Cleanup(func() { testutil.Exec(t, db, `DELETE FROM organizations WHERE id = $1`, org.ID) })

	if _, err := svc.Create(ctx, name, "reg_"+suffix+"@example.com", "password1"); !errors.Is(err, service.ErrUsernameTaken) {
		t.Errorf("Create: err = %v, want ErrUsernameTaken", err)
	}
	if _, err := svc.CreateSuperadmin(ctx, name, "admin_"+suffix+"@example.com", "password1"); !errors.Is(err, service.ErrUsernameTaken) {
		t.Errorf("CreateSuperadmin: err = %v, want ErrUsernameTaken", err)
	}
	u, _, err := svc.AuthenticateOAuth(ctx, service.OAuthIdentity{
		Provider: "google", ID: "g_" + suffix, Email: "google_" + suffix + "@example.com", EmailVerified: true, Name: name,
	}, true, true)
	if err != nil {
		t.Fatalf("AuthenticateOAuth: %v", err)
	}
	t.Cleanup(func() { testutil.DeleteUsers(t, db, u.ID) })
	if u.Username == name {
		t.Errorf("Google sign-up derived the org's name %q", name)
	}

	var n int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM users WHERE username = $1`, name).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		testutil.Exec(t, db, `DELETE FROM users WHERE username = $1`, name)
		t.Errorf("%d users took the org's name", n)
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
	t.Cleanup(func() { testutil.DeleteUsers(t, db, u.ID) })

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
	t.Cleanup(func() { testutil.DeleteUsers(t, db, u.ID) })

	token, err := svc.GenerateTokenForUser(context.Background(), u.ID)
	if err != nil {
		t.Fatalf("GenerateTokenForUser: %v", err)
	}
	if token == "" {
		t.Error("GenerateTokenForUser must return a non-empty JWT token")
	}
}

func TestUserService_UpdateProfile_LeavesUsernameUnchanged(t *testing.T) {
	db := testutil.OpenTestDB(t)
	suffix := testutil.UniqueSuffix(t)
	userID := testutil.SeedUser(t, db, suffix)
	svc := service.NewUserService(store.NewUserStore(db), config.AuthConfig{JWTSecret: "test-secret-32bytes-minimum-len!"})
	ctx := context.Background()

	newEmail := "profile_" + suffix + "@test.invalid"
	if err := svc.UpdateProfile(ctx, userID, "New Name", newEmail, "bio", "Acme", "Colombo"); err != nil {
		t.Fatalf("UpdateProfile: %v", err)
	}

	u, err := svc.GetByID(ctx, userID)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if want := "testuser_" + suffix; u.Username != want {
		t.Errorf("username = %q, want %q", u.Username, want)
	}
	if u.Name != "New Name" || u.Email != newEmail || u.Bio != "bio" || u.Company != "Acme" || u.Location != "Colombo" {
		t.Errorf("profile fields not saved: name=%q email=%q bio=%q company=%q location=%q", u.Name, u.Email, u.Bio, u.Company, u.Location)
	}
}

func TestUserService_UpdateNotificationPrefs_RoundTrips(t *testing.T) {
	db := testutil.OpenTestDB(t)
	userID := testutil.SeedUser(t, db, testutil.UniqueSuffix(t))
	svc := service.NewUserService(store.NewUserStore(db), config.AuthConfig{JWTSecret: "test-secret-32bytes-minimum-len!"})
	ctx := context.Background()

	for _, want := range []model.NotificationPrefs{
		{EmailNotifications: false, EmailDigest: model.EmailDigestWeekly, NotifyPRReview: false, NotifyMention: true},
		{EmailNotifications: true, EmailDigest: model.EmailDigestDaily, NotifyPRReview: true, NotifyMention: false},
	} {
		if err := svc.UpdateNotificationPrefs(ctx, userID, want); err != nil {
			t.Fatalf("UpdateNotificationPrefs(%+v): %v", want, err)
		}
		u, err := svc.GetByID(ctx, userID)
		if err != nil {
			t.Fatalf("GetByID: %v", err)
		}
		got := model.NotificationPrefs{
			EmailNotifications: u.EmailNotifications,
			EmailDigest:        u.EmailDigest,
			NotifyPRReview:     u.NotifyPRReview,
			NotifyMention:      u.NotifyMention,
		}
		if got != want {
			t.Errorf("saved prefs = %+v, want %+v", got, want)
		}
	}
}

func TestUserService_UpdateNotificationPrefs_UnknownDigestFallsBackToImmediate(t *testing.T) {
	db := testutil.OpenTestDB(t)
	userID := testutil.SeedUser(t, db, testutil.UniqueSuffix(t))
	svc := service.NewUserService(store.NewUserStore(db), config.AuthConfig{JWTSecret: "test-secret-32bytes-minimum-len!"})
	ctx := context.Background()

	if err := svc.UpdateNotificationPrefs(ctx, userID, model.NotificationPrefs{EmailNotifications: true, EmailDigest: "hourly"}); err != nil {
		t.Fatalf("UpdateNotificationPrefs: %v", err)
	}
	u, err := svc.GetByID(ctx, userID)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if u.EmailDigest != model.EmailDigestImmediate {
		t.Errorf("email_digest = %q, want %q", u.EmailDigest, model.EmailDigestImmediate)
	}
}
