package service_test

// Integration tests for EmailVerificationService. All tests require TEST_DATABASE_DSN and skip otherwise.

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"net"
	"regexp"
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

const verifyBaseURL = "http://cz.test"

func newVerificationServices(t *testing.T, smtp config.SMTPConfig) (*service.Services, *sql.DB) {
	t.Helper()
	db := testutil.OpenTestDB(t)
	cfg := &config.Config{
		Server: config.ServerConfig{BaseURL: verifyBaseURL + "/"},
		Auth:   config.AuthConfig{JWTSecret: "test-secret-32bytes-minimum-len!", JWTExpiry: time.Hour},
		Git:    config.GitConfig{ReposRoot: t.TempDir()},
		SMTP:   smtp,
	}
	return service.New(store.New(db), cfg), db
}

func emailVerified(t *testing.T, db *sql.DB, userID int64) bool {
	t.Helper()
	var verified bool
	if err := db.QueryRowContext(context.Background(), `SELECT email_verified_at IS NOT NULL FROM users WHERE id = $1`, userID).Scan(&verified); err != nil {
		t.Fatalf("read email_verified_at: %v", err)
	}
	return verified
}

func tokenRows(t *testing.T, db *sql.DB, userID int64) int {
	t.Helper()
	var n int
	if err := db.QueryRowContext(context.Background(), `SELECT COUNT(*) FROM email_verification_tokens WHERE user_id = $1`, userID).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

// sendLink sends userID a verification email and returns the token in its link.
func sendLink(t *testing.T, svc *service.Services, box *testutil.Mailbox, userID int64) string {
	t.Helper()
	if err := svc.EmailVerifier.Send(context.Background(), userID); err != nil {
		t.Fatalf("Send: %v", err)
	}
	return box.Next(t).VerificationToken(t)
}

const testPassword = "password1"

var confirmed = service.Confirmation{Password: testPassword}

// withPassword gives a seeded user a real password, so its email changes can be confirmed.
func withPassword(t *testing.T, db *sql.DB, userID int64) int64 {
	t.Helper()
	hash, err := bcrypt.GenerateFromPassword([]byte(testPassword), bcrypt.MinCost)
	if err != nil {
		t.Fatal(err)
	}
	testutil.Exec(t, db, `UPDATE users SET password_hash = $2 WHERE id = $1`, userID, string(hash))
	return userID
}

// Lets the user's next Send past the cooldown without sleeping through it.
func ageTokens(t *testing.T, db *sql.DB, userID int64) {
	t.Helper()
	testutil.Exec(t, db, `UPDATE email_verification_tokens SET created_at = created_at - interval '2 minutes' WHERE user_id = $1`, userID)
}

func TestEmailVerification_LinkVerifiesTheAddressOnce(t *testing.T) {
	smtp, box := testutil.FakeSMTP(t)
	svc, db := newVerificationServices(t, smtp)
	ctx := context.Background()
	suffix := testutil.UniqueSuffix(t)
	userID := testutil.SeedUser(t, db, suffix)
	username, email := "testuser_"+suffix, "testuser_"+suffix+"@test.invalid"

	if err := svc.EmailVerifier.Send(ctx, userID); err != nil {
		t.Fatalf("Send: %v", err)
	}
	mail := box.Next(t)
	if len(mail.To) != 1 || mail.To[0] != email {
		t.Errorf("email sent to %v, want %s", mail.To, email)
	}
	if !strings.Contains(mail.Data, verifyBaseURL+"/verify-email?token=") {
		t.Errorf("link does not use the configured base URL: %.400s", mail.Data)
	}
	if !strings.Contains(mail.Data, username) {
		t.Errorf("email does not name the account %s: %.400s", username, mail.Data)
	}
	token := mail.VerificationToken(t)

	link, err := svc.EmailVerifier.Check(ctx, token)
	if err != nil || link != (model.EmailVerificationLink{State: model.EmailVerificationPending, Username: username, Email: email}) {
		t.Fatalf("Check = %+v, %v; want pending for %s <%s>", link, err, username, email)
	}
	if emailVerified(t, db, userID) {
		t.Fatal("Check verified the address")
	}
	state, verifiedUser, err := svc.EmailVerifier.Verify(ctx, token)
	if err != nil || state != model.EmailVerificationVerified || verifiedUser == nil || verifiedUser.ID != userID {
		t.Fatalf("Verify = %q, %v, %v; want verified user %d", state, verifiedUser, err, userID)
	}
	if !emailVerified(t, db, userID) {
		t.Fatal("address not verified")
	}

	if state, u, err := svc.EmailVerifier.Verify(ctx, token); err != nil || state != model.EmailVerificationInvalid || u != nil {
		t.Errorf("second Verify = %q, %v, %v; want invalid", state, u, err)
	}
	if link, _ := svc.EmailVerifier.Check(ctx, token); link != (model.EmailVerificationLink{State: model.EmailVerificationInvalid}) {
		t.Errorf("Check after use = %+v, want invalid and nothing named", link)
	}
}

func TestEmailVerification_StoresOnlyTheTokenHash(t *testing.T) {
	smtp, box := testutil.FakeSMTP(t)
	svc, db := newVerificationServices(t, smtp)
	userID := testutil.SeedUser(t, db, testutil.UniqueSuffix(t))

	token := sendLink(t, svc, box, userID)
	if len(token) < 43 {
		t.Errorf("token %q is shorter than 32 random bytes", token)
	}

	var stored string
	if err := db.QueryRowContext(context.Background(),
		`SELECT token_hash FROM email_verification_tokens WHERE user_id = $1`, userID).Scan(&stored); err != nil {
		t.Fatalf("read token row: %v", err)
	}
	sum := sha256.Sum256([]byte(token))
	if stored != hex.EncodeToString(sum[:]) {
		t.Errorf("token_hash = %q, want the SHA-256 of the token", stored)
	}
	var leaks int
	if err := db.QueryRowContext(context.Background(),
		`SELECT COUNT(*) FROM email_verification_tokens t WHERE strpos(t::text, $1) > 0`, token).Scan(&leaks); err != nil {
		t.Fatal(err)
	}
	if leaks != 0 {
		t.Error("the raw token is stored in the database")
	}
}

func TestEmailVerification_ExpiredLinkDoesNotVerify(t *testing.T) {
	smtp, box := testutil.FakeSMTP(t)
	svc, db := newVerificationServices(t, smtp)
	ctx := context.Background()
	userID := testutil.SeedUser(t, db, testutil.UniqueSuffix(t))
	token := sendLink(t, svc, box, userID)

	var secs float64
	if err := db.QueryRowContext(ctx,
		`SELECT EXTRACT(EPOCH FROM expires_at - created_at) FROM email_verification_tokens WHERE user_id = $1`, userID).Scan(&secs); err != nil {
		t.Fatal(err)
	}
	if ttl := time.Duration(secs * float64(time.Second)); ttl < 23*time.Hour || ttl > 25*time.Hour {
		t.Errorf("link lifetime = %v, want 24h", ttl)
	}

	testutil.Exec(t, db, `UPDATE email_verification_tokens SET expires_at = NOW() - interval '1 second' WHERE user_id = $1`, userID)
	if link, _ := svc.EmailVerifier.Check(ctx, token); link != (model.EmailVerificationLink{State: model.EmailVerificationExpired}) {
		t.Errorf("Check = %+v, want expired and nothing named", link)
	}
	if state, u, err := svc.EmailVerifier.Verify(ctx, token); err != nil || state != model.EmailVerificationExpired || u != nil {
		t.Errorf("Verify = %q, %v, %v; want expired", state, u, err)
	}
	if emailVerified(t, db, userID) {
		t.Error("an expired link verified the address")
	}
}

func TestEmailVerification_EmailChangeInvalidatesLinksAndVerification(t *testing.T) {
	smtp, box := testutil.FakeSMTP(t)
	svc, db := newVerificationServices(t, smtp)
	ctx := context.Background()
	suffix := testutil.UniqueSuffix(t)
	userID := withPassword(t, db, testutil.SeedUser(t, db, suffix))
	oldLink := sendLink(t, svc, box, userID)

	// Within the cooldown, so no new link replaces the old one: the change itself must revoke it.
	newEmail := "changed_" + suffix + "@test.invalid"
	if err := svc.User.UpdateProfile(ctx, userID, "", newEmail, "", "", "", confirmed); err != nil {
		t.Fatalf("UpdateProfile: %v", err)
	}
	if state, u, err := svc.EmailVerifier.Verify(ctx, oldLink); err != nil || state != model.EmailVerificationInvalid || u != nil {
		t.Errorf("link for the old address: Verify = %q, %v, %v; want invalid", state, u, err)
	}
	if emailVerified(t, db, userID) {
		t.Fatal("a link sent to the old address verified the new one")
	}

	if notice := box.NextTo(t, "testuser_"+suffix+"@test.invalid"); !strings.Contains(notice.Data, newEmail) {
		t.Errorf("the old address's notice doesn't name the new one: %.300s", notice.Data)
	}

	ageTokens(t, db, userID)
	newEmail = "changed2_" + suffix + "@test.invalid"
	if err := svc.User.UpdateProfile(ctx, userID, "", newEmail, "", "", "", confirmed); err != nil {
		t.Fatalf("UpdateProfile: %v", err)
	}
	mail := box.NextTo(t, newEmail)
	if state, _, err := svc.EmailVerifier.Verify(ctx, mail.VerificationToken(t)); err != nil || state != model.EmailVerificationVerified {
		t.Fatalf("link for the new address: Verify = %q, %v", state, err)
	}

	if err := svc.User.UpdateProfile(ctx, userID, "New name", newEmail, "", "", "", confirmed); err != nil {
		t.Fatalf("UpdateProfile, same email: %v", err)
	}
	if !emailVerified(t, db, userID) {
		t.Error("saving the profile without changing the email cleared verification")
	}
	if err := svc.User.UpdateProfile(ctx, userID, "", "again_"+suffix+"@test.invalid", "", "", "", confirmed); err != nil {
		t.Fatalf("UpdateProfile: %v", err)
	}
	if emailVerified(t, db, userID) {
		t.Error("changing a verified email kept it verified")
	}
}

// A link verifies the account it was issued to and nothing else, whoever holds it.
func TestEmailVerification_TokenVerifiesOnlyItsOwnAccount(t *testing.T) {
	smtp, box := testutil.FakeSMTP(t)
	svc, db := newVerificationServices(t, smtp)
	ctx := context.Background()
	alice := testutil.SeedUser(t, db, testutil.UniqueSuffix(t))
	bob := testutil.SeedUser(t, db, testutil.UniqueSuffix(t))
	aliceLink := sendLink(t, svc, box, alice)
	bobLink := sendLink(t, svc, box, bob)

	state, u, err := svc.EmailVerifier.Verify(ctx, aliceLink)
	if err != nil || state != model.EmailVerificationVerified || u == nil || u.ID != alice {
		t.Fatalf("Verify(alice's link) = %q, %v, %v; want alice verified", state, u, err)
	}
	if emailVerified(t, db, bob) {
		t.Error("alice's link verified bob")
	}
	if link, _ := svc.EmailVerifier.Check(ctx, bobLink); link.State != model.EmailVerificationPending {
		t.Errorf("alice's verification spent bob's link: %q", link.State)
	}
}

func TestEmailVerification_MalformedTokensAreInvalid(t *testing.T) {
	smtp, box := testutil.FakeSMTP(t)
	svc, db := newVerificationServices(t, smtp)
	ctx := context.Background()
	userID := testutil.SeedUser(t, db, testutil.UniqueSuffix(t))
	token := sendLink(t, svc, box, userID)

	sum := sha256.Sum256([]byte(token))
	for _, bad := range []string{"", "x", token + "A", token[:len(token)-1], strings.ToUpper(token), hex.EncodeToString(sum[:])} {
		if link, err := svc.EmailVerifier.Check(ctx, bad); err != nil || link.State != model.EmailVerificationInvalid {
			t.Errorf("Check(%q) = %q, %v; want invalid", bad, link.State, err)
		}
		if state, _, err := svc.EmailVerifier.Verify(ctx, bad); err != nil || state != model.EmailVerificationInvalid {
			t.Errorf("Verify(%q) = %q, %v; want invalid", bad, state, err)
		}
	}
	if emailVerified(t, db, userID) {
		t.Error("a malformed token verified the address")
	}
}

func TestEmailVerification_ResendCooldown(t *testing.T) {
	smtp, box := testutil.FakeSMTP(t)
	svc, db := newVerificationServices(t, smtp)
	ctx := context.Background()
	suffix := testutil.UniqueSuffix(t)
	userID := withPassword(t, db, testutil.SeedUser(t, db, suffix))
	first := sendLink(t, svc, box, userID)

	if err := svc.EmailVerifier.Send(ctx, userID); !errors.Is(err, service.ErrVerificationCooldown) {
		t.Fatalf("second Send: err = %v, want ErrVerificationCooldown", err)
	}
	// Changing the address revokes the outstanding link, which must not reset the cooldown.
	newEmail := "cooldown_" + suffix + "@test.invalid"
	if err := svc.User.UpdateProfile(ctx, userID, "", newEmail, "", "", "", confirmed); err != nil {
		t.Fatalf("UpdateProfile: %v", err)
	}
	if pending, err := svc.EmailVerifier.LinkPending(ctx, userID); err != nil || pending {
		t.Errorf("LinkPending after an email change within the cooldown = %v, %v; want false", pending, err)
	}
	if err := svc.EmailVerifier.Send(ctx, userID); !errors.Is(err, service.ErrVerificationCooldown) {
		t.Fatalf("Send after an email change: err = %v, want ErrVerificationCooldown", err)
	}
	box.NextTo(t, "testuser_"+suffix+"@test.invalid")
	box.Empty(t, 200*time.Millisecond)

	ageTokens(t, db, userID)
	second := sendLink(t, svc, box, userID)
	if link, _ := svc.EmailVerifier.Check(ctx, first); link.State != model.EmailVerificationInvalid {
		t.Errorf("a resend left the earlier link %q", link.State)
	}
	if link, _ := svc.EmailVerifier.Check(ctx, second); link.State != model.EmailVerificationPending {
		t.Errorf("new link = %q, want pending", link.State)
	}
	if pending, err := svc.EmailVerifier.LinkPending(ctx, userID); err != nil || !pending {
		t.Errorf("LinkPending after a resend = %v, %v; want true", pending, err)
	}
}

// Recreating the account or handing the address to another one must not buy
// another email to it: that would let anyone flood an address.
func TestEmailVerification_CooldownFollowsTheAddress(t *testing.T) {
	smtp, box := testutil.FakeSMTP(t)
	svc, db := newVerificationServices(t, smtp)
	ctx := context.Background()
	suffix := testutil.UniqueSuffix(t)
	victim := "victim_" + suffix + "@test.invalid"
	t.Cleanup(func() {
		testutil.Exec(t, db, `DELETE FROM email_verification_tokens WHERE user_id IS NULL AND lower(email) = lower($1)`, victim)
	})

	first := withPassword(t, db, testutil.SeedUser(t, db, testutil.UniqueSuffix(t)))
	if err := svc.User.UpdateProfile(ctx, first, "", victim, "", "", "", confirmed); err != nil {
		t.Fatalf("UpdateProfile: %v", err)
	}
	box.NextTo(t, victim)
	if err := svc.User.DeleteUser(ctx, first); err != nil {
		t.Fatalf("DeleteUser: %v", err)
	}

	second := withPassword(t, db, testutil.SeedUser(t, db, testutil.UniqueSuffix(t)))
	if err := svc.User.UpdateProfile(ctx, second, "", strings.ToUpper(victim), "", "", "", confirmed); err != nil {
		t.Fatalf("UpdateProfile: %v", err)
	}
	if err := svc.EmailVerifier.Send(ctx, second); !errors.Is(err, service.ErrVerificationCooldown) {
		t.Errorf("address taken by a new account after a delete: err = %v, want ErrVerificationCooldown", err)
	}

	moved := "moved_" + suffix + "@test.invalid"
	if err := svc.User.UpdateProfile(ctx, second, "", moved, "", "", "", confirmed); err != nil {
		t.Fatalf("UpdateProfile: %v", err)
	}
	box.NextTo(t, moved)
	third := withPassword(t, db, testutil.SeedUser(t, db, testutil.UniqueSuffix(t)))
	if err := svc.User.UpdateProfile(ctx, third, "", victim, "", "", "", confirmed); err != nil {
		t.Fatalf("UpdateProfile: %v", err)
	}
	if err := svc.EmailVerifier.Send(ctx, third); !errors.Is(err, service.ErrVerificationCooldown) {
		t.Errorf("address moved to another account: err = %v, want ErrVerificationCooldown", err)
	}
	// Change notices go to the addresses being left; only a link would be a second email to the victim.
	for _, mail := range box.Drain(200 * time.Millisecond) {
		if len(mail.To) == 1 && strings.EqualFold(mail.To[0], victim) && strings.Contains(mail.Data, "/verify-email?token=") {
			t.Errorf("a second verification link reached the victim's address")
		}
	}
}

func TestEmailVerification_ConcurrentSendsShareOneCooldown(t *testing.T) {
	smtp, box := testutil.FakeSMTP(t)
	svc, db := newVerificationServices(t, smtp)
	userID := testutil.SeedUser(t, db, testutil.UniqueSuffix(t))

	const n = 8
	// Open the connections up front: dialing serializes the sends otherwise.
	db.SetMaxIdleConns(n)
	var warm sync.WaitGroup
	for range n {
		warm.Add(1)
		go func() {
			defer warm.Done()
			_, _ = db.ExecContext(context.Background(), `SELECT pg_sleep(0.05)`)
		}()
	}
	warm.Wait()

	var wg sync.WaitGroup
	start := make(chan struct{})
	errs := make(chan error, n)
	for range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			errs <- svc.EmailVerifier.Send(context.Background(), userID)
		}()
	}
	close(start)
	wg.Wait()
	close(errs)
	sent := 0
	for err := range errs {
		switch {
		case err == nil:
			sent++
		case !errors.Is(err, service.ErrVerificationCooldown):
			t.Errorf("Send: %v", err)
		}
	}
	if sent != 1 {
		t.Errorf("%d concurrent sends got through, want 1", sent)
	}
	box.Next(t)
	box.Empty(t, 200*time.Millisecond)
}

// The server may accept a message and still fail the session, so a delivery
// error must not revoke a link that could have arrived.
func TestEmailVerification_FailedDeliveryKeepsTheLink(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	_ = ln.Close()
	svc, db := newVerificationServices(t, config.SMTPConfig{Host: "127.0.0.1", Port: port, From: "cz@test.invalid"})
	userID := testutil.SeedUser(t, db, testutil.UniqueSuffix(t))

	if err := svc.EmailVerifier.Send(context.Background(), userID); err != nil {
		t.Fatalf("Send: %v", err)
	}
	// Delivery fails in the background; a refused connection takes well under this.
	time.Sleep(200 * time.Millisecond)
	if err := svc.EmailVerifier.Send(context.Background(), userID); !errors.Is(err, service.ErrVerificationCooldown) {
		t.Errorf("second Send: err = %v, want ErrVerificationCooldown", err)
	}
	if n := tokenRows(t, db, userID); n != 1 {
		t.Errorf("%d tokens, want the one issued", n)
	}
}

func TestEmailVerification_AlreadyVerifiedIsNotResent(t *testing.T) {
	smtp, box := testutil.FakeSMTP(t)
	svc, db := newVerificationServices(t, smtp)
	userID := testutil.SeedUser(t, db, testutil.UniqueSuffix(t))
	testutil.Exec(t, db, `UPDATE users SET email_verified_at = NOW() WHERE id = $1`, userID)

	if err := svc.EmailVerifier.Send(context.Background(), userID); !errors.Is(err, service.ErrEmailAlreadyVerified) {
		t.Errorf("Send: err = %v, want ErrEmailAlreadyVerified", err)
	}
	box.Empty(t, 200*time.Millisecond)
}

func TestEmailVerification_WithoutSMTPNothingIsIssued(t *testing.T) {
	svc, db := newVerificationServices(t, config.SMTPConfig{})
	ctx := context.Background()
	suffix := testutil.UniqueSuffix(t)

	if svc.EmailVerifier.Available() {
		t.Error("Available() without an SMTP host")
	}
	u, err := svc.User.Create(ctx, "nosmtp_"+suffix, "nosmtp_"+suffix+"@test.invalid", "password1")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	t.Cleanup(func() { testutil.DeleteUsers(t, db, u.ID) })
	if err := svc.EmailVerifier.Send(ctx, u.ID); !errors.Is(err, service.ErrEmailVerificationUnavailable) {
		t.Errorf("Send: err = %v, want ErrEmailVerificationUnavailable", err)
	}
	if n := tokenRows(t, db, u.ID); n != 0 || emailVerified(t, db, u.ID) {
		t.Errorf("without SMTP: %d tokens, verified=%v; want none, unverified", n, emailVerified(t, db, u.ID))
	}
}

// The invite link came from an admin, so the invitee still proves the address.
func TestEmailVerification_InviteAcceptanceSendsALink(t *testing.T) {
	smtp, box := testutil.FakeSMTP(t)
	svc, db := newVerificationServices(t, smtp)
	ctx := context.Background()
	suffix := testutil.UniqueSuffix(t)
	adminID := testutil.SeedSuperadmin(t, db, suffix)
	t.Cleanup(func() { testutil.Exec(t, db, `DELETE FROM invitations WHERE invited_by_id = $1`, adminID) })
	email := "invitee_" + suffix + "@test.invalid"
	inv, err := svc.Invitation.Create(ctx, adminID, email)
	if err != nil {
		t.Fatalf("Invitation.Create: %v", err)
	}

	u, err := svc.User.CreateFromInvitation(ctx, inv, "invitee_"+suffix, "password1")
	if err != nil {
		t.Fatalf("CreateFromInvitation: %v", err)
	}
	t.Cleanup(func() { testutil.DeleteUsers(t, db, u.ID) })
	if u.EmailVerified() {
		t.Error("an invited account starts verified")
	}
	mail := box.Next(t)
	if len(mail.To) != 1 || mail.To[0] != email {
		t.Fatalf("invite acceptance sent to %v, want %s", mail.To, email)
	}
	if state, _, err := svc.EmailVerifier.Verify(ctx, mail.VerificationToken(t)); err != nil || state != model.EmailVerificationVerified {
		t.Errorf("Verify = %q, %v; want verified", state, err)
	}
}

var signupLinkRe = regexp.MustCompile(`/register/complete/([0-9a-f]{64})`)

// Only the address's owner can open a signup link, so its account needs no second proof.
func TestEmailVerification_SignupLinkAccountStartsVerified(t *testing.T) {
	smtp, box := testutil.FakeSMTP(t)
	svc, db := newVerificationServices(t, smtp)
	ctx := context.Background()
	suffix := testutil.UniqueSuffix(t)
	email := "signup_" + suffix + "@test.invalid"
	t.Cleanup(func() { testutil.Exec(t, db, `DELETE FROM signup_tokens WHERE lower(email) = lower($1)`, email) })

	if err := svc.Signup.Request(ctx, email); err != nil {
		t.Fatalf("Signup.Request: %v", err)
	}
	match := signupLinkRe.FindStringSubmatch(box.Next(t).Data)
	if match == nil {
		t.Fatal("no signup link in the email")
	}
	u, err := svc.Signup.Complete(ctx, match[1], "signup_"+suffix, "password1")
	if err != nil {
		t.Fatalf("Signup.Complete: %v", err)
	}
	t.Cleanup(func() { testutil.DeleteUsers(t, db, u.ID) })
	if !u.EmailVerified() || !emailVerified(t, db, u.ID) {
		t.Errorf("signup-link account: verified=%v (db %v), want verified", u.EmailVerified(), emailVerified(t, db, u.ID))
	}
	box.Empty(t, 200*time.Millisecond)
}

func TestEmailVerification_MarkVerifiedNeedsTheCurrentEmail(t *testing.T) {
	svc, db := newVerificationServices(t, config.SMTPConfig{})
	ctx := context.Background()
	suffix := testutil.UniqueSuffix(t)
	userID := testutil.SeedUser(t, db, suffix)
	username, email := "testuser_"+suffix, "testuser_"+suffix+"@test.invalid"

	for _, tc := range []struct{ username, email string }{
		{username, "other_" + suffix + "@test.invalid"},
		{"nobody_" + suffix, email},
		{strings.ToUpper(username), email},
	} {
		if _, err := svc.EmailVerifier.MarkVerified(ctx, tc.username, tc.email); !errors.Is(err, service.ErrNoSuchUserEmail) {
			t.Errorf("MarkVerified(%q, %q): err = %v, want ErrNoSuchUserEmail", tc.username, tc.email, err)
		}
	}
	if emailVerified(t, db, userID) {
		t.Fatal("a mismatched MarkVerified verified the address")
	}
	u, err := svc.EmailVerifier.MarkVerified(ctx, username, email)
	if err != nil || u.ID != userID {
		t.Fatalf("MarkVerified = %v, %v; want user %d", u, err, userID)
	}
	if !emailVerified(t, db, userID) {
		t.Error("MarkVerified did not verify the address")
	}
}

// The link UPDATE re-checks what AuthenticateOAuth read, so an address changed
// or linked in between is not linked.
func TestUserStore_LinkOAuthByVerifiedEmail_RechecksTheAccount(t *testing.T) {
	db := testutil.OpenTestDB(t)
	users := store.NewUserStore(db)
	ctx := context.Background()
	suffix := testutil.UniqueSuffix(t)
	userID := testutil.SeedUser(t, db, suffix)
	email := "testuser_" + suffix + "@test.invalid"

	if _, err := users.LinkOAuthByVerifiedEmail(ctx, userID, email, "google", "g_"+suffix); !errors.Is(err, sql.ErrNoRows) {
		t.Errorf("unverified: err = %v, want sql.ErrNoRows", err)
	}
	testutil.Exec(t, db, `UPDATE users SET email_verified_at = NOW() WHERE id = $1`, userID)
	if _, err := users.LinkOAuthByVerifiedEmail(ctx, userID, "stale_"+email, "google", "g_"+suffix); !errors.Is(err, sql.ErrNoRows) {
		t.Errorf("stale email: err = %v, want sql.ErrNoRows", err)
	}
	u, err := users.LinkOAuthByVerifiedEmail(ctx, userID, email, "google", "g_"+suffix)
	if err != nil || u.OAuthID != "g_"+suffix {
		t.Fatalf("LinkOAuth = %v, %v; want linked", u, err)
	}
	if _, err := users.LinkOAuthByVerifiedEmail(ctx, userID, email, "google", "g_other_"+suffix); !errors.Is(err, sql.ErrNoRows) {
		t.Errorf("already linked: err = %v, want sql.ErrNoRows", err)
	}
}
