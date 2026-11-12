package service_test

// Integration tests for additional email addresses. All tests require TEST_DATABASE_DSN and skip otherwise.

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"

	"github.com/mkappworks-dev/cloudzilla-app/internal/config"
	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/service"
	"github.com/mkappworks-dev/cloudzilla-app/internal/testutil"
)

type extraEnv struct {
	svc *service.Services
	db  *sql.DB
	box *testutil.Mailbox
}

func newExtraEnv(t *testing.T) extraEnv {
	t.Helper()
	smtp, box := testutil.FakeSMTP(t)
	svc, db := newVerificationServices(t, smtp)
	return extraEnv{svc, db, box}
}

// user seeds a user who can confirm with the shared test password.
func (e extraEnv) user(t *testing.T) (id int64, primary string) {
	t.Helper()
	suffix := testutil.UniqueSuffix(t)
	return withPassword(t, e.db, testutil.SeedUser(t, e.db, suffix)), "testuser_" + suffix + "@test.invalid"
}

// add registers address for userID and returns the token in its link.
func (e extraEnv) add(t *testing.T, userID int64, address string) string {
	t.Helper()
	if err := e.svc.EmailVerifier.AddAddress(context.Background(), userID, address, confirmed); err != nil {
		t.Fatalf("AddAddress(%s): %v", address, err)
	}
	return e.box.NextTo(t, address).VerificationToken(t)
}

func (e extraEnv) verify(t *testing.T, token string) {
	t.Helper()
	state, row, _, err := e.svc.EmailVerifier.VerifyAddress(context.Background(), token)
	if err != nil || state != model.EmailVerificationVerified || row == nil {
		t.Fatalf("VerifyAddress = %q, %v, %v; want verified", state, row, err)
	}
}

func (e extraEnv) credited(t *testing.T, address string) string {
	t.Helper()
	got, err := e.svc.User.ResolveCommitAuthors(context.Background(), []string{address})
	if err != nil {
		t.Fatal(err)
	}
	return got[address]
}

func extraAddress(t *testing.T) string {
	return "extra_" + testutil.UniqueSuffix(t) + "@test.invalid"
}

func ageExtra(t *testing.T, db *sql.DB, userID int64) {
	t.Helper()
	testutil.Exec(t, db, `UPDATE user_emails SET created_at = created_at - interval '2 minutes' WHERE user_id = $1`, userID)
}

func TestAdditionalEmail_CreditsCommitsOnlyOnceVerified(t *testing.T) {
	e := newExtraEnv(t)
	userID, _ := e.user(t)
	address := extraAddress(t)

	token := e.add(t, userID, address)
	if got := e.credited(t, address); got != "" {
		t.Fatalf("unverified address credited %q", got)
	}
	if link, _ := e.svc.EmailVerifier.Check(context.Background(), token); link.State != model.EmailVerificationPending || link.Email != address {
		t.Fatalf("Check = %+v, want pending for %s", link, address)
	}
	if got := e.credited(t, address); got != "" {
		t.Fatalf("Check verified the address: credited %q", got)
	}

	e.verify(t, token)
	if got := e.credited(t, address); got == "" {
		t.Fatal("verified address credits nobody")
	}
	if got := e.credited(t, strings.ToUpper(address)); got == "" {
		t.Error("lookup is case-sensitive")
	}

	if state, row, _, err := e.svc.EmailVerifier.VerifyAddress(context.Background(), token); err != nil || state != model.EmailVerificationInvalid || row != nil {
		t.Errorf("second VerifyAddress = %q, %v, %v; want invalid", state, row, err)
	}

	rows, err := e.svc.EmailVerifier.Addresses(context.Background(), userID)
	if err != nil || len(rows) != 1 {
		t.Fatalf("Addresses = %v, %v", rows, err)
	}
	if err := e.svc.EmailVerifier.RemoveAddress(context.Background(), userID, rows[0].ID, confirmed); err != nil {
		t.Fatalf("RemoveAddress: %v", err)
	}
	if got := e.credited(t, address); got != "" {
		t.Errorf("removed address still credits %q", got)
	}
}

func TestAdditionalEmail_AddRefusesAnAddressAlreadyTaken(t *testing.T) {
	e := newExtraEnv(t)
	ctx := context.Background()
	alice, alicePrimary := e.user(t)
	bob, _ := e.user(t)
	aliceExtra := extraAddress(t)
	e.add(t, alice, aliceExtra)

	for name, address := range map[string]string{
		"another user's primary":             alicePrimary,
		"another user's primary, other case": strings.ToUpper(alicePrimary),
		"another user's extra":               aliceExtra,
		"another user's extra, other case":   strings.ToUpper(aliceExtra),
	} {
		if err := e.svc.EmailVerifier.AddAddress(ctx, bob, address, confirmed); !errors.Is(err, service.ErrEmailTaken) {
			t.Errorf("%s: err = %v, want ErrEmailTaken", name, err)
		}
	}
	ageExtra(t, e.db, alice)
	if err := e.svc.EmailVerifier.AddAddress(ctx, alice, aliceExtra, confirmed); !errors.Is(err, service.ErrEmailTaken) {
		t.Errorf("own extra again: err = %v, want ErrEmailTaken", err)
	}
	if err := e.svc.EmailVerifier.AddAddress(ctx, alice, alicePrimary, confirmed); !errors.Is(err, service.ErrEmailTaken) {
		t.Errorf("own primary: err = %v, want ErrEmailTaken", err)
	}
	if err := e.svc.EmailVerifier.AddAddress(ctx, alice, "not an address", confirmed); !errors.Is(err, service.ErrInvalidEmail) {
		t.Errorf("invalid: err = %v, want ErrInvalidEmail", err)
	}
}

func TestAdditionalEmail_ExtraAddressIsRefusedAsAPrimary(t *testing.T) {
	e := newExtraEnv(t)
	ctx := context.Background()
	alice, _ := e.user(t)
	bob, _ := e.user(t)
	address := extraAddress(t)
	e.add(t, alice, address)

	err := e.svc.User.UpdateProfile(ctx, bob, "", strings.ToUpper(address), "", "", "", confirmed)
	if !errors.Is(err, service.ErrEmailTaken) {
		t.Errorf("UpdateProfile to another user's extra: err = %v, want ErrEmailTaken", err)
	}
	if err := e.svc.User.UpdateProfile(ctx, alice, "", address, "", "", "", confirmed); !errors.Is(err, service.ErrEmailTaken) {
		t.Errorf("UpdateProfile to own extra: err = %v, want ErrEmailTaken", err)
	}
	if _, err := e.svc.User.Create(ctx, "newuser_"+testutil.UniqueSuffix(t), address, "password1"); !errors.Is(err, service.ErrEmailTaken) {
		t.Errorf("sign-up with an extra address: err = %v, want ErrEmailTaken", err)
	}
}

func TestAdditionalEmail_TokenOfAnotherAddressOrStaleTokenFails(t *testing.T) {
	e := newExtraEnv(t)
	ctx := context.Background()
	alice, _ := e.user(t)
	bob, _ := e.user(t)
	aliceAddr, bobAddr := extraAddress(t), extraAddress(t)
	aliceToken := e.add(t, alice, aliceAddr)
	bobToken := e.add(t, bob, bobAddr)

	e.verify(t, aliceToken)
	if e.credited(t, bobAddr) != "" {
		t.Error("alice's token verified bob's address")
	}
	if link, _ := e.svc.EmailVerifier.Check(ctx, bobToken); link.State != model.EmailVerificationPending {
		t.Errorf("bob's link = %+v, want still pending", link)
	}

	rows, _ := e.svc.EmailVerifier.Addresses(ctx, bob)
	if err := e.svc.EmailVerifier.RemoveAddress(ctx, bob, rows[0].ID, confirmed); err != nil {
		t.Fatal(err)
	}
	if state, row, _, err := e.svc.EmailVerifier.VerifyAddress(ctx, bobToken); err != nil || state != model.EmailVerificationInvalid || row != nil {
		t.Errorf("link of a removed address = %q, %v, %v; want invalid", state, row, err)
	}
	if state, _, _, err := e.svc.EmailVerifier.VerifyAddress(ctx, "short"); err != nil || state != model.EmailVerificationInvalid {
		t.Errorf("malformed token = %q, %v; want invalid", state, err)
	}

	ageExtra(t, e.db, alice)
	carolToken := e.add(t, alice, extraAddress(t))
	testutil.Exec(t, e.db, `UPDATE user_email_tokens SET expires_at = NOW() - interval '1 minute'`+
		` WHERE token_hash = (SELECT token_hash FROM user_email_tokens ORDER BY id DESC LIMIT 1)`)
	if state, row, _, err := e.svc.EmailVerifier.VerifyAddress(ctx, carolToken); err != nil || state != model.EmailVerificationExpired || row != nil {
		t.Errorf("expired link = %q, %v, %v; want expired", state, row, err)
	}
}

func TestAdditionalEmail_ReauthRequiredToAddAndRemove(t *testing.T) {
	e := newExtraEnv(t)
	ctx := context.Background()
	userID, _ := e.user(t)
	address := extraAddress(t)

	if err := e.svc.EmailVerifier.AddAddress(ctx, userID, address, service.Confirmation{Password: "wrong"}); !errors.Is(err, service.ErrReauthFailed) {
		t.Fatalf("add with a wrong password: err = %v, want ErrReauthFailed", err)
	}
	if rows, _ := e.svc.EmailVerifier.Addresses(ctx, userID); len(rows) != 0 {
		t.Fatalf("refused add stored %v", rows)
	}
	e.box.Empty(t, 200*1000*1000)

	e.add(t, userID, address)
	rows, _ := e.svc.EmailVerifier.Addresses(ctx, userID)
	if err := e.svc.EmailVerifier.RemoveAddress(ctx, userID, rows[0].ID, service.Confirmation{}); !errors.Is(err, service.ErrReauthFailed) {
		t.Fatalf("remove without a password: err = %v, want ErrReauthFailed", err)
	}
	if rows, _ := e.svc.EmailVerifier.Addresses(ctx, userID); len(rows) != 1 {
		t.Fatalf("refused remove dropped the row: %v", rows)
	}
}

func TestAdditionalEmail_RemoveIsScopedToTheOwner(t *testing.T) {
	e := newExtraEnv(t)
	ctx := context.Background()
	alice, _ := e.user(t)
	bob, _ := e.user(t)
	e.add(t, alice, extraAddress(t))
	rows, _ := e.svc.EmailVerifier.Addresses(ctx, alice)

	if err := e.svc.EmailVerifier.RemoveAddress(ctx, bob, rows[0].ID, confirmed); !errors.Is(err, service.ErrNoSuchAdditionalEmail) {
		t.Errorf("bob removing alice's address: err = %v, want ErrNoSuchAdditionalEmail", err)
	}
	if rows, _ := e.svc.EmailVerifier.Addresses(ctx, alice); len(rows) != 1 {
		t.Errorf("alice's address was removed: %v", rows)
	}
}

func TestAdditionalEmail_WithoutSMTPNothingIsStored(t *testing.T) {
	svc, db := newVerificationServices(t, config.SMTPConfig{})
	userID := withPassword(t, db, testutil.SeedUser(t, db, testutil.UniqueSuffix(t)))

	if err := svc.EmailVerifier.AddAddress(context.Background(), userID, extraAddress(t), confirmed); !errors.Is(err, service.ErrEmailVerificationUnavailable) {
		t.Fatalf("err = %v, want ErrEmailVerificationUnavailable", err)
	}
	if rows, _ := svc.EmailVerifier.Addresses(context.Background(), userID); len(rows) != 0 {
		t.Errorf("stored %v without a way to verify it", rows)
	}
}

func TestAdditionalEmail_CapAndCooldown(t *testing.T) {
	e := newExtraEnv(t)
	ctx := context.Background()
	userID, _ := e.user(t)
	e.add(t, userID, extraAddress(t))

	if err := e.svc.EmailVerifier.AddAddress(ctx, userID, extraAddress(t), confirmed); !errors.Is(err, service.ErrVerificationCooldown) {
		t.Fatalf("second add within the minute: err = %v, want ErrVerificationCooldown", err)
	}
	for i := 1; i < service.MaxAdditionalEmails; i++ {
		ageExtra(t, e.db, userID)
		e.add(t, userID, extraAddress(t))
	}
	ageExtra(t, e.db, userID)
	if err := e.svc.EmailVerifier.AddAddress(ctx, userID, extraAddress(t), confirmed); !errors.Is(err, service.ErrTooManyEmails) {
		t.Errorf("add past the cap: err = %v, want ErrTooManyEmails", err)
	}
}

func TestAdditionalEmail_StaleUnverifiedClaimDoesNotHoldTheAddress(t *testing.T) {
	e := newExtraEnv(t)
	ctx := context.Background()
	squatter, _ := e.user(t)
	owner, _ := e.user(t)
	address := extraAddress(t)
	e.add(t, squatter, address)
	if err := e.svc.EmailVerifier.AddAddress(ctx, owner, address, confirmed); !errors.Is(err, service.ErrEmailTaken) {
		t.Fatalf("fresh claim: err = %v, want ErrEmailTaken", err)
	}

	testutil.Exec(t, e.db, `UPDATE user_emails SET created_at = NOW() - interval '25 hours' WHERE user_id = $1`, squatter)
	e.add(t, owner, address)
	if rows, _ := e.svc.EmailVerifier.Addresses(ctx, squatter); len(rows) != 0 {
		t.Errorf("stale claim survived: %v", rows)
	}

	verified := extraAddress(t)
	e.verify(t, e.add(t, squatter, verified))
	testutil.Exec(t, e.db, `UPDATE user_emails SET created_at = NOW() - interval '25 hours' WHERE user_id = $1`, squatter)
	ageExtra(t, e.db, owner)
	if err := e.svc.EmailVerifier.AddAddress(ctx, owner, verified, confirmed); !errors.Is(err, service.ErrEmailTaken) {
		t.Errorf("old verified claim: err = %v, want ErrEmailTaken", err)
	}
}
