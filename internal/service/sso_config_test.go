package service

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/mkappworks-dev/cloudzilla-app/internal/config"
	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/store"
	"github.com/mkappworks-dev/cloudzilla-app/internal/testutil"
)

const ssoTestJWTSecret = "test-secret-32bytes-minimum-len!"

// newSSOTestService backs the service with a fresh schema, so configs, replay
// records and provisioned users never leak between tests.
func newSSOTestService(t *testing.T) (*SSOService, *sql.DB) {
	t.Helper()
	db := testutil.OpenFreshTestDB(t)
	users := store.NewUserStore(db)
	settings := NewSiteSettingService(store.NewSiteSettingStore(db), users)
	cfg := config.AuthConfig{JWTSecret: ssoTestJWTSecret, JWTExpiry: time.Hour}
	return NewSSOService(store.NewSSOStore(db), users, cfg, settings), db
}

var (
	readyLDAP = map[string]string{model.LDAPKeyHost: "ldap.test.invalid", model.LDAPKeyBindDNTmpl: "uid=%s,dc=test"}
	readySAML = map[string]string{
		model.SAMLKeyEntityID: "cz", model.SAMLKeySSOURL: "https://idp.test.invalid/sso",
		model.SAMLKeyACSURL: "https://cz.test.invalid/acs", model.SAMLKeyCert: "MIIB",
	}
)

func TestSSOService_GetConfig_NilWhenNothingSaved(t *testing.T) {
	svc, _ := newSSOTestService(t)
	cfg, err := svc.GetConfig(context.Background(), "ldap")
	if err != nil || cfg != nil {
		t.Errorf("GetConfig = %v, %v; want nil, nil", cfg, err)
	}
}

func TestSSOService_UnknownProviderIsRefused(t *testing.T) {
	svc, _ := newSSOTestService(t)
	ctx := context.Background()
	for name, err := range map[string]error{
		"SetConfig":    svc.SetConfig(ctx, "oidc", readyLDAP, true),
		"SaveSettings": svc.SaveSettings(ctx, "oidc", readyLDAP),
		"SetEnabled":   svc.SetEnabled(ctx, "oidc", true),
	} {
		if err == nil {
			t.Errorf("%s accepted an unknown provider", name)
		}
	}
}

func TestSSOService_SetConfig_RoundTripsThroughListConfigs(t *testing.T) {
	svc, _ := newSSOTestService(t)
	ctx := context.Background()
	if err := svc.SetConfig(ctx, "ldap", readyLDAP, true); err != nil {
		t.Fatal(err)
	}
	if err := svc.SetConfig(ctx, "saml", readySAML, false); err != nil {
		t.Fatal(err)
	}

	all, err := svc.ListConfigs(ctx)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]*model.SSOConfig{}
	for _, c := range all {
		got[c.Provider] = c
	}
	if len(got) != 2 || !got["ldap"].Enabled || got["saml"].Enabled {
		t.Fatalf("ListConfigs = %+v, want ldap enabled and saml disabled", got)
	}
	if got["ldap"].Config[model.LDAPKeyHost] != "ldap.test.invalid" {
		t.Errorf("ldap host = %q", got["ldap"].Config[model.LDAPKeyHost])
	}
}

func TestSSOService_SaveSettings_KeepsTheProviderOnOrOff(t *testing.T) {
	svc, _ := newSSOTestService(t)
	ctx := context.Background()
	if err := svc.SetConfig(ctx, "ldap", readyLDAP, true); err != nil {
		t.Fatal(err)
	}
	next := map[string]string{model.LDAPKeyHost: "other.test.invalid", model.LDAPKeyBindDNTmpl: "cn=%s"}
	if err := svc.SaveSettings(ctx, "ldap", next); err != nil {
		t.Fatal(err)
	}
	cfg, err := svc.GetConfig(ctx, "ldap")
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.Enabled || cfg.Config[model.LDAPKeyHost] != "other.test.invalid" {
		t.Errorf("after save: enabled = %v, host = %q", cfg.Enabled, cfg.Config[model.LDAPKeyHost])
	}
}

// Clearing a required setting while the provider is on would leave a login
// button that can only fail.
func TestSSOService_SaveSettings_RefusesToBlankAnEnabledProvider(t *testing.T) {
	svc, _ := newSSOTestService(t)
	ctx := context.Background()
	if err := svc.SetConfig(ctx, "ldap", readyLDAP, true); err != nil {
		t.Fatal(err)
	}
	err := svc.SaveSettings(ctx, "ldap", map[string]string{model.LDAPKeyHost: "ldap.test.invalid"})
	if !errors.Is(err, ErrSSOIncomplete) {
		t.Fatalf("got %v, want ErrSSOIncomplete", err)
	}
	cfg, _ := svc.GetConfig(ctx, "ldap")
	if cfg.Config[model.LDAPKeyBindDNTmpl] == "" {
		t.Error("refused save still overwrote the stored settings")
	}
}

func TestSSOService_SaveSettings_AllowsIncompleteDraftsWhileOff(t *testing.T) {
	svc, _ := newSSOTestService(t)
	ctx := context.Background()
	if err := svc.SaveSettings(ctx, "saml", map[string]string{model.SAMLKeyEntityID: "cz"}); err != nil {
		t.Fatalf("first draft: %v", err)
	}
	if err := svc.SetConfig(ctx, "ldap", readyLDAP, false); err != nil {
		t.Fatal(err)
	}
	if err := svc.SaveSettings(ctx, "ldap", map[string]string{}); err != nil {
		t.Fatalf("blanking a disabled provider: %v", err)
	}
}

func TestSSOService_SetEnabled(t *testing.T) {
	svc, _ := newSSOTestService(t)
	ctx := context.Background()

	if err := svc.SetEnabled(ctx, "ldap", true); !errors.Is(err, ErrSSOIncomplete) {
		t.Errorf("enable with nothing saved: got %v, want ErrSSOIncomplete", err)
	}
	if err := svc.SetEnabled(ctx, "ldap", false); err != nil {
		t.Errorf("disable with nothing saved: %v", err)
	}

	if err := svc.SetConfig(ctx, "saml", map[string]string{model.SAMLKeyEntityID: "cz"}, false); err != nil {
		t.Fatal(err)
	}
	if err := svc.SetEnabled(ctx, "saml", true); !errors.Is(err, ErrSSOIncomplete) {
		t.Errorf("enable with partial settings: got %v, want ErrSSOIncomplete", err)
	}

	if err := svc.SetConfig(ctx, "ldap", readyLDAP, false); err != nil {
		t.Fatal(err)
	}
	if err := svc.SetEnabled(ctx, "ldap", true); err != nil {
		t.Fatalf("enable ready provider: %v", err)
	}
	if !svc.ProviderEnabled(ctx, "ldap") {
		t.Error("ProviderEnabled = false after enabling")
	}
	if err := svc.SetEnabled(ctx, "ldap", false); err != nil {
		t.Fatal(err)
	}
	if svc.ProviderEnabled(ctx, "ldap") {
		t.Error("ProviderEnabled = true after disabling")
	}
}

func TestSSOService_ProviderEnabled_FalseWhenNotConfigured(t *testing.T) {
	svc, _ := newSSOTestService(t)
	if svc.ProviderEnabled(context.Background(), "saml") {
		t.Error("ProviderEnabled = true with no config")
	}
}

func TestSSOService_FindOrProvisionUser(t *testing.T) {
	svc, db := newSSOTestService(t)
	ctx := context.Background()

	u, token, err := svc.findOrProvisionUser(ctx, "ldap", "uid=ann", "Ann.Lee", "ann@example.test", true)
	if err != nil {
		t.Fatal(err)
	}
	if u.Username != "ann_lee" || token == "" {
		t.Errorf("provisioned %q, token issued = %v; want ann_lee with a token", u.Username, token != "")
	}

	again, _, err := svc.findOrProvisionUser(ctx, "ldap", "uid=ann", "renamed", "other@example.test", false)
	if err != nil || again.ID != u.ID {
		t.Errorf("returning sign-in: user %v, err %v; want user %d even with registration closed", again, err, u.ID)
	}

	// A different provider's identical ID must not resolve to the same account.
	if _, _, err := svc.findOrProvisionUser(ctx, "saml", "uid=ann", "x", "x@example.test", false); !errors.Is(err, ErrRegistrationDisabled) {
		t.Errorf("same ID on another provider: got %v, want ErrRegistrationDisabled", err)
	}

	if _, _, err := svc.findOrProvisionUser(ctx, "ldap", "uid=bob", "bob", "new@example.test", false); !errors.Is(err, ErrRegistrationDisabled) {
		t.Errorf("registration closed: got %v, want ErrRegistrationDisabled", err)
	}
	var n int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM users WHERE sso_id = 'uid=bob'`).Scan(&n); err != nil || n != 0 {
		t.Errorf("closed registration still created %d rows (err %v)", n, err)
	}
}

func TestSSOService_FindOrProvisionUser_SuspendedReturningUserGetsNoToken(t *testing.T) {
	svc, db := newSSOTestService(t)
	ctx := context.Background()
	u, _, err := svc.findOrProvisionUser(ctx, "ldap", "uid=sam", "sam", "sam@example.test", true)
	if err != nil {
		t.Fatal(err)
	}
	testutil.Exec(t, db, `UPDATE users SET suspended_at = NOW() WHERE id = $1`, u.ID)

	got, token, err := svc.findOrProvisionUser(ctx, "ldap", "uid=sam", "sam", "sam@example.test", true)
	if !errors.Is(err, ErrAccountSuspended) || token != "" {
		t.Errorf("got user %v, token issued = %v, err %v; want ErrAccountSuspended and no token", got, token != "", err)
	}
}

func TestSanitizeUsername(t *testing.T) {
	for in, want := range map[string]string{
		"Ann.Lee":          "ann_lee",
		"a b+c":            "a_b_c",
		"keep-dash":        "keep-dash",
		"UPPER99":          "upper99",
		"":                 "sso_user",
		"___":              "sso_user",
		"..":               "sso_user",
		"trailing.":        "trailing",
		"ünï":              "n", // non-ASCII letters become underscores, then the edges are trimmed
		"user@example.com": "user_example_com",
	} {
		if got := sanitizeUsername(in); got != want {
			t.Errorf("sanitizeUsername(%q) = %q, want %q", in, got, want)
		}
	}
}
