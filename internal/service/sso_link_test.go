package service

import (
	"context"
	"testing"

	"github.com/mkappworks-dev/cloudzilla-app/internal/config"
	"github.com/mkappworks-dev/cloudzilla-app/internal/store"
	"github.com/mkappworks-dev/cloudzilla-app/internal/testutil"
)

// Google links on an address both sides verified; LDAP and SAML must not
// follow, since only the IdP operator vouches for the address they assert.
func TestFindOrProvisionUser_DoesNotLinkAVerifiedEmail(t *testing.T) {
	db := testutil.OpenTestDB(t)
	suffix := testutil.UniqueSuffix(t)
	userID, email := testutil.SeedUserWithPassword(t, db, suffix, "password1")
	testutil.Exec(t, db, `UPDATE users SET email_verified_at = NOW() WHERE id = $1`, userID)
	svc := NewSSOService(store.NewSSOStore(db), store.NewUserStore(db), config.AuthConfig{JWTSecret: "test-secret-32bytes-minimum-len!"}, nil)

	for _, provider := range []string{"ldap", "saml"} {
		u, token, err := svc.findOrProvisionUser(context.Background(), provider, "cn=sso_"+suffix, "sso_"+suffix, email, true)
		if u != nil {
			if u.ID != userID {
				testutil.DeleteUsers(t, db, u.ID)
			}
			t.Errorf("%s sign-in with a verified local email returned user %d", provider, u.ID)
		}
		if err == nil || token != "" {
			t.Errorf("%s: err = %v, token issued = %v; want refused", provider, err, token != "")
		}
	}
	var ssoID string
	if err := db.QueryRowContext(context.Background(), `SELECT COALESCE(sso_id, '') FROM users WHERE id = $1`, userID).Scan(&ssoID); err != nil {
		t.Fatal(err)
	}
	if ssoID != "" {
		t.Errorf("account was linked to SSO identity %q", ssoID)
	}
}
