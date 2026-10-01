package service_test

// Integration tests for confirming sensitive actions. All tests require TEST_DATABASE_DSN and skip otherwise.

import (
	"bytes"
	"compress/flate"
	"context"
	"database/sql"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/mkappworks-dev/cloudzilla-app/internal/config"
	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/service"
	"github.com/mkappworks-dev/cloudzilla-app/internal/store"
	"github.com/mkappworks-dev/cloudzilla-app/internal/testutil"
)

func newReauth(t *testing.T) (*service.ReauthService, *service.UserService) {
	t.Helper()
	users := store.NewUserStore(testutil.OpenTestDB(t))
	return service.NewReauthService(users, service.NewTOTPService(users)),
		service.NewUserService(users, config.AuthConfig{JWTSecret: "test-secret-32bytes-minimum-len!", JWTExpiry: time.Hour})
}

func guessWrong(t *testing.T, reauth *service.ReauthService, userID int64, n int) {
	t.Helper()
	for i := range n {
		if _, err := reauth.Confirm(context.Background(), userID, service.Confirmation{Password: "guess"}); !errors.Is(err, service.ErrReauthFailed) {
			t.Fatalf("guess %d: err = %v, want ErrReauthFailed", i, err)
		}
	}
}

func TestReauthService_ChecksTheFactorsTheAccountHas(t *testing.T) {
	db := testutil.OpenTestDB(t)
	reauth, _ := newReauth(t)
	ctx := context.Background()
	passwordOnly, _ := testutil.SeedUserWithPassword(t, db, testutil.UniqueSuffix(t), "password1")
	withTOTP, _ := testutil.SeedUserWithPassword(t, db, testutil.UniqueSuffix(t), "password1")
	testutil.EnableTOTP(t, db, withTOTP)
	noFactors := testutil.SeedPasswordlessUser(t, db, testutil.UniqueSuffix(t), "g_nofactor_"+testutil.UniqueSuffix(t))
	totpOnly := testutil.SeedPasswordlessUser(t, db, testutil.UniqueSuffix(t), "g_totponly_"+testutil.UniqueSuffix(t))
	testutil.EnableTOTP(t, db, totpOnly)
	code := func() string { return testutil.TOTPCode(t, testutil.TestTOTPSecret) }

	tests := []struct {
		name    string
		userID  int64
		c       service.Confirmation
		wantErr error
	}{
		{"password", passwordOnly, service.Confirmation{Password: "password1"}, nil},
		{"wrong password", passwordOnly, service.Confirmation{Password: "password2"}, service.ErrReauthFailed},
		{"no password", passwordOnly, service.Confirmation{}, service.ErrReauthFailed},
		{"password and code", withTOTP, service.Confirmation{Password: "password1", Code: code()}, nil},
		{"password without the code", withTOTP, service.Confirmation{Password: "password1"}, service.ErrReauthFailed},
		{"code without the password", withTOTP, service.Confirmation{Code: code()}, service.ErrReauthFailed},
		{"code for a passwordless account", totpOnly, service.Confirmation{Code: code()}, nil},
		{"no code for a passwordless account", totpOnly, service.Confirmation{}, service.ErrReauthFailed},
		{"an account with nothing to confirm with", noFactors, service.Confirmation{}, service.ErrReauthUnavailable},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := reauth.Confirm(ctx, tt.userID, tt.c); !errors.Is(err, tt.wantErr) {
				t.Errorf("Confirm = %v, want %v", err, tt.wantErr)
			}
		})
	}
	if _, err := reauth.ConfirmWithPassword(ctx, noFactors, service.Confirmation{}); !errors.Is(err, service.ErrReauthNoPassword) {
		t.Errorf("ConfirmWithPassword on a passwordless account = %v, want ErrReauthNoPassword", err)
	}
}

// Without a limit, a stolen session could guess the account's password through
// any confirmed action.
func TestReauthService_ThrottlesFailuresPerUser(t *testing.T) {
	db := testutil.OpenTestDB(t)
	reauth, _ := newReauth(t)
	ctx := context.Background()
	victim, _ := testutil.SeedUserWithPassword(t, db, testutil.UniqueSuffix(t), "password1")
	bystander, _ := testutil.SeedUserWithPassword(t, db, testutil.UniqueSuffix(t), "password1")

	for i := range 5 {
		if _, err := reauth.Confirm(ctx, victim, service.Confirmation{Password: "guess"}); !errors.Is(err, service.ErrReauthFailed) {
			t.Fatalf("guess %d: err = %v, want ErrReauthFailed", i, err)
		}
	}
	if _, err := reauth.Confirm(ctx, victim, service.Confirmation{Password: "password1"}); !errors.Is(err, service.ErrReauthThrottled) {
		t.Errorf("right password after 5 failures: err = %v, want ErrReauthThrottled", err)
	}
	if _, err := reauth.Confirm(ctx, bystander, service.Confirmation{Password: "password1"}); err != nil {
		t.Errorf("another user's confirmation: err = %v, want nil", err)
	}
}

// Each server process has its own ReauthService, so the count lives in the
// database: spreading guesses over instances still gets five.
func TestReauthService_FailuresCountAcrossInstances(t *testing.T) {
	db := testutil.OpenTestDB(t)
	first, _ := newReauth(t)
	second, _ := newReauth(t)
	userID, _ := testutil.SeedUserWithPassword(t, db, testutil.UniqueSuffix(t), "password1")

	guessWrong(t, first, userID, 3)
	guessWrong(t, second, userID, 2)
	if _, err := first.Confirm(context.Background(), userID, service.Confirmation{Password: "password1"}); !errors.Is(err, service.ErrReauthThrottled) {
		t.Errorf("right password after 5 failures across instances: err = %v, want ErrReauthThrottled", err)
	}
}

// The claim is one conditional UPDATE, so guesses racing each other can't all
// see four failures and all be checked.
func TestReauthService_ConcurrentGuessesGetOnlyTheLimit(t *testing.T) {
	db := testutil.OpenTestDB(t)
	reauth, _ := newReauth(t)
	userID, _ := testutil.SeedUserWithPassword(t, db, testutil.UniqueSuffix(t), "password1")

	const guesses = 20
	var failed, throttled atomic.Int32
	var wg sync.WaitGroup
	start := make(chan struct{})
	for range guesses {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			_, err := reauth.Confirm(context.Background(), userID, service.Confirmation{Password: "guess"})
			switch {
			case errors.Is(err, service.ErrReauthFailed):
				failed.Add(1)
			case errors.Is(err, service.ErrReauthThrottled):
				throttled.Add(1)
			default:
				t.Errorf("guess: err = %v", err)
			}
		}()
	}
	close(start)
	wg.Wait()
	if failed.Load() != 5 || throttled.Load() != guesses-5 {
		t.Errorf("%d guesses checked and %d throttled, want 5 and %d", failed.Load(), throttled.Load(), guesses-5)
	}
}

// The window runs from its first failure, so waiting it out lifts the limit
// while more failures inside it don't extend it.
func TestReauthService_ThrottleLiftsWhenTheWindowEnds(t *testing.T) {
	db := testutil.OpenTestDB(t)
	reauth, _ := newReauth(t)
	ctx := context.Background()
	userID, _ := testutil.SeedUserWithPassword(t, db, testutil.UniqueSuffix(t), "password1")
	right := service.Confirmation{Password: "password1"}

	guessWrong(t, reauth, userID, 5)
	testutil.Exec(t, db, `UPDATE users SET reauth_window_start = NOW() - interval '14 minutes' WHERE id = $1`, userID)
	if _, err := reauth.Confirm(ctx, userID, right); !errors.Is(err, service.ErrReauthThrottled) {
		t.Errorf("14 minutes into the window: err = %v, want ErrReauthThrottled", err)
	}
	testutil.Exec(t, db, `UPDATE users SET reauth_window_start = NOW() - interval '16 minutes' WHERE id = $1`, userID)
	if _, err := reauth.Confirm(ctx, userID, right); err != nil {
		t.Errorf("after the window: err = %v, want nil", err)
	}
	guessWrong(t, reauth, userID, 5)
	if _, err := reauth.Confirm(ctx, userID, right); !errors.Is(err, service.ErrReauthThrottled) {
		t.Errorf("5 failures in the new window: err = %v, want ErrReauthThrottled", err)
	}
}

// Only failures use up the window, so someone who confirms often isn't locked out.
func TestReauthService_SuccessesDoNotCount(t *testing.T) {
	db := testutil.OpenTestDB(t)
	reauth, _ := newReauth(t)
	ctx := context.Background()
	userID, _ := testutil.SeedUserWithPassword(t, db, testutil.UniqueSuffix(t), "password1")
	right := service.Confirmation{Password: "password1"}

	for i := range 10 {
		if _, err := reauth.Confirm(ctx, userID, right); err != nil {
			t.Fatalf("confirmation %d: err = %v", i, err)
		}
	}
	guessWrong(t, reauth, userID, 4)
	if _, err := reauth.Confirm(ctx, userID, right); err != nil {
		t.Fatalf("right password after 4 failures: err = %v", err)
	}
	guessWrong(t, reauth, userID, 1)
	if _, err := reauth.Confirm(ctx, userID, right); !errors.Is(err, service.ErrReauthThrottled) {
		t.Errorf("right password after the fifth failure: err = %v, want ErrReauthThrottled", err)
	}
}

// Past the password, the sign-in code page is where a stolen password could
// guess the code, so it draws on the same window.
func TestReauthService_CheckSecondFactor_IsThrottled(t *testing.T) {
	db := testutil.OpenTestDB(t)
	reauth, _ := newReauth(t)
	ctx := context.Background()
	userID, _ := testutil.SeedUserWithPassword(t, db, testutil.UniqueSuffix(t), "password1")
	testutil.EnableTOTP(t, db, userID)
	code := func() string { return testutil.TOTPCode(t, testutil.TestTOTPSecret) }

	if err := reauth.CheckSecondFactor(ctx, userID, code(), ""); err != nil {
		t.Fatalf("right code: err = %v", err)
	}
	for i, c := range []struct{ code, backup string }{{"", ""}, {"000000", ""}, {"", "deadbeef"}, {"123456", "cafebabe"}} {
		if err := reauth.CheckSecondFactor(ctx, userID, c.code, c.backup); !errors.Is(err, service.ErrReauthFailed) {
			t.Fatalf("wrong code %d: err = %v, want ErrReauthFailed", i, err)
		}
	}
	guessWrong(t, reauth, userID, 1)
	if err := reauth.CheckSecondFactor(ctx, userID, code(), ""); !errors.Is(err, service.ErrReauthThrottled) {
		t.Errorf("right code after 5 failures: err = %v, want ErrReauthThrottled", err)
	}
}

func TestReauthService_Factors(t *testing.T) {
	db := testutil.OpenTestDB(t)
	reauth, _ := newReauth(t)
	passwordOnly, _ := testutil.SeedUserWithPassword(t, db, testutil.UniqueSuffix(t), "password1")
	withTOTP, _ := testutil.SeedUserWithPassword(t, db, testutil.UniqueSuffix(t), "password1")
	testutil.EnableTOTP(t, db, withTOTP)
	totpOnly := testutil.SeedPasswordlessUser(t, db, testutil.UniqueSuffix(t), "g_factors_"+testutil.UniqueSuffix(t))
	testutil.EnableTOTP(t, db, totpOnly)

	for _, tt := range []struct {
		name   string
		userID int64
		want   service.Factors
	}{
		{"password only", passwordOnly, service.Factors{Password: true}},
		{"password and 2FA", withTOTP, service.Factors{Password: true, Code: true}},
		{"2FA only", totpOnly, service.Factors{Code: true}},
	} {
		if got, err := reauth.Factors(context.Background(), tt.userID); err != nil || got != tt.want {
			t.Errorf("%s: Factors = %+v, %v; want %+v", tt.name, got, err, tt.want)
		}
	}
}

// An account with no password or 2FA confirms with a code mailed to it, which
// a session alone can't read.
func TestReauthService_EmailCodes(t *testing.T) {
	smtp, box := testutil.FakeSMTP(t)
	svc, db := newVerificationServices(t, smtp)
	reauth := svc.Reauth
	ctx := context.Background()
	suffix := testutil.UniqueSuffix(t)
	userID := testutil.SeedPasswordlessUser(t, db, suffix, "g_code_"+suffix)
	email := "testnopw_" + suffix + "@test.invalid"
	confirm := func(code string) error {
		_, err := reauth.Confirm(ctx, userID, service.Confirmation{OneTimeCode: code})
		return err
	}

	if f, err := reauth.Factors(ctx, userID); err != nil || f != (service.Factors{Email: true}) {
		t.Fatalf("Factors = %+v, %v; want only Email", f, err)
	}
	if err := confirm("123456"); !errors.Is(err, service.ErrReauthFailed) {
		t.Errorf("before any code was sent: err = %v, want ErrReauthFailed", err)
	}
	if err := reauth.SendEmailCode(ctx, userID); err != nil {
		t.Fatalf("SendEmailCode: %v", err)
	}
	code := box.NextTo(t, email).ConfirmationCode(t)
	if err := reauth.SendEmailCode(ctx, userID); !errors.Is(err, service.ErrEmailCodeCooldown) {
		t.Errorf("a second code within the minute: err = %v, want ErrEmailCodeCooldown", err)
	}
	wrong := "000000"
	if code == wrong {
		wrong = "111111"
	}
	if err := confirm(wrong); !errors.Is(err, service.ErrReauthFailed) {
		t.Errorf("wrong code: err = %v, want ErrReauthFailed", err)
	}
	if err := confirm(code); err != nil {
		t.Fatalf("right code: err = %v", err)
	}
	if err := confirm(code); !errors.Is(err, service.ErrReauthFailed) {
		t.Errorf("the same code again: err = %v, want ErrReauthFailed", err)
	}

	testutil.Exec(t, db, `UPDATE users SET reauth_code_sent_at = NOW() - interval '2 minutes' WHERE id = $1`, userID)
	if err := reauth.SendEmailCode(ctx, userID); err != nil {
		t.Fatalf("SendEmailCode after the cooldown: %v", err)
	}
	code = box.NextTo(t, email).ConfirmationCode(t)
	testutil.Exec(t, db, `UPDATE users SET reauth_code_expires_at = NOW() - interval '1 second' WHERE id = $1`, userID)
	if err := confirm(code); !errors.Is(err, service.ErrReauthFailed) {
		t.Errorf("an expired code: err = %v, want ErrReauthFailed", err)
	}

	passwordUser, _ := testutil.SeedUserWithPassword(t, db, testutil.UniqueSuffix(t), "password1")
	if err := reauth.SendEmailCode(ctx, passwordUser); !errors.Is(err, service.ErrEmailCodeNeedless) {
		t.Errorf("an account with a password: err = %v, want ErrEmailCodeNeedless", err)
	}
}

// Without outgoing email, such an account has nothing to confirm with, so its
// sensitive actions are refused instead of let through.
func TestReauthService_NoWayToConfirmIsRefused(t *testing.T) {
	svc, db := newVerificationServices(t, config.SMTPConfig{})
	ctx := context.Background()
	userID := testutil.SeedPasswordlessUser(t, db, testutil.UniqueSuffix(t), "g_nosmtp_"+testutil.UniqueSuffix(t))

	if _, err := svc.Reauth.Confirm(ctx, userID, service.Confirmation{OneTimeCode: "123456"}); !errors.Is(err, service.ErrReauthUnavailable) {
		t.Errorf("Confirm: err = %v, want ErrReauthUnavailable", err)
	}
	if err := svc.Reauth.SendEmailCode(ctx, userID); !errors.Is(err, service.ErrReauthUnavailable) {
		t.Errorf("SendEmailCode: err = %v, want ErrReauthUnavailable", err)
	}
}

// Someone else who knows the password is signed out by the change, which needs
// the current password so a session alone can't lock the owner out.
func TestUserService_ChangePassword(t *testing.T) {
	smtp, box := testutil.FakeSMTP(t)
	svc, db := newVerificationServices(t, smtp)
	ctx := context.Background()
	suffix := testutil.UniqueSuffix(t)
	userID, email := testutil.SeedUserWithPassword(t, db, suffix, "password1")
	before, err := svc.User.SessionVersion(ctx, userID)
	if err != nil {
		t.Fatal(err)
	}

	for _, tt := range []struct {
		name        string
		c           service.Confirmation
		newPassword string
		want        error
	}{
		{"no current password", service.Confirmation{}, "password2", service.ErrReauthFailed},
		{"wrong current password", service.Confirmation{Password: "guess"}, "password2", service.ErrReauthFailed},
		{"too short", service.Confirmation{Password: "password1"}, "short", service.ErrPasswordTooShort},
		{"too long", service.Confirmation{Password: "password1"}, strings.Repeat("x", 73), service.ErrPasswordTooLong},
	} {
		if _, err := svc.User.ChangePassword(ctx, userID, tt.c, tt.newPassword); !errors.Is(err, tt.want) {
			t.Errorf("%s: err = %v, want %v", tt.name, err, tt.want)
		}
	}
	if _, _, err := svc.User.Authenticate(ctx, email, "password1"); err != nil {
		t.Fatalf("a refused change still replaced the password: %v", err)
	}
	if v, _ := svc.User.SessionVersion(ctx, userID); v != before {
		t.Fatalf("a refused change ended sessions (version %d → %d)", before, v)
	}
	box.Empty(t, 200*time.Millisecond)

	token, err := svc.User.ChangePassword(ctx, userID, service.Confirmation{Password: "password1"}, "password2")
	if err != nil || token == "" {
		t.Fatalf("ChangePassword = %q, %v", token, err)
	}
	if _, _, err := svc.User.Authenticate(ctx, email, "password1"); err == nil {
		t.Error("the old password still signs in")
	}
	if _, _, err := svc.User.Authenticate(ctx, email, "password2"); err != nil {
		t.Errorf("the new password doesn't sign in: %v", err)
	}
	if v, _ := svc.User.SessionVersion(ctx, userID); v != before+1 {
		t.Errorf("session version %d → %d, want +1", before, v)
	}
	if notice := box.NextTo(t, email); !strings.Contains(notice.Data, "password was changed") || !strings.Contains(notice.Data, "testpw_"+suffix) {
		t.Errorf("the notice doesn't say the account's password changed: %.400s", notice.Data)
	}
}

// Two changes racing on the same old password can't both succeed: the later
// one confirmed a password the first has already replaced.
func TestUserService_ChangePassword_RacingChangesCantBothWin(t *testing.T) {
	db := testutil.OpenTestDB(t)
	_, users := newReauth(t)
	ctx := context.Background()
	userID, email := testutil.SeedUserWithPassword(t, db, testutil.UniqueSuffix(t), "password1")

	errs := make([]error, 2)
	var wg sync.WaitGroup
	start := make(chan struct{})
	for i := range errs {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			_, errs[i] = users.ChangePassword(ctx, userID, service.Confirmation{Password: "password1"}, fmt.Sprintf("new-password-%d", i))
		}()
	}
	close(start)
	wg.Wait()
	won := -1
	for i, err := range errs {
		switch {
		case err == nil && won >= 0:
			t.Fatal("both changes succeeded")
		case err == nil:
			won = i
		case !errors.Is(err, service.ErrReauthFailed):
			t.Fatalf("change %d: err = %v", i, err)
		}
	}
	if won < 0 {
		t.Fatal("neither change succeeded")
	}
	if _, _, err := users.Authenticate(ctx, email, fmt.Sprintf("new-password-%d", won)); err != nil {
		t.Errorf("the winning change's password doesn't sign in: %v", err)
	}
}

// Setting a first password would add a way in to a Google-only account, and
// such an account has no password to confirm it with.
func TestUserService_ChangePassword_RefusesPasswordlessAccounts(t *testing.T) {
	db := testutil.OpenTestDB(t)
	_, users := newReauth(t)
	userID := testutil.SeedPasswordlessUser(t, db, testutil.UniqueSuffix(t), "g_setpw_"+testutil.UniqueSuffix(t))

	if _, err := users.ChangePassword(context.Background(), userID, service.Confirmation{}, "password2"); !errors.Is(err, service.ErrReauthNoPassword) {
		t.Errorf("err = %v, want ErrReauthNoPassword", err)
	}
	if n := countUsersWithPassword(t, db, userID); n != 0 {
		t.Error("a passwordless account got a password")
	}
}

// A confirmation checks one password; if another change lands first, the hash
// it confirmed is gone and the change must not go through.
func TestUserStore_ChangePassword_OnlyReplacesTheConfirmedHash(t *testing.T) {
	db := testutil.OpenTestDB(t)
	users := store.NewUserStore(db)
	ctx := context.Background()
	userID, _ := testutil.SeedUserWithPassword(t, db, testutil.UniqueSuffix(t), "password1")
	u, err := users.GetByID(ctx, userID)
	if err != nil {
		t.Fatal(err)
	}

	if changed, err := users.ChangePassword(ctx, userID, "not-the-current-hash", "new-hash"); err != nil || changed {
		t.Fatalf("stale hash: changed = %v, err = %v; want false", changed, err)
	}
	if changed, err := users.ChangePassword(ctx, userID, u.PasswordHash, "new-hash"); err != nil || !changed {
		t.Fatalf("current hash: changed = %v, err = %v; want true", changed, err)
	}
}

func TestTOTPService_MailsANoticeWhen2FAChanges(t *testing.T) {
	smtp, box := testutil.FakeSMTP(t)
	svc, db := newVerificationServices(t, smtp)
	ctx := context.Background()
	userID, email := testutil.SeedUserWithPassword(t, db, testutil.UniqueSuffix(t), "password1")
	secret, _, err := svc.TOTP.Generate("notice", "Cloudzilla")
	if err != nil {
		t.Fatal(err)
	}

	if _, err := svc.TOTP.Enable(ctx, userID, secret, testutil.TOTPCode(t, secret)); err != nil {
		t.Fatalf("Enable: %v", err)
	}
	if notice := box.NextTo(t, email); !strings.Contains(notice.Data, "turned on") {
		t.Errorf("enable notice: %.300s", notice.Data)
	}
	if err := svc.TOTP.Disable(ctx, userID, testutil.TOTPCode(t, secret)); err != nil {
		t.Fatalf("Disable: %v", err)
	}
	if notice := box.NextTo(t, email); !strings.Contains(notice.Data, "turned off") {
		t.Errorf("disable notice: %.300s", notice.Data)
	}
}

func TestUserService_UpdateProfile_EmailChangeNeedsConfirmation(t *testing.T) {
	smtp, box := testutil.FakeSMTP(t)
	svc, db := newVerificationServices(t, smtp)
	ctx := context.Background()
	suffix := testutil.UniqueSuffix(t)
	userID, oldEmail := testutil.SeedUserWithPassword(t, db, suffix, "password1")
	newEmail := "confirmed_" + suffix + "@test.invalid"

	for _, c := range []service.Confirmation{{}, {Password: "guess"}} {
		if err := svc.User.UpdateProfile(ctx, userID, "", newEmail, "", "", "", c); !errors.Is(err, service.ErrReauthFailed) {
			t.Errorf("UpdateProfile(%+v): err = %v, want ErrReauthFailed", c, err)
		}
	}
	if u, _ := svc.User.GetByID(ctx, userID); u.Email != oldEmail {
		t.Fatalf("an unconfirmed change set the email to %q", u.Email)
	}
	box.Empty(t, 200*time.Millisecond)

	if err := svc.User.UpdateProfile(ctx, userID, "Name only", oldEmail, "", "", "", service.Confirmation{}); err != nil {
		t.Errorf("a profile edit that keeps the email needed a confirmation: %v", err)
	}
	if err := svc.User.UpdateProfile(ctx, userID, "", newEmail, "", "", "", service.Confirmation{Password: "password1"}); err != nil {
		t.Fatalf("confirmed UpdateProfile: %v", err)
	}
	notice := box.NextTo(t, oldEmail)
	if !strings.Contains(notice.Data, newEmail) || !strings.Contains(notice.Data, "testpw_"+suffix) {
		t.Errorf("the old address's notice doesn't name the account and the new address: %.400s", notice.Data)
	}
	box.NextTo(t, newEmail)
}

// Accounts with no password have nothing guarding their email, so a stolen
// session could set its own address and pull in its own Google account.
func TestUserService_AuthenticateOAuth_DoesNotLinkPasswordlessAccounts(t *testing.T) {
	db := testutil.OpenTestDB(t)
	svc := newOAuthUserSvc(t)
	suffix := testutil.UniqueSuffix(t)
	userID := testutil.SeedUser(t, db, suffix)
	email := "testuser_" + suffix + "@test.invalid"
	testutil.Exec(t, db, `UPDATE users SET password_hash = '', email_verified_at = NOW() WHERE id = $1`, userID)

	login, err := svc.AuthenticateOAuth(context.Background(), service.OAuthIdentity{
		Provider: "google", ID: "g_nopw_" + suffix, Email: email, EmailVerified: true,
	}, true, true)
	if !errors.Is(err, service.ErrOAuthAccountExists) || login != nil {
		t.Errorf("passwordless account: login = %+v, err = %v; want ErrOAuthAccountExists", login, err)
	}
	links, _ := newOAuthLinkSvc(t)
	if _, err := links.LinkByVerifiedEmail(context.Background(), service.OAuthLink{UserID: userID, Email: email, Provider: "google", ID: "g_nopw_" + suffix}); !errors.Is(err, service.ErrOAuthAccountExists) {
		t.Errorf("LinkByVerifiedEmail on a passwordless account: err = %v, want ErrOAuthAccountExists", err)
	}
	if got := linkedOAuthID(t, db, userID); got != "" {
		t.Errorf("passwordless account linked to %q", got)
	}
}

func TestUserService_RevokeSessions_BumpsTheVersion(t *testing.T) {
	db := testutil.OpenTestDB(t)
	_, users := newReauth(t)
	ctx := context.Background()
	userID := testutil.SeedUser(t, db, testutil.UniqueSuffix(t))

	before, err := users.SessionVersion(ctx, userID)
	if err != nil {
		t.Fatal(err)
	}
	if token, err := users.RevokeSessions(ctx, userID); err != nil || token == "" {
		t.Fatalf("RevokeSessions = %q, %v", token, err)
	}
	if after, _ := users.SessionVersion(ctx, userID); after != before+1 {
		t.Errorf("session version %d → %d, want +1", before, after)
	}
}

func countUsersWithPassword(t *testing.T, db *sql.DB, userID int64) int {
	t.Helper()
	var n int
	if err := db.QueryRowContext(context.Background(), `SELECT COUNT(*) FROM users WHERE id = $1 AND password_hash <> ''`, userID).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

// Codes go out at most five times an hour, so a stolen session can't flood
// the owner's inbox.
func TestReauthService_EmailCodesAreCappedPerHour(t *testing.T) {
	smtp, box := testutil.FakeSMTP(t)
	svc, db := newVerificationServices(t, smtp)
	ctx := context.Background()
	suffix := testutil.UniqueSuffix(t)
	userID := testutil.SeedPasswordlessUser(t, db, suffix, "g_cap_"+suffix)
	nextMinute := func() {
		testutil.Exec(t, db, `UPDATE users SET reauth_code_sent_at = NOW() - interval '2 minutes' WHERE id = $1`, userID)
	}

	for i := range 5 {
		nextMinute()
		if err := svc.Reauth.SendEmailCode(ctx, userID); err != nil {
			t.Fatalf("code %d: %v", i+1, err)
		}
	}
	box.Drain(500 * time.Millisecond)
	nextMinute()
	if err := svc.Reauth.SendEmailCode(ctx, userID); !errors.Is(err, service.ErrEmailCodeCooldown) {
		t.Errorf("a sixth code within the hour: err = %v, want ErrEmailCodeCooldown", err)
	}
	testutil.Exec(t, db, `UPDATE users SET reauth_codes_window_start = NOW() - interval '61 minutes' WHERE id = $1`, userID)
	if err := svc.Reauth.SendEmailCode(ctx, userID); err != nil {
		t.Errorf("after the hour: err = %v", err)
	}
}

// A provider sign-in confirms only the account that started it, once.
func TestReauthService_ProviderSignInIsBoundToItsAccount(t *testing.T) {
	db := testutil.OpenTestDB(t)
	users := store.NewUserStore(db)
	states := store.NewOAuthStateStore(db)
	reauth := service.NewReauthService(users, service.NewTOTPService(users)).WithProviderSignIn(states, nil, true)
	ctx := context.Background()
	suffix := testutil.UniqueSuffix(t)
	userID := testutil.SeedPasswordlessUser(t, db, suffix, "g_bound_"+suffix)
	otherID := testutil.SeedPasswordlessUser(t, db, suffix+"_o", "g_other_"+suffix)
	passwordUser, _ := testutil.SeedUserWithPassword(t, db, testutil.UniqueSuffix(t), "password1")

	if _, _, err := reauth.BeginProviderSignIn(ctx, passwordUser, "google"); !errors.Is(err, service.ErrSignInMismatch) {
		t.Errorf("an account with a password: err = %v, want ErrSignInMismatch", err)
	}
	if _, _, err := reauth.BeginProviderSignIn(ctx, userID, "saml"); !errors.Is(err, service.ErrSignInMismatch) {
		t.Errorf("a provider the account doesn't use: err = %v, want ErrSignInMismatch", err)
	}
	state, _, err := reauth.BeginProviderSignIn(ctx, userID, "google")
	if err != nil {
		t.Fatalf("BeginProviderSignIn: %v", err)
	}
	if _, _, err := reauth.FinishSAMLSignIn(ctx, state, otherID); !errors.Is(err, service.ErrSignInMismatch) {
		t.Errorf("a sign-in as another account: err = %v, want ErrSignInMismatch", err)
	}
	if _, _, err := reauth.FinishGoogleSignIn(ctx, state, "g_bound_"+suffix); !errors.Is(err, service.ErrSignInMismatch) {
		t.Errorf("the right account after a refused try: err = %v, want the spent state refused", err)
	}

	state, _, _ = reauth.BeginProviderSignIn(ctx, userID, "google")
	gotID, code, err := reauth.FinishGoogleSignIn(ctx, state, "g_bound_"+suffix)
	if err != nil || gotID != userID || code == "" {
		t.Fatalf("FinishGoogleSignIn = %d, %q, %v", gotID, code, err)
	}
	if _, err := reauth.Confirm(ctx, userID, service.Confirmation{OneTimeCode: code}); err != nil {
		t.Errorf("Confirm with the sign-in code: %v", err)
	}
	if _, err := reauth.Confirm(ctx, userID, service.Confirmation{OneTimeCode: code}); !errors.Is(err, service.ErrReauthFailed) {
		t.Errorf("the code again: err = %v, want ErrReauthFailed", err)
	}
}

// A SAML sign-in to confirm asks the IdP to authenticate again.
func TestSSOService_SAMLRequestCanForceAuthentication(t *testing.T) {
	svc, db := newVerificationServices(t, config.SMTPConfig{})
	ctx := context.Background()
	prior, err := svc.SSO.GetConfig(ctx, "saml")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if prior == nil {
			testutil.Exec(t, db, `DELETE FROM sso_configs WHERE provider = 'saml'`)
		} else if err := svc.SSO.SetConfig(ctx, "saml", prior.Config, prior.Enabled); err != nil {
			t.Errorf("restore saml config: %v", err)
		}
	})
	if err := svc.SSO.SetConfig(ctx, "saml", map[string]string{
		model.SAMLKeySSOURL: "https://idp.test.invalid/sso", model.SAMLKeyACSURL: "https://cz.test.invalid/acs", model.SAMLKeyEntityID: "cz",
	}, true); err != nil {
		t.Fatal(err)
	}
	request := func(force bool) string {
		raw, err := svc.SSO.SAMLAuthnRequestURL(ctx, "", force)
		if err != nil {
			t.Fatal(err)
		}
		u, _ := url.Parse(raw)
		deflated, err := base64.StdEncoding.DecodeString(u.Query().Get("SAMLRequest"))
		if err != nil {
			t.Fatal(err)
		}
		xml, err := io.ReadAll(flate.NewReader(bytes.NewReader(deflated)))
		if err != nil {
			t.Fatal(err)
		}
		return string(xml)
	}
	if !strings.Contains(request(true), `ForceAuthn="true"`) {
		t.Error("a forced request lacks ForceAuthn")
	}
	if strings.Contains(request(false), "ForceAuthn") {
		t.Error("a sign-in request forces authentication")
	}
}
