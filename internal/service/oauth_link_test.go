package service_test

// Integration tests for linking a Google sign-in to an existing account by email.
// All tests require TEST_DATABASE_DSN and skip otherwise.

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/mkappworks-dev/cloudzilla-app/internal/config"
	"github.com/mkappworks-dev/cloudzilla-app/internal/service"
	"github.com/mkappworks-dev/cloudzilla-app/internal/store"
	"github.com/mkappworks-dev/cloudzilla-app/internal/testutil"
)

func newOAuthUserSvc(t *testing.T) *service.UserService {
	t.Helper()
	return service.NewUserService(store.NewUserStore(testutil.OpenTestDB(t)), config.AuthConfig{JWTSecret: "test-secret-32bytes-minimum-len!"})
}

func TestUserService_AuthenticateOAuth_LinksOnlyWhenBothSidesVerified(t *testing.T) {
	db := testutil.OpenTestDB(t)
	svc := newOAuthUserSvc(t)
	ctx := context.Background()
	tests := []struct {
		name           string
		googleVerified bool
		localVerified  bool
		wantErr        error
	}{
		{"both verified", true, true, nil},
		{"only Google verified", true, false, service.ErrOAuthAccountExists},
		{"only the local email verified", false, true, service.ErrOAuthEmailUnverified},
		{"neither verified", false, false, service.ErrOAuthEmailUnverified},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			suffix := testutil.UniqueSuffix(t)
			userID, email := testutil.SeedUserWithPassword(t, db, suffix, "password1")
			if tt.localVerified {
				testutil.Exec(t, db, `UPDATE users SET email_verified_at = NOW() WHERE id = $1`, userID)
			}
			id := service.OAuthIdentity{Provider: "google", ID: "g_link_" + suffix, Email: email, EmailVerified: tt.googleVerified}

			login, err := svc.AuthenticateOAuth(ctx, id, true, true)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("err = %v, want %v", err, tt.wantErr)
			}
			if tt.wantErr != nil {
				if login != nil {
					t.Errorf("signed in as user %d", login.User.ID)
				}
				if got := linkedOAuthID(t, db, userID); got != "" {
					t.Errorf("account linked to Google ID %q", got)
				}
				var n int
				if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM users WHERE email = $1`, email).Scan(&n); err != nil || n != 1 {
					t.Errorf("%d accounts with the email (err %v), want 1", n, err)
				}
				return
			}
			want := service.OAuthLink{UserID: userID, Email: email, Provider: "google", ID: id.ID}
			if login.User.ID != userID || login.Link == nil || *login.Link != want {
				t.Fatalf("login = user %d, link %+v; want user %d, link %+v", login.User.ID, login.Link, userID, want)
			}
			if got := linkedOAuthID(t, db, userID); got != "" {
				t.Fatalf("linked to %q before LinkByVerifiedEmail", got)
			}
			links, _ := newOAuthLinkSvc(t)
			if _, err := links.LinkByVerifiedEmail(ctx, *login.Link); err != nil {
				t.Fatalf("LinkByVerifiedEmail: %v", err)
			}
			if got := linkedOAuthID(t, db, userID); got != id.ID {
				t.Errorf("oauth_id = %q, want %q", got, id.ID)
			}
			again, err := svc.AuthenticateOAuth(ctx, id, false, true)
			if err != nil || again.User.ID != userID || again.Link != nil {
				t.Errorf("second sign-in = %+v, %v; want user %d found by Google ID", again, err, userID)
			}
		})
	}
}

// Google normalizes addresses to lower case; a verified local address that
// differs only in case is the same mailbox.
func TestUserService_AuthenticateOAuth_MatchesEmailCaseInsensitively(t *testing.T) {
	db := testutil.OpenTestDB(t)
	svc := newOAuthUserSvc(t)
	ctx := context.Background()
	suffix := testutil.UniqueSuffix(t)
	userID := testutil.SeedUser(t, db, suffix)
	local := "Mixed_" + suffix + "@Test.Invalid"
	testutil.Exec(t, db, `UPDATE users SET email = $2 WHERE id = $1`, userID, local)
	id := service.OAuthIdentity{Provider: "google", ID: "g_case_" + suffix, Email: strings.ToLower(local), EmailVerified: true}

	if _, err := svc.AuthenticateOAuth(ctx, id, true, true); !errors.Is(err, service.ErrOAuthAccountExists) {
		t.Errorf("unverified case variant: err = %v, want ErrOAuthAccountExists", err)
	}
	var n int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM users WHERE lower(email) = lower($1)`, local).Scan(&n); err != nil || n != 1 {
		t.Fatalf("%d accounts for the address (err %v), want 1: a duplicate was created", n, err)
	}

	testutil.Exec(t, db, `UPDATE users SET email_verified_at = NOW() WHERE id = $1`, userID)
	login, err := svc.AuthenticateOAuth(ctx, id, true, true)
	if err != nil || login.Link == nil || login.Link.UserID != userID || login.Link.Email != local {
		t.Fatalf("verified case variant = %+v, %v; want a link to user %d on %s", login, err, userID, local)
	}
}

func TestOAuthLinkService_LinkByVerifiedEmail_RefusesAChangedAccount(t *testing.T) {
	db := testutil.OpenTestDB(t)
	svc := newOAuthUserSvc(t)
	ctx := context.Background()
	suffix := testutil.UniqueSuffix(t)
	userID, email := testutil.SeedUserWithPassword(t, db, suffix, "password1")
	testutil.Exec(t, db, `UPDATE users SET email_verified_at = NOW() WHERE id = $1`, userID)
	login, err := svc.AuthenticateOAuth(ctx, service.OAuthIdentity{Provider: "google", ID: "g_race_" + suffix, Email: email, EmailVerified: true}, true, true)
	if err != nil || login.Link == nil {
		t.Fatalf("AuthenticateOAuth = %+v, %v; want a pending link", login, err)
	}

	testutil.Exec(t, db, `UPDATE users SET email = $2, email_verified_at = NULL WHERE id = $1`, userID, "changed_"+suffix+"@test.invalid")
	links, _ := newOAuthLinkSvc(t)
	if _, err := links.LinkByVerifiedEmail(ctx, *login.Link); !errors.Is(err, service.ErrOAuthAccountExists) {
		t.Errorf("LinkByVerifiedEmail after an email change: err = %v, want ErrOAuthAccountExists", err)
	}
	if got := linkedOAuthID(t, db, userID); got != "" {
		t.Errorf("account linked to %q after its email changed", got)
	}
}

// A link made without re-authentication must still reach the account's inbox,
// as a connect from settings does.
func TestOAuthLinkService_LinkByVerifiedEmail_EmailsTheAccount(t *testing.T) {
	smtp, box := testutil.FakeSMTP(t)
	db := testutil.OpenTestDB(t)
	users := store.NewUserStore(db)
	links := service.NewOAuthLinkService(users, store.NewOAuthStateStore(db), service.NewTOTPService(users), service.NewEmailService(smtp))
	ctx := context.Background()
	suffix := testutil.UniqueSuffix(t)
	userID, email := testutil.SeedUserWithPassword(t, db, suffix, "password1")
	testutil.Exec(t, db, `UPDATE users SET email_verified_at = NOW() WHERE id = $1`, userID)
	login, err := newOAuthUserSvc(t).AuthenticateOAuth(ctx, service.OAuthIdentity{Provider: "google", ID: "g_notice_" + suffix, Email: email, EmailVerified: true}, true, true)
	if err != nil || login.Link == nil {
		t.Fatalf("AuthenticateOAuth = %+v, %v; want a pending link", login, err)
	}

	if _, err := links.LinkByVerifiedEmail(ctx, *login.Link); err != nil {
		t.Fatalf("LinkByVerifiedEmail: %v", err)
	}
	mail := box.Next(t)
	if len(mail.To) != 1 || mail.To[0] != email || !strings.Contains(mail.Data, "connected") {
		t.Errorf("notice sent to %v: %.300s; want a connected notice to %s", mail.To, mail.Data, email)
	}
}

func TestUserService_AuthenticateOAuth_DoesNotRelinkAnAccountLinkedElsewhere(t *testing.T) {
	db := testutil.OpenTestDB(t)
	svc := newOAuthUserSvc(t)
	suffix := testutil.UniqueSuffix(t)
	userID, email := testutil.SeedUserWithPassword(t, db, suffix, "password1")
	testutil.Exec(t, db, `UPDATE users SET email_verified_at = NOW(), oauth_provider = 'google', oauth_id = $2 WHERE id = $1`, userID, "g_owner_"+suffix)

	login, err := svc.AuthenticateOAuth(context.Background(), service.OAuthIdentity{
		Provider: "google", ID: "g_other_" + suffix, Email: email, EmailVerified: true,
	}, true, true)
	if !errors.Is(err, service.ErrOAuthAlreadyLinked) {
		t.Errorf("err = %v, want ErrOAuthAlreadyLinked", err)
	}
	if login != nil {
		t.Errorf("a second Google account signed in as user %d", login.User.ID)
	}
	if got := linkedOAuthID(t, db, userID); got != "g_owner_"+suffix {
		t.Errorf("oauth_id = %q, want the original Google ID kept", got)
	}
}

func TestUserService_AuthenticateOAuth_LinkKeepsTheLoginGate(t *testing.T) {
	db := testutil.OpenTestDB(t)
	svc := newOAuthUserSvc(t)
	ctx := context.Background()
	suffix := testutil.UniqueSuffix(t)
	userID := testutil.SeedUser(t, db, suffix)
	email := "testuser_" + suffix + "@test.invalid"
	testutil.Exec(t, db, `UPDATE users SET email_verified_at = NOW() WHERE id = $1`, userID)
	id := service.OAuthIdentity{Provider: "google", ID: "g_gate_" + suffix, Email: email, EmailVerified: true}

	if login, err := svc.AuthenticateOAuth(ctx, id, true, false); !errors.Is(err, service.ErrLoginDisabled) || login != nil {
		t.Fatalf("login disabled: = %v, %v; want ErrLoginDisabled", login, err)
	}

	testutil.Exec(t, db, `UPDATE users SET is_invited = TRUE WHERE id = $1`, userID)
	if login, err := svc.AuthenticateOAuth(ctx, id, true, false); err != nil || login.User.ID != userID || login.Link == nil {
		t.Errorf("invited user with login disabled = %+v, %v; want a pending link", login, err)
	}
}

func TestUserService_AuthenticateOAuth_GoogleSignUpStartsVerified(t *testing.T) {
	db := testutil.OpenTestDB(t)
	svc := newOAuthUserSvc(t)
	suffix := testutil.UniqueSuffix(t)
	login, err := svc.AuthenticateOAuth(context.Background(), service.OAuthIdentity{
		Provider: "google", ID: "g_new_" + suffix, Email: "google_" + suffix + "@example.com", EmailVerified: true,
	}, true, true)
	if err != nil {
		t.Fatalf("AuthenticateOAuth: %v", err)
	}
	t.Cleanup(func() { testutil.DeleteUsers(t, db, login.User.ID) })
	if !login.User.EmailVerified() || !emailVerified(t, db, login.User.ID) || login.Link != nil {
		t.Errorf("Google sign-up: verified=%v (db %v), link=%+v; want verified, no link",
			login.User.EmailVerified(), emailVerified(t, db, login.User.ID), login.Link)
	}
}
