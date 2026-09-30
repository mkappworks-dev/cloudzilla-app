package service_test

// Integration tests for OAuthLinkService. All tests require TEST_DATABASE_DSN and skip otherwise.

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/mkappworks-dev/cloudzilla-app/internal/config"
	"github.com/mkappworks-dev/cloudzilla-app/internal/service"
	"github.com/mkappworks-dev/cloudzilla-app/internal/store"
	"github.com/mkappworks-dev/cloudzilla-app/internal/testutil"
)

const linkTestPassword = "correct horse battery"

func newOAuthLinkSvc(t *testing.T) (*service.OAuthLinkService, *sql.DB) {
	t.Helper()
	db := testutil.OpenTestDB(t)
	users := store.NewUserStore(db)
	return service.NewOAuthLinkService(users, store.NewOAuthStateStore(db), service.NewTOTPService(users), service.NewEmailService(config.SMTPConfig{})), db
}

// linkGrant runs the connect flow up to the callback for a password account without 2FA.
func linkGrant(t *testing.T, svc *service.OAuthLinkService, userID int64) service.LinkGrant {
	t.Helper()
	ctx := context.Background()
	state, _, err := svc.BeginLink(ctx, userID, linkTestPassword, "")
	if err != nil {
		t.Fatalf("BeginLink: %v", err)
	}
	grant, err := svc.ConsumeLinkState(ctx, state, userID)
	if err != nil {
		t.Fatalf("ConsumeLinkState: %v", err)
	}
	return grant
}

func googleIdentity(id, email string) service.OAuthIdentity {
	return service.OAuthIdentity{Provider: "google", ID: id, Email: email, EmailVerified: true, Name: "Linked"}
}

func linkedGoogleID(t *testing.T, db *sql.DB, userID int64) string {
	t.Helper()
	var provider, id string
	if err := db.QueryRowContext(context.Background(), `SELECT oauth_provider, oauth_id FROM users WHERE id = $1`, userID).Scan(&provider, &id); err != nil {
		t.Fatalf("read oauth link: %v", err)
	}
	if provider != "google" {
		return ""
	}
	return id
}

func TestOAuthLink_BeginLink_RefusesWrongPassword(t *testing.T) {
	svc, db := newOAuthLinkSvc(t)
	userID, _ := testutil.SeedUserWithPassword(t, db, testutil.UniqueSuffix(t), linkTestPassword)

	if _, _, err := svc.BeginLink(context.Background(), userID, "wrong password", ""); !errors.Is(err, service.ErrReauthFailed) {
		t.Fatalf("BeginLink with a wrong password: err = %v, want ErrReauthFailed", err)
	}
}

func TestOAuthLink_BeginLink_RequiresTOTPCodeWhenEnabled(t *testing.T) {
	svc, db := newOAuthLinkSvc(t)
	ctx := context.Background()
	userID, _ := testutil.SeedUserWithPassword(t, db, testutil.UniqueSuffix(t), linkTestPassword)
	testutil.EnableTOTP(t, db, userID)

	for _, code := range []string{"", "000000x", "12345"} {
		if _, _, err := svc.BeginLink(ctx, userID, linkTestPassword, code); !errors.Is(err, service.ErrReauthFailed) {
			t.Errorf("BeginLink with TOTP code %q: err = %v, want ErrReauthFailed", code, err)
		}
	}
	if _, _, err := svc.BeginLink(ctx, userID, linkTestPassword, testutil.TOTPCode(t, testutil.TestTOTPSecret)); err != nil {
		t.Fatalf("BeginLink with the right password and TOTP code: %v", err)
	}
}

func TestOAuthLink_BeginLink_RefusesAccountWithoutPassword(t *testing.T) {
	svc, db := newOAuthLinkSvc(t)
	suffix := testutil.UniqueSuffix(t)
	userID := testutil.SeedPasswordlessUser(t, db, suffix, "g_nopw_"+suffix)

	if _, _, err := svc.BeginLink(context.Background(), userID, "", ""); !errors.Is(err, service.ErrReauthNoPassword) {
		t.Fatalf("BeginLink on a passwordless account: err = %v, want ErrReauthNoPassword", err)
	}
}

func TestOAuthLink_BeginLink_RefusesAnAlreadyLinkedAccount(t *testing.T) {
	svc, db := newOAuthLinkSvc(t)
	ctx := context.Background()
	suffix := testutil.UniqueSuffix(t)
	userID, email := testutil.SeedUserWithPassword(t, db, suffix, linkTestPassword)
	if _, err := svc.Link(ctx, linkGrant(t, svc, userID), googleIdentity("g_linked_"+suffix, email)); err != nil {
		t.Fatalf("Link: %v", err)
	}

	if _, _, err := svc.BeginLink(ctx, userID, linkTestPassword, ""); !errors.Is(err, service.ErrOAuthAlreadyLinked) {
		t.Fatalf("BeginLink on a linked account: err = %v, want ErrOAuthAlreadyLinked", err)
	}
}

func TestOAuthLink_BeginLink_StateLivesFiveMinutes(t *testing.T) {
	svc, db := newOAuthLinkSvc(t)
	userID, _ := testutil.SeedUserWithPassword(t, db, testutil.UniqueSuffix(t), linkTestPassword)

	_, expiresAt, err := svc.BeginLink(context.Background(), userID, linkTestPassword, "")
	if err != nil {
		t.Fatalf("BeginLink: %v", err)
	}
	if left := time.Until(expiresAt); left < 4*time.Minute || left > 5*time.Minute+30*time.Second {
		t.Errorf("state expires in %v, want about 5 minutes", left)
	}
}

func TestOAuthLink_State_IsSingleUse(t *testing.T) {
	svc, db := newOAuthLinkSvc(t)
	ctx := context.Background()
	userID, _ := testutil.SeedUserWithPassword(t, db, testutil.UniqueSuffix(t), linkTestPassword)
	state, _, err := svc.BeginLink(ctx, userID, linkTestPassword, "")
	if err != nil {
		t.Fatalf("BeginLink: %v", err)
	}

	if _, err := svc.ConsumeLinkState(ctx, state, userID); err != nil {
		t.Fatalf("first ConsumeLinkState: %v", err)
	}
	if _, err := svc.ConsumeLinkState(ctx, state, userID); !errors.Is(err, service.ErrOAuthLinkInvalid) {
		t.Fatalf("replayed ConsumeLinkState: err = %v, want ErrOAuthLinkInvalid", err)
	}
}

func TestOAuthLink_State_IsBoundToItsUser(t *testing.T) {
	svc, db := newOAuthLinkSvc(t)
	ctx := context.Background()
	ownerID, _ := testutil.SeedUserWithPassword(t, db, testutil.UniqueSuffix(t), linkTestPassword)
	otherID, _ := testutil.SeedUserWithPassword(t, db, testutil.UniqueSuffix(t), linkTestPassword)
	state, _, err := svc.BeginLink(ctx, ownerID, linkTestPassword, "")
	if err != nil {
		t.Fatalf("BeginLink: %v", err)
	}

	if _, err := svc.ConsumeLinkState(ctx, state, otherID); !errors.Is(err, service.ErrOAuthLinkWrongUser) {
		t.Fatalf("ConsumeLinkState by another user: err = %v, want ErrOAuthLinkWrongUser", err)
	}
	// Any attempt burns the state, so the one who tried cannot hand it back to its owner.
	if _, err := svc.ConsumeLinkState(ctx, state, ownerID); !errors.Is(err, service.ErrOAuthLinkInvalid) {
		t.Fatalf("ConsumeLinkState by its owner after a refused attempt: err = %v, want ErrOAuthLinkInvalid", err)
	}
}

func TestOAuthLink_State_ExpiredIsRefused(t *testing.T) {
	svc, db := newOAuthLinkSvc(t)
	ctx := context.Background()
	userID, _ := testutil.SeedUserWithPassword(t, db, testutil.UniqueSuffix(t), linkTestPassword)
	state, _, err := svc.BeginLink(ctx, userID, linkTestPassword, "")
	if err != nil {
		t.Fatalf("BeginLink: %v", err)
	}
	testutil.Exec(t, db, `UPDATE oauth_states SET expires_at = NOW() - INTERVAL '1 second' WHERE user_id = $1`, userID)

	if _, err := svc.ConsumeLinkState(ctx, state, userID); !errors.Is(err, service.ErrOAuthLinkInvalid) {
		t.Fatalf("ConsumeLinkState on an expired state: err = %v, want ErrOAuthLinkInvalid", err)
	}
}

func TestOAuthLink_State_NewerBeginSupersedesOlder(t *testing.T) {
	svc, db := newOAuthLinkSvc(t)
	ctx := context.Background()
	userID, _ := testutil.SeedUserWithPassword(t, db, testutil.UniqueSuffix(t), linkTestPassword)
	older, _, err := svc.BeginLink(ctx, userID, linkTestPassword, "")
	if err != nil {
		t.Fatalf("first BeginLink: %v", err)
	}
	newer, _, err := svc.BeginLink(ctx, userID, linkTestPassword, "")
	if err != nil {
		t.Fatalf("second BeginLink: %v", err)
	}

	if _, err := svc.ConsumeLinkState(ctx, older, userID); !errors.Is(err, service.ErrOAuthLinkInvalid) {
		t.Errorf("superseded state: err = %v, want ErrOAuthLinkInvalid", err)
	}
	if _, err := svc.ConsumeLinkState(ctx, newer, userID); err != nil {
		t.Errorf("latest state: %v", err)
	}
}

func TestOAuthLink_Link_RefusesAGrantItDidNotIssue(t *testing.T) {
	svc, db := newOAuthLinkSvc(t)
	suffix := testutil.UniqueSuffix(t)
	userID, email := testutil.SeedUserWithPassword(t, db, suffix, linkTestPassword)

	if _, err := svc.Link(context.Background(), service.LinkGrant{}, googleIdentity("g_nogrant_"+suffix, email)); !errors.Is(err, service.ErrOAuthLinkInvalid) {
		t.Fatalf("Link with a zero grant: err = %v, want ErrOAuthLinkInvalid", err)
	}
	if got := linkedGoogleID(t, db, userID); got != "" {
		t.Errorf("account was linked to %q", got)
	}
}

func TestOAuthLink_Link_RefusesUnverifiedGoogleEmail(t *testing.T) {
	svc, db := newOAuthLinkSvc(t)
	suffix := testutil.UniqueSuffix(t)
	userID, email := testutil.SeedUserWithPassword(t, db, suffix, linkTestPassword)
	id := googleIdentity("g_unverified_"+suffix, email)
	id.EmailVerified = false

	if _, err := svc.Link(context.Background(), linkGrant(t, svc, userID), id); !errors.Is(err, service.ErrOAuthEmailUnverified) {
		t.Fatalf("Link with an unverified Google email: err = %v, want ErrOAuthEmailUnverified", err)
	}
	if got := linkedGoogleID(t, db, userID); got != "" {
		t.Errorf("account was linked to %q", got)
	}
}

func TestOAuthLink_Link_RefusesGoogleIDLinkedToAnotherAccount(t *testing.T) {
	svc, db := newOAuthLinkSvc(t)
	suffix := testutil.UniqueSuffix(t)
	googleID := "g_taken_" + suffix
	holderID := testutil.SeedPasswordlessUser(t, db, suffix, googleID)
	userID, email := testutil.SeedUserWithPassword(t, db, suffix, linkTestPassword)

	if _, err := svc.Link(context.Background(), linkGrant(t, svc, userID), googleIdentity(googleID, email)); !errors.Is(err, service.ErrOAuthLinkedElsewhere) {
		t.Fatalf("Link with a Google ID another account holds: err = %v, want ErrOAuthLinkedElsewhere", err)
	}
	if got := linkedGoogleID(t, db, userID); got != "" {
		t.Errorf("account was linked to %q", got)
	}
	if got := linkedGoogleID(t, db, holderID); got != googleID {
		t.Errorf("holder's link changed to %q", got)
	}
}

func TestOAuthLink_Link_SameGoogleIDIsANoOp(t *testing.T) {
	svc, db := newOAuthLinkSvc(t)
	ctx := context.Background()
	suffix := testutil.UniqueSuffix(t)
	userID, email := testutil.SeedUserWithPassword(t, db, suffix, linkTestPassword)
	grant := linkGrant(t, svc, userID)
	id := googleIdentity("g_same_"+suffix, email)

	if changed, err := svc.Link(ctx, grant, id); err != nil || !changed {
		t.Fatalf("first Link: changed = %v, err = %v; want a new link", changed, err)
	}
	if changed, err := svc.Link(ctx, grant, id); err != nil || changed {
		t.Fatalf("repeated Link: changed = %v, err = %v; want a no-op", changed, err)
	}
}

func TestOAuthLink_Link_RefusesWhenAccountHasADifferentGoogleID(t *testing.T) {
	svc, db := newOAuthLinkSvc(t)
	ctx := context.Background()
	suffix := testutil.UniqueSuffix(t)
	userID, email := testutil.SeedUserWithPassword(t, db, suffix, linkTestPassword)
	grant := linkGrant(t, svc, userID)
	if _, err := svc.Link(ctx, grant, googleIdentity("g_first_"+suffix, email)); err != nil {
		t.Fatalf("first Link: %v", err)
	}

	if _, err := svc.Link(ctx, grant, googleIdentity("g_second_"+suffix, email)); !errors.Is(err, service.ErrOAuthAlreadyLinked) {
		t.Fatalf("Link with a second Google ID: err = %v, want ErrOAuthAlreadyLinked", err)
	}
	if got := linkedGoogleID(t, db, userID); got != "g_first_"+suffix {
		t.Errorf("link changed to %q", got)
	}
}

func TestOAuthLink_Unlink_RefusesAccountWithoutPassword(t *testing.T) {
	svc, db := newOAuthLinkSvc(t)
	suffix := testutil.UniqueSuffix(t)
	googleID := "g_only_" + suffix
	userID := testutil.SeedPasswordlessUser(t, db, suffix, googleID)

	if _, err := svc.Unlink(context.Background(), userID, "google", "", ""); !errors.Is(err, service.ErrReauthNoPassword) {
		t.Fatalf("Unlink on a passwordless account: err = %v, want ErrReauthNoPassword", err)
	}
	if got := linkedGoogleID(t, db, userID); got != googleID {
		t.Errorf("passwordless account lost its only sign-in: link is %q", got)
	}
}

func TestOAuthLink_Unlink_RequiresPasswordAndTOTP(t *testing.T) {
	svc, db := newOAuthLinkSvc(t)
	ctx := context.Background()
	suffix := testutil.UniqueSuffix(t)
	googleID := "g_unlink_" + suffix
	userID, email := testutil.SeedUserWithPassword(t, db, suffix, linkTestPassword)
	if _, err := svc.Link(ctx, linkGrant(t, svc, userID), googleIdentity(googleID, email)); err != nil {
		t.Fatalf("Link: %v", err)
	}
	testutil.EnableTOTP(t, db, userID)

	if _, err := svc.Unlink(ctx, userID, "google", "wrong password", testutil.TOTPCode(t, testutil.TestTOTPSecret)); !errors.Is(err, service.ErrReauthFailed) {
		t.Errorf("Unlink with a wrong password: err = %v, want ErrReauthFailed", err)
	}
	if _, err := svc.Unlink(ctx, userID, "google", linkTestPassword, ""); !errors.Is(err, service.ErrReauthFailed) {
		t.Errorf("Unlink without the TOTP code: err = %v, want ErrReauthFailed", err)
	}
	if got := linkedGoogleID(t, db, userID); got == "" {
		t.Fatal("a refused Unlink removed the link")
	}
	removed, err := svc.Unlink(ctx, userID, "google", linkTestPassword, testutil.TOTPCode(t, testutil.TestTOTPSecret))
	if err != nil {
		t.Fatalf("Unlink with the right password and code: %v", err)
	}
	if removed != googleID {
		t.Errorf("Unlink reported removing %q, want %q", removed, googleID)
	}
	if got := linkedGoogleID(t, db, userID); got != "" {
		t.Errorf("link still %q after Unlink", got)
	}
	if _, err := svc.Unlink(ctx, userID, "google", linkTestPassword, testutil.TOTPCode(t, testutil.TestTOTPSecret)); !errors.Is(err, service.ErrOAuthNotLinked) {
		t.Errorf("second Unlink: err = %v, want ErrOAuthNotLinked", err)
	}
}
