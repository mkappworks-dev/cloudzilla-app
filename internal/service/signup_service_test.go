package service_test

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"

	"github.com/mkappworks-dev/cloudzilla-app/internal/service"
	"github.com/mkappworks-dev/cloudzilla-app/internal/store"
	"github.com/mkappworks-dev/cloudzilla-app/internal/testutil"
)

type sentSignupMail struct{ kind, to, url string }

type recordingSignupMailer struct{ sent []sentSignupMail }

func (m *recordingSignupMailer) Enabled() bool { return true }

func (m *recordingSignupMailer) SendSignupLink(to, link string) error {
	m.sent = append(m.sent, sentSignupMail{"link", to, link})
	return nil
}

func (m *recordingSignupMailer) SendAccountExists(to, loginURL string) error {
	m.sent = append(m.sent, sentSignupMail{"exists", to, loginURL})
	return nil
}

const signupBaseURL = "https://cz.test"

func newSignupSvc(db *sql.DB, mailer service.SignupMailer) *service.SignupService {
	return service.NewSignupService(store.NewSignupTokenStore(db), store.NewUserStore(db), mailer, signupBaseURL+"/")
}

func cleanupSignup(t *testing.T, db *sql.DB, email string) {
	t.Helper()
	t.Cleanup(func() {
		testutil.Exec(t, db, `DELETE FROM signup_tokens WHERE lower(email) = lower($1)`, email)
		testutil.Exec(t, db, `DELETE FROM users WHERE lower(email) = lower($1) AND username LIKE 'signup\_%'`, email)
	})
}

func requestLink(t *testing.T, svc *service.SignupService, mailer *recordingSignupMailer, email string) string {
	t.Helper()
	if err := svc.Request(context.Background(), email); err != nil {
		t.Fatalf("Request: %v", err)
	}
	if len(mailer.sent) == 0 || mailer.sent[len(mailer.sent)-1].kind != "link" {
		t.Fatalf("want a signup link email, got %+v", mailer.sent)
	}
	link := mailer.sent[len(mailer.sent)-1].url
	const prefix = signupBaseURL + "/register/complete/"
	if !strings.HasPrefix(link, prefix) {
		t.Fatalf("link %q must start with %q", link, prefix)
	}
	return strings.TrimPrefix(link, prefix)
}

func TestSignupService_Request_NewEmail_SendsLinkStoredOnlyAsHash(t *testing.T) {
	db := testutil.OpenTestDB(t)
	email := "signup_" + testutil.UniqueSuffix(t) + "@test.invalid"
	cleanupSignup(t, db, email)
	mailer := &recordingSignupMailer{}
	svc := newSignupSvc(db, mailer)

	token := requestLink(t, svc, mailer, email)

	if mailer.sent[0].to != email {
		t.Errorf("mail must go to %s, went to %s", email, mailer.sent[0].to)
	}
	tok, err := svc.GetUsable(context.Background(), token)
	if err != nil || tok.Email != email {
		t.Errorf("the emailed link must be usable for %s: %+v, %v", email, tok, err)
	}
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM signup_tokens WHERE token_hash = $1`, token).Scan(&n); err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != 0 {
		t.Error("the raw token must never be stored")
	}
}

func TestSignupService_Request_ExistingEmailInOtherCase_SendsAccountExists(t *testing.T) {
	db := testutil.OpenTestDB(t)
	suffix := testutil.UniqueSuffix(t)
	testutil.SeedUser(t, db, suffix)
	email := "TestUser_" + suffix + "@Test.Invalid"
	cleanupSignup(t, db, email)
	mailer := &recordingSignupMailer{}

	if err := newSignupSvc(db, mailer).Request(context.Background(), email); err != nil {
		t.Fatalf("Request: %v", err)
	}

	if len(mailer.sent) != 1 || mailer.sent[0].kind != "exists" || mailer.sent[0].url != signupBaseURL+"/login" {
		t.Errorf("want one account-exists mail linking to sign-in, got %+v", mailer.sent)
	}
}

func TestSignupService_Request_WithinThrottleWindow_SendsOnce(t *testing.T) {
	db := testutil.OpenTestDB(t)
	email := "signup_" + testutil.UniqueSuffix(t) + "@test.invalid"
	cleanupSignup(t, db, email)
	mailer := &recordingSignupMailer{}
	svc := newSignupSvc(db, mailer)

	for range 2 {
		if err := svc.Request(context.Background(), email); err != nil {
			t.Fatalf("Request: %v", err)
		}
	}

	if len(mailer.sent) != 1 {
		t.Errorf("want 1 mail within 5 minutes, got %d", len(mailer.sent))
	}
}

func TestSignupService_Complete_CreatesOrdinaryUserWithLinkEmail(t *testing.T) {
	db := testutil.OpenTestDB(t)
	suffix := testutil.UniqueSuffix(t)
	email := "signup_" + suffix + "@test.invalid"
	cleanupSignup(t, db, email)
	mailer := &recordingSignupMailer{}
	svc := newSignupSvc(db, mailer)
	token := requestLink(t, svc, mailer, email)

	u, err := svc.Complete(context.Background(), token, "signup_"+suffix, "password123")
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}

	if u.Email != email || u.IsInvited {
		t.Errorf("want an ordinary user with the link's email, got %+v", u)
	}
	if _, err := svc.Complete(context.Background(), token, "signup2_"+suffix, "password123"); !errors.Is(err, service.ErrSignupTokenUnusable) {
		t.Errorf("a redeemed link must be unusable; got %v", err)
	}
}
