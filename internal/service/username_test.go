package service

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/mkappworks-dev/cloudzilla-app/internal/config"
	"github.com/mkappworks-dev/cloudzilla-app/internal/store"
	"github.com/mkappworks-dev/cloudzilla-app/internal/testutil"
)

func TestValidateUsername(t *testing.T) {
	for _, ok := range []string{"a", "A1", "bob-the_builder", strings.Repeat("a", 39)} {
		if err := ValidateUsername(ok); err != nil {
			t.Errorf("ValidateUsername(%q) = %v, want nil", ok, err)
		}
		if err := ValidateName(ok); err != nil {
			t.Errorf("valid username %q is not a valid owner name: %v", ok, err)
		}
	}
	bad := append([]string{"", strings.Repeat("a", 40), "-bob", "_bob", "bob.smith", "bob smith", "a/b", "..", "jöhn"}, testutil.HostileNames...)
	for _, name := range bad {
		if err := ValidateUsername(name); !errors.Is(err, ErrInvalidUsername) {
			t.Errorf("ValidateUsername(%q) = %v, want ErrInvalidUsername", name, err)
		}
	}
}

func derivedNameInputs() []string {
	return append([]string{
		"John Smith",
		"-_Bob",
		"__--",
		"Jöhn Dœ",
		"🙂",
		strings.Repeat("Long Name ", 10),
	}, testutil.HostileNames...)
}

func TestUsernameBase_IsEmptyOrValid(t *testing.T) {
	for _, raw := range derivedNameInputs() {
		if got := usernameBase(raw); got != "" && ValidateUsername(got) != nil {
			t.Errorf("usernameBase(%q) = %q, not a valid username", raw, got)
		}
	}
	if got := usernameBase("John Smith"); got != "johnsmith" {
		t.Errorf(`usernameBase("John Smith") = %q, want "johnsmith"`, got)
	}
}

func TestSanitizeUsername_IsValid(t *testing.T) {
	for _, raw := range derivedNameInputs() {
		if got := sanitizeUsername(raw); ValidateUsername(got) != nil {
			t.Errorf("sanitizeUsername(%q) = %q, not a valid username", raw, got)
		}
	}
}

// A taken 39-char base must still yield a username within the length limit once suffixed.
func TestUniqueUsername_SuffixStaysWithinLimit(t *testing.T) {
	db := testutil.OpenTestDB(t)
	suffix := testutil.UniqueSuffix(t)
	taken := ("u" + strings.ReplaceAll(suffix, "_", "") + strings.Repeat("x", 39))[:39]
	testutil.Exec(t, db, `INSERT INTO users (username, email, password_hash) VALUES ($1, $2, 'x')`, taken, "unique_"+suffix+"@test.invalid")
	t.Cleanup(func() { testutil.Exec(t, db, `DELETE FROM users WHERE username = $1`, taken) })

	svc := NewUserService(store.NewUserStore(db), config.AuthConfig{})
	got := svc.uniqueUsername(context.Background(), "x@test.invalid", taken+"yyy")
	if got == taken || ValidateUsername(got) != nil {
		t.Errorf("uniqueUsername = %q, want a free valid username", got)
	}
}

func TestFindOrProvisionUser_SanitizesHostileSSOName(t *testing.T) {
	db := testutil.OpenTestDB(t)
	svc := NewSSOService(store.NewSSOStore(db), store.NewUserStore(db), config.AuthConfig{JWTSecret: "test-secret-32bytes-minimum-len!"}, nil)
	for _, name := range testutil.HostileNames {
		suffix := testutil.UniqueSuffix(t)
		u, _, err := svc.findOrProvisionUser(context.Background(), "ldap", "cn=sso_"+suffix, name+suffix, "sso_"+suffix+"@test.invalid", true)
		if err != nil {
			t.Fatalf("provision %q: %v", name, err)
		}
		testutil.DeleteUsers(t, db, u.ID)
		if err := ValidateUsername(u.Username); err != nil {
			t.Errorf("SSO name %q provisioned invalid username %q", name, u.Username)
		}
	}
}
