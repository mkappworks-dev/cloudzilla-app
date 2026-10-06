package service_test

// Integration tests for PasswordResetService. All tests require TEST_DATABASE_DSN and skip otherwise.

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/mkappworks-dev/cloudzilla-app/internal/config"
	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/service"
	"github.com/mkappworks-dev/cloudzilla-app/internal/store"
	"github.com/mkappworks-dev/cloudzilla-app/internal/testutil"
	"golang.org/x/crypto/bcrypt"
)

const newPassword = "new-password-1"

// requestReset asks for a reset of email and returns the token in the emailed link.
func requestReset(t *testing.T, svc *service.Services, box *testutil.Mailbox, email string) string {
	t.Helper()
	if err := svc.PasswordReset.Request(context.Background(), email); err != nil {
		t.Fatalf("Request: %v", err)
	}
	return box.NextTo(t, email).PasswordResetToken(t)
}

func passwordIs(t *testing.T, db *sql.DB, userID int64, password string) bool {
	t.Helper()
	var hash string
	if err := db.QueryRowContext(context.Background(), `SELECT password_hash FROM users WHERE id = $1`, userID).Scan(&hash); err != nil {
		t.Fatal(err)
	}
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) == nil
}

func sessionVersion(t *testing.T, db *sql.DB, userID int64) int {
	t.Helper()
	var v int
	if err := db.QueryRowContext(context.Background(), `SELECT session_version FROM users WHERE id = $1`, userID).Scan(&v); err != nil {
		t.Fatal(err)
	}
	return v
}

func linkState(t *testing.T, svc *service.Services, token string) model.PasswordResetState {
	t.Helper()
	link, err := svc.PasswordReset.Check(context.Background(), token)
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	return link.State
}

func TestPasswordReset_EmailedLinkResetsPassword(t *testing.T) {
	smtp, box := testutil.FakeSMTP(t)
	svc, db := newVerificationServices(t, smtp)
	ctx := context.Background()
	userID, email := testutil.SeedUserWithPassword(t, db, testutil.UniqueSuffix(t), testPassword)
	before := sessionVersion(t, db, userID)

	token := requestReset(t, svc, box, email)

	link, err := svc.PasswordReset.Check(ctx, token)
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if link.State != model.PasswordResetPending || link.UserID != userID || link.Email != email || link.IssuedBy != model.PasswordResetByEmail {
		t.Fatalf("Check = %+v, want a pending emailed link for user %d", link, userID)
	}
	if !passwordIs(t, db, userID, testPassword) {
		t.Fatal("Check changed the password")
	}

	u, issuedBy, err := svc.PasswordReset.Reset(ctx, token, newPassword, "")
	if err != nil {
		t.Fatalf("Reset: %v", err)
	}
	if u.ID != userID || issuedBy != model.PasswordResetByEmail {
		t.Fatalf("Reset = user %d issued by %q, want user %d by email", u.ID, issuedBy, userID)
	}
	if !passwordIs(t, db, userID, newPassword) {
		t.Error("password not changed")
	}
	if got := sessionVersion(t, db, userID); got != before+1 {
		t.Errorf("session_version = %d, want %d", got, before+1)
	}
	if !emailVerified(t, db, userID) {
		t.Error("a completed reset left the address unverified")
	}
	notice := box.NextTo(t, email)
	if !strings.Contains(notice.Data, "password was reset") || !strings.Contains(notice.Data, "SSH keys") {
		t.Errorf("notice = %.500s, want the reset notice naming what still works", notice.Data)
	}

	if _, _, err := svc.PasswordReset.Reset(ctx, token, "another-password", ""); !errors.Is(err, service.ErrPasswordResetInvalid) {
		t.Errorf("second Reset err = %v, want ErrPasswordResetInvalid", err)
	}
	if !passwordIs(t, db, userID, newPassword) {
		t.Error("a spent link changed the password again")
	}
}

func TestPasswordReset_KeepsEarlierVerification(t *testing.T) {
	smtp, box := testutil.FakeSMTP(t)
	svc, db := newVerificationServices(t, smtp)
	userID, email := testutil.SeedUserWithPassword(t, db, testutil.UniqueSuffix(t), testPassword)
	verifiedAt := time.Date(2025, 1, 2, 3, 4, 5, 0, time.UTC)
	testutil.Exec(t, db, `UPDATE users SET email_verified_at = $2 WHERE id = $1`, userID, verifiedAt)

	token := requestReset(t, svc, box, email)
	if _, _, err := svc.PasswordReset.Reset(context.Background(), token, newPassword, ""); err != nil {
		t.Fatalf("Reset: %v", err)
	}
	var got time.Time
	if err := db.QueryRow(`SELECT email_verified_at FROM users WHERE id = $1`, userID).Scan(&got); err != nil {
		t.Fatal(err)
	}
	if !got.Equal(verifiedAt) {
		t.Errorf("email_verified_at = %v, want it kept at %v", got, verifiedAt)
	}
}

func TestPasswordReset_UnknownAddressGetsNothing(t *testing.T) {
	smtp, box := testutil.FakeSMTP(t)
	svc, _ := newVerificationServices(t, smtp)
	if err := svc.PasswordReset.Request(context.Background(), "nobody_"+testutil.UniqueSuffix(t)+"@test.invalid"); err != nil {
		t.Fatalf("Request: %v", err)
	}
	box.Empty(t, 300*time.Millisecond)
}

func TestPasswordReset_PasswordlessAccountGetsNoteNotLink(t *testing.T) {
	smtp, box := testutil.FakeSMTP(t)
	svc, db := newVerificationServices(t, smtp)
	suffix := testutil.UniqueSuffix(t)
	userID := testutil.SeedPasswordlessUser(t, db, suffix, "g-"+suffix)
	email := "testnopw_" + suffix + "@test.invalid"

	if err := svc.PasswordReset.Request(context.Background(), email); err != nil {
		t.Fatalf("Request: %v", err)
	}
	note := box.NextTo(t, email)
	if !strings.Contains(note.Data, "signs in with Google") || strings.Contains(note.Data, "/auth/password/reset/") {
		t.Errorf("note = %.500s, want the Google sign-in note and no link", note.Data)
	}

	// The note counts against the cooldown too.
	if err := svc.PasswordReset.Request(context.Background(), email); err != nil {
		t.Fatalf("second Request: %v", err)
	}
	box.Empty(t, 300*time.Millisecond)

	if _, err := svc.PasswordReset.IssueLink(context.Background(), userID, model.PasswordResetByCLI); !errors.Is(err, service.ErrPasswordResetNoPassword) {
		t.Errorf("IssueLink err = %v, want ErrPasswordResetNoPassword", err)
	}
}

func TestPasswordReset_UnverifiedAddressGetsLink(t *testing.T) {
	smtp, box := testutil.FakeSMTP(t)
	svc, db := newVerificationServices(t, smtp)
	userID, email := testutil.SeedUserWithPassword(t, db, testutil.UniqueSuffix(t), testPassword)
	if emailVerified(t, db, userID) {
		t.Fatal("seeded user is already verified")
	}
	requestReset(t, svc, box, email)
}

func TestPasswordReset_CooldownLimitsEmails(t *testing.T) {
	smtp, box := testutil.FakeSMTP(t)
	svc, db := newVerificationServices(t, smtp)
	ctx := context.Background()
	userID, email := testutil.SeedUserWithPassword(t, db, testutil.UniqueSuffix(t), testPassword)

	first := requestReset(t, svc, box, email)
	var wg sync.WaitGroup
	for range 5 {
		wg.Go(func() {
			if err := svc.PasswordReset.Request(ctx, email); err != nil {
				t.Errorf("Request: %v", err)
			}
		})
	}
	wg.Wait()
	box.Empty(t, 300*time.Millisecond)
	if got := linkState(t, svc, first); got != model.PasswordResetPending {
		t.Fatalf("a request inside the cooldown changed the first link to %s", got)
	}

	testutil.Exec(t, db, `UPDATE password_reset_tokens SET emailed_at = NOW() - INTERVAL '6 minutes' WHERE user_id = $1`, userID)
	second := requestReset(t, svc, box, email)
	if got := linkState(t, svc, first); got != model.PasswordResetInvalid {
		t.Errorf("replaced link state = %s, want invalid", got)
	}
	if got := linkState(t, svc, second); got != model.PasswordResetPending {
		t.Errorf("new link state = %s, want pending", got)
	}
}

func TestPasswordReset_BadPasswordSpendsNothing(t *testing.T) {
	smtp, box := testutil.FakeSMTP(t)
	svc, db := newVerificationServices(t, smtp)
	_, email := testutil.SeedUserWithPassword(t, db, testutil.UniqueSuffix(t), testPassword)
	token := requestReset(t, svc, box, email)

	for _, tc := range []struct {
		password string
		want     error
	}{
		{"short", service.ErrPasswordTooShort},
		{strings.Repeat("x", service.MaxPasswordBytes+1), service.ErrPasswordTooLong},
	} {
		if _, _, err := svc.PasswordReset.Reset(context.Background(), token, tc.password, ""); !errors.Is(err, tc.want) {
			t.Errorf("Reset(%d bytes) err = %v, want %v", len(tc.password), err, tc.want)
		}
	}
	if got := linkState(t, svc, token); got != model.PasswordResetPending {
		t.Errorf("link state = %s after rejected passwords, want pending", got)
	}
}

func TestPasswordReset_TwoFactorAccountNeedsCode(t *testing.T) {
	smtp, box := testutil.FakeSMTP(t)
	svc, db := newVerificationServices(t, smtp)
	ctx := context.Background()
	userID, email := testutil.SeedUserWithPassword(t, db, testutil.UniqueSuffix(t), testPassword)
	testutil.EnableTOTP(t, db, userID)
	token := requestReset(t, svc, box, email)

	link, err := svc.PasswordReset.Check(ctx, token)
	if err != nil || !link.TOTPEnabled {
		t.Fatalf("Check = %+v, %v; want TOTPEnabled", link, err)
	}
	for _, code := range []string{"", "000000"} {
		if _, _, err := svc.PasswordReset.Reset(ctx, token, newPassword, code); !errors.Is(err, service.ErrReauthFailed) {
			t.Errorf("Reset with code %q err = %v, want ErrReauthFailed", code, err)
		}
	}
	if got := linkState(t, svc, token); got != model.PasswordResetPending {
		t.Fatalf("link state = %s after wrong codes, want pending", got)
	}
	if passwordIs(t, db, userID, newPassword) {
		t.Fatal("password changed without a valid code")
	}

	if _, _, err := svc.PasswordReset.Reset(ctx, token, newPassword, testutil.TOTPCode(t, testutil.TestTOTPSecret)); err != nil {
		t.Fatalf("Reset with TOTP code: %v", err)
	}
	if !passwordIs(t, db, userID, newPassword) {
		t.Error("password not changed")
	}
	var totpOn bool
	if err := db.QueryRow(`SELECT totp_enabled FROM users WHERE id = $1`, userID).Scan(&totpOn); err != nil || !totpOn {
		t.Errorf("totp_enabled = %v, %v after reset; want it kept on", totpOn, err)
	}
}

func TestPasswordReset_WrongCodesCountAgainstReauthLimit(t *testing.T) {
	smtp, box := testutil.FakeSMTP(t)
	svc, db := newVerificationServices(t, smtp)
	ctx := context.Background()
	userID, email := testutil.SeedUserWithPassword(t, db, testutil.UniqueSuffix(t), testPassword)
	testutil.EnableTOTP(t, db, userID)
	token := requestReset(t, svc, box, email)

	var err error
	for range 10 {
		if _, _, err = svc.PasswordReset.Reset(ctx, token, newPassword, "000000"); errors.Is(err, service.ErrReauthThrottled) {
			break
		}
	}
	if !errors.Is(err, service.ErrReauthThrottled) {
		t.Fatalf("Reset err = %v after repeated wrong codes, want ErrReauthThrottled", err)
	}
	if _, _, err := svc.PasswordReset.Reset(ctx, token, newPassword, testutil.TOTPCode(t, testutil.TestTOTPSecret)); !errors.Is(err, service.ErrReauthThrottled) {
		t.Errorf("Reset with the right code while throttled err = %v, want ErrReauthThrottled", err)
	}
}

func TestPasswordReset_ExpiredLink(t *testing.T) {
	smtp, box := testutil.FakeSMTP(t)
	svc, db := newVerificationServices(t, smtp)
	userID, email := testutil.SeedUserWithPassword(t, db, testutil.UniqueSuffix(t), testPassword)
	token := requestReset(t, svc, box, email)

	var ttl time.Duration
	var secs float64
	if err := db.QueryRow(`SELECT EXTRACT(EPOCH FROM expires_at - created_at) FROM password_reset_tokens WHERE user_id = $1`, userID).Scan(&secs); err != nil {
		t.Fatal(err)
	}
	ttl = time.Duration(secs) * time.Second
	if ttl != service.PasswordResetEmailTTL {
		t.Errorf("emailed link lasts %v, want %v", ttl, service.PasswordResetEmailTTL)
	}

	testutil.Exec(t, db, `UPDATE password_reset_tokens SET expires_at = NOW() - INTERVAL '1 second' WHERE user_id = $1`, userID)
	if got := linkState(t, svc, token); got != model.PasswordResetExpired {
		t.Errorf("Check state = %s, want expired", got)
	}
	if _, _, err := svc.PasswordReset.Reset(context.Background(), token, newPassword, ""); !errors.Is(err, service.ErrPasswordResetExpired) {
		t.Errorf("Reset err = %v, want ErrPasswordResetExpired", err)
	}
}

func TestPasswordReset_AccountChangesRevokeLink(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change string
	}{
		{"password change or sign-out everywhere", `UPDATE users SET session_version = session_version + 1 WHERE id = $1`},
		{"email change", `UPDATE users SET email = 'changed_' || email WHERE id = $1`},
		{"password removed", `UPDATE users SET password_hash = '' WHERE id = $1`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			smtp, box := testutil.FakeSMTP(t)
			svc, db := newVerificationServices(t, smtp)
			userID, email := testutil.SeedUserWithPassword(t, db, testutil.UniqueSuffix(t), testPassword)
			token := requestReset(t, svc, box, email)

			testutil.Exec(t, db, tc.change, userID)
			if got := linkState(t, svc, token); got != model.PasswordResetInvalid {
				t.Errorf("Check state = %s, want invalid", got)
			}
			if _, _, err := svc.PasswordReset.Reset(context.Background(), token, newPassword, ""); !errors.Is(err, service.ErrPasswordResetInvalid) {
				t.Errorf("Reset err = %v, want ErrPasswordResetInvalid", err)
			}
		})
	}
}

func TestPasswordReset_MalformedTokenIsInvalid(t *testing.T) {
	svc, _ := newVerificationServices(t, config.SMTPConfig{})
	for _, token := range []string{"", "short", strings.Repeat("!", 43)} {
		if got := linkState(t, svc, token); got != model.PasswordResetInvalid {
			t.Errorf("Check(%q) = %s, want invalid", token, got)
		}
	}
}

func TestPasswordReset_IssueLinkWithoutSMTP(t *testing.T) {
	svc, db := newVerificationServices(t, config.SMTPConfig{})
	ctx := context.Background()
	userID, email := testutil.SeedUserWithPassword(t, db, testutil.UniqueSuffix(t), testPassword)

	if err := svc.PasswordReset.Request(ctx, email); !errors.Is(err, service.ErrPasswordResetUnavailable) {
		t.Errorf("Request without SMTP err = %v, want ErrPasswordResetUnavailable", err)
	}
	if _, err := svc.PasswordReset.IssueLink(ctx, userID, model.PasswordResetByEmail); err == nil {
		t.Error("IssueLink accepted the email issuer")
	}

	first, err := svc.PasswordReset.IssueLink(ctx, userID, model.PasswordResetByCLI)
	if err != nil {
		t.Fatalf("IssueLink: %v", err)
	}
	second, err := svc.PasswordReset.IssueLink(ctx, userID, model.PasswordResetByCLI)
	if err != nil {
		t.Fatalf("second IssueLink: %v", err)
	}
	prefix := verifyBaseURL + "/auth/password/reset/"
	if !strings.HasPrefix(second, prefix) {
		t.Fatalf("link = %q, want it under %s", second, prefix)
	}
	if got := linkState(t, svc, strings.TrimPrefix(first, prefix)); got != model.PasswordResetInvalid {
		t.Errorf("replaced link state = %s, want invalid", got)
	}

	var secs float64
	if err := db.QueryRow(`SELECT EXTRACT(EPOCH FROM expires_at - created_at) FROM password_reset_tokens WHERE user_id = $1`, userID).Scan(&secs); err != nil {
		t.Fatal(err)
	}
	if got := time.Duration(secs) * time.Second; got != service.PasswordResetManualTTL {
		t.Errorf("CLI link lasts %v, want %v", got, service.PasswordResetManualTTL)
	}

	_, issuedBy, err := svc.PasswordReset.Reset(ctx, strings.TrimPrefix(second, prefix), newPassword, "")
	if err != nil {
		t.Fatalf("Reset: %v", err)
	}
	if issuedBy != model.PasswordResetByCLI {
		t.Errorf("issuedBy = %q, want cli", issuedBy)
	}
}

func TestPasswordReset_ConcurrentResetsSpendOnce(t *testing.T) {
	svc, db := newVerificationServices(t, config.SMTPConfig{})
	userID, _ := testutil.SeedUserWithPassword(t, db, testutil.UniqueSuffix(t), testPassword)
	link, err := svc.PasswordReset.IssueLink(context.Background(), userID, model.PasswordResetByAdmin)
	if err != nil {
		t.Fatal(err)
	}
	token := link[strings.LastIndex(link, "/")+1:]
	before := sessionVersion(t, db, userID)

	var wg sync.WaitGroup
	var mu sync.Mutex
	succeeded := 0
	for i := range 8 {
		wg.Go(func() {
			_, _, err := svc.PasswordReset.Reset(context.Background(), token, newPassword+string(rune('a'+i)), "")
			if err == nil {
				mu.Lock()
				succeeded++
				mu.Unlock()
			} else if !errors.Is(err, service.ErrPasswordResetInvalid) {
				t.Errorf("Reset err = %v", err)
			}
		})
	}
	wg.Wait()
	if succeeded != 1 {
		t.Errorf("%d resets succeeded, want 1", succeeded)
	}
	if got := sessionVersion(t, db, userID); got != before+1 {
		t.Errorf("session_version = %d, want %d", got, before+1)
	}
}

func TestPasswordReset_ManualLinkDoesNotVerifyAddress(t *testing.T) {
	svc, db := newVerificationServices(t, config.SMTPConfig{})
	userID, _ := testutil.SeedUserWithPassword(t, db, testutil.UniqueSuffix(t), testPassword)
	link, err := svc.PasswordReset.IssueLink(context.Background(), userID, model.PasswordResetByAdmin)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := svc.PasswordReset.Reset(context.Background(), link[strings.LastIndex(link, "/")+1:], newPassword, ""); err != nil {
		t.Fatalf("Reset: %v", err)
	}
	if emailVerified(t, db, userID) {
		t.Error("a handed-over link verified an address nobody proved they own")
	}
}

func TestPasswordReset_ManualLinkAndEmailCooldown(t *testing.T) {
	smtp, box := testutil.FakeSMTP(t)
	svc, db := newVerificationServices(t, smtp)
	ctx := context.Background()
	userID, email := testutil.SeedUserWithPassword(t, db, testutil.UniqueSuffix(t), testPassword)

	emailed := requestReset(t, svc, box, email)
	link, err := svc.PasswordReset.IssueLink(ctx, userID, model.PasswordResetByCLI)
	if err != nil {
		t.Fatalf("IssueLink inside the email cooldown: %v", err)
	}
	if got := linkState(t, svc, emailed); got != model.PasswordResetInvalid {
		t.Errorf("emailed link after IssueLink = %s, want invalid", got)
	}
	manual := link[strings.LastIndex(link, "/")+1:]

	// Past the cooldown, the forgot form still can't cancel a live manual link.
	testutil.Exec(t, db, `UPDATE password_reset_tokens SET emailed_at = NOW() - INTERVAL '6 minutes' WHERE user_id = $1`, userID)
	if err := svc.PasswordReset.Request(ctx, email); err != nil {
		t.Fatalf("Request: %v", err)
	}
	box.Empty(t, 300*time.Millisecond)
	if got := linkState(t, svc, manual); got != model.PasswordResetPending {
		t.Fatalf("manual link after a forgot request = %s, want pending", got)
	}

	// A manual link doesn't reset the cooldown of the last email.
	testutil.Exec(t, db, `UPDATE password_reset_tokens SET emailed_at = NOW() WHERE user_id = $1`, userID)
	if _, _, err := svc.PasswordReset.Reset(ctx, manual, newPassword, ""); err != nil {
		t.Fatalf("Reset: %v", err)
	}
	if err := svc.PasswordReset.Request(ctx, email); err != nil {
		t.Fatalf("Request: %v", err)
	}
	box.NextTo(t, email) // the reset notice
	box.Empty(t, 300*time.Millisecond)
}

func TestPasswordReset_SuspendedAccountGetsNothing(t *testing.T) {
	for _, tc := range []struct {
		name string
		seed func(t *testing.T, db *sql.DB) (int64, string)
	}{
		{"with a password", func(t *testing.T, db *sql.DB) (int64, string) {
			return testutil.SeedUserWithPassword(t, db, testutil.UniqueSuffix(t), testPassword)
		}},
		{"passwordless", func(t *testing.T, db *sql.DB) (int64, string) {
			suffix := testutil.UniqueSuffix(t)
			id := testutil.SeedPasswordlessUser(t, db, suffix, "g-"+suffix)
			var email string
			if err := db.QueryRow(`SELECT email FROM users WHERE id = $1`, id).Scan(&email); err != nil {
				t.Fatal(err)
			}
			return id, email
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			smtp, box := testutil.FakeSMTP(t)
			svc, db := newVerificationServices(t, smtp)
			userID, email := tc.seed(t, db)
			testutil.Exec(t, db, `UPDATE users SET suspended_at = NOW() WHERE id = $1`, userID)

			if err := svc.PasswordReset.Request(context.Background(), email); err != nil {
				t.Fatalf("Request: %v", err)
			}
			box.Empty(t, 300*time.Millisecond)
		})
	}
}

func TestPasswordReset_SuspensionRevokesLink(t *testing.T) {
	svc, db := newVerificationServices(t, config.SMTPConfig{})
	ctx := context.Background()
	userID, _ := testutil.SeedUserWithPassword(t, db, testutil.UniqueSuffix(t), testPassword)
	link, err := svc.PasswordReset.IssueLink(ctx, userID, model.PasswordResetByAdmin)
	if err != nil {
		t.Fatal(err)
	}
	token := link[strings.LastIndex(link, "/")+1:]

	// Set directly so the session_version bump that UserStore.Suspend makes can't be what revokes it.
	testutil.Exec(t, db, `UPDATE users SET suspended_at = NOW() WHERE id = $1`, userID)
	if got := linkState(t, svc, token); got != model.PasswordResetInvalid {
		t.Errorf("Check state while suspended = %s, want invalid", got)
	}
	if _, _, err := svc.PasswordReset.Reset(ctx, token, newPassword, ""); !errors.Is(err, service.ErrPasswordResetInvalid) {
		t.Errorf("Reset while suspended err = %v, want ErrPasswordResetInvalid", err)
	}
	if !passwordIs(t, db, userID, testPassword) {
		t.Error("a suspended account's password changed")
	}

	// A real suspension revokes the link for good, so unsuspending doesn't bring it back.
	testutil.Exec(t, db, `UPDATE users SET suspended_at = NULL WHERE id = $1`, userID)
	if got := linkState(t, svc, token); got != model.PasswordResetPending {
		t.Fatalf("Check state after clearing suspended_at = %s, want pending", got)
	}
	users := store.NewUserStore(db)
	if _, err := users.Suspend(ctx, userID); err != nil {
		t.Fatalf("Suspend: %v", err)
	}
	if _, err := users.Unsuspend(ctx, userID); err != nil {
		t.Fatalf("Unsuspend: %v", err)
	}
	if got := linkState(t, svc, token); got != model.PasswordResetInvalid {
		t.Errorf("Check state after suspend and unsuspend = %s, want invalid", got)
	}
}

func TestPasswordReset_IssueLinkRefusesSuspendedAccount(t *testing.T) {
	svc, db := newVerificationServices(t, config.SMTPConfig{})
	userID, _ := testutil.SeedUserWithPassword(t, db, testutil.UniqueSuffix(t), testPassword)
	testutil.Exec(t, db, `UPDATE users SET suspended_at = NOW() WHERE id = $1`, userID)

	for _, by := range []string{model.PasswordResetByAdmin, model.PasswordResetByCLI} {
		if _, err := svc.PasswordReset.IssueLink(context.Background(), userID, by); !errors.Is(err, service.ErrUserSuspended) {
			t.Errorf("IssueLink by %s err = %v, want ErrUserSuspended", by, err)
		}
	}
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM password_reset_tokens WHERE user_id = $1`, userID).Scan(&n); err != nil || n != 0 {
		t.Errorf("password_reset_tokens rows = %d, %v; want none", n, err)
	}
}
