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

func TestValidateOwnerName(t *testing.T) {
	valid := []string{"alice", "a-b_c", "A1", strings.Repeat("a", 39)}
	for _, name := range valid {
		if err := ValidateOwnerName(name); err != nil {
			t.Errorf("ValidateOwnerName(%q) = %v, want nil", name, err)
		}
	}

	invalid := []string{
		"", ".", "..", "../x", "a/b", `a\b`, "a\x00", ".hidden", "-lead", "_lead", "a.b",
		strings.Repeat("a", 40), "admin", "Admin", "API", "settings",
	}
	for _, name := range invalid {
		if err := ValidateOwnerName(name); !errors.Is(err, ErrInvalidOwnerName) {
			t.Errorf("ValidateOwnerName(%q) = %v, want ErrInvalidOwnerName", name, err)
		}
	}
}

var awkwardProvisionedNames = []string{"admin", "..", "---", "a.b.c", strings.Repeat("n", 60), "Ωmega"}

func TestFitOwnerNameAndSanitizeUsername_AlwaysValid(t *testing.T) {
	for _, in := range awkwardProvisionedNames {
		if got := fitOwnerName(in, "user"); ValidateOwnerName(got) != nil {
			t.Errorf("fitOwnerName(%q) = %q, not a valid owner name", in, got)
		}
		if got := sanitizeUsername(in); ValidateOwnerName(got) != nil {
			t.Errorf("sanitizeUsername(%q) = %q, not a valid owner name", in, got)
		}
	}
	if got := sanitizeUsername("a.b.c"); got != "a_b_c" {
		t.Errorf("SSO usernames replace invalid characters with _; got %q", got)
	}
}

func TestUniqueUsername_AlwaysValid(t *testing.T) {
	svc := NewUserService(store.NewUserStore(testutil.OpenTestDB(t)), config.AuthConfig{})
	for _, in := range awkwardProvisionedNames {
		if got := svc.uniqueUsername(context.Background(), in+"@test.invalid", in); ValidateOwnerName(got) != nil {
			t.Errorf("uniqueUsername(%q) = %q, not a valid owner name", in, got)
		}
	}
	if got := svc.uniqueUsername(context.Background(), "x@test.invalid", "a.b.c"); !strings.HasPrefix(got, "abc") {
		t.Errorf("OAuth usernames drop invalid characters; got %q", got)
	}
}

func TestUniqueUsername_NumericSuffixStaysWithinLimit(t *testing.T) {
	db := testutil.OpenTestDB(t)
	taken := ("long_" + testutil.UniqueSuffix(t) + strings.Repeat("n", maxOwnerNameLen))[:maxOwnerNameLen]
	testutil.Exec(t, db, `INSERT INTO users (username, email, password_hash) VALUES ($1, $2, 'x')`, taken, taken+"@test.invalid")
	t.Cleanup(func() { testutil.Exec(t, db, `DELETE FROM users WHERE username = $1`, taken) })
	svc := NewUserService(store.NewUserStore(db), config.AuthConfig{})

	got := svc.uniqueUsername(context.Background(), "x@test.invalid", taken+"tail")

	if got == taken || ValidateOwnerName(got) != nil {
		t.Errorf("uniqueUsername = %q; want a valid name other than the taken %q", got, taken)
	}
}
