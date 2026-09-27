package service_test

// Integration tests for OAuthAppService. All tests require TEST_DATABASE_DSN and skip otherwise.

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/service"
	"github.com/mkappworks-dev/cloudzilla-app/internal/store"
	"github.com/mkappworks-dev/cloudzilla-app/internal/testutil"
)

const testRedirectURI = "https://example.com/cb"

// newOAuthSvc builds an OAuthAppService backed by the test database and seeds an owner user.
func newOAuthSvc(t *testing.T) (*service.OAuthAppService, int64) {
	t.Helper()
	db := testutil.OpenTestDB(t)
	suffix := testutil.UniqueSuffix(t)
	ownerID := testutil.SeedUser(t, db, suffix)
	svc := service.NewOAuthAppService(
		store.NewOAuthAppStore(db),
		store.NewOAuthAuthorizationStore(db),
		store.NewUserStore(db),
	)
	return svc, ownerID
}

// TestOAuthApp_CreateApp_ReturnsClientIDAndSecret verifies that CreateApp generates
// a non-empty client ID and returns the raw (unhashed) client secret to the caller.
func TestOAuthApp_CreateApp_ReturnsClientIDAndSecret(t *testing.T) {
	svc, ownerID := newOAuthSvc(t)

	app, rawSecret, err := svc.CreateApp(context.Background(), ownerID,
		"Test App", "https://example.com", "A test app", []string{"https://example.com/callback"})
	if err != nil {
		t.Fatalf("CreateApp: %v", err)
	}
	if app.ID == 0 {
		t.Error("created app must have non-zero ID")
	}
	if app.ClientID == "" {
		t.Error("CreateApp must return a non-empty client ID")
	}
	if rawSecret == "" {
		t.Error("CreateApp must return the raw client secret for one-time display")
	}
	// The stored hash must differ from the raw secret.
	if app.ClientSecret == rawSecret {
		t.Error("stored client_secret must be a hash, not the raw secret")
	}
}

// TestOAuthApp_CreateApp_UniqueClientIDs verifies that successive CreateApp calls
// generate different client IDs (entropy comes from crypto/rand).
func TestOAuthApp_CreateApp_UniqueClientIDs(t *testing.T) {
	svc, ownerID := newOAuthSvc(t)

	app1, _, err := svc.CreateApp(context.Background(), ownerID, "App1", "", "", []string{testRedirectURI})
	if err != nil {
		t.Fatalf("CreateApp: %v", err)
	}
	app2, _, err := svc.CreateApp(context.Background(), ownerID, "App2", "", "", []string{testRedirectURI})
	if err != nil {
		t.Fatalf("CreateApp: %v", err)
	}
	if app1.ClientID == app2.ClientID {
		t.Error("consecutive CreateApp calls must produce unique client IDs")
	}
}

// Without a registered redirect URI, a code could be sent wherever the
// authorize request says.
func TestOAuthApp_CreateApp_RejectsBadRedirectURIs(t *testing.T) {
	svc, ownerID := newOAuthSvc(t)

	tests := []struct {
		name string
		uris []string
	}{
		{"none", nil},
		{"empty entry", []string{testRedirectURI, ""}},
		{"relative", []string{"/cb"}},
		{"no host", []string{"https:///cb"}},
		{"non-http scheme", []string{"javascript:alert(1)"}},
		{"fragment", []string{"https://example.com/cb#x"}},
		{"comma", []string{"https://example.com/cb,https://attacker.example/cb"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, _, err := svc.CreateApp(context.Background(), ownerID, "Bad URI App", "", "", tt.uris)
			if !errors.Is(err, service.ErrInvalidRedirectURI) {
				t.Errorf("want ErrInvalidRedirectURI, got %v", err)
			}
		})
	}
}

// TestOAuthApp_IsRedirectURIAllowed_Registered verifies that a redirect URI in the
// app's registered list is allowed.
func TestOAuthApp_IsRedirectURIAllowed_Registered(t *testing.T) {
	svc, _ := newOAuthSvc(t)
	// Use a mock app with a hardcoded redirect URI list (no DB needed).
	db := testutil.OpenTestDB(t)
	suffix := testutil.UniqueSuffix(t)
	ownerID := testutil.SeedUser(t, db, suffix)
	svcLocal := service.NewOAuthAppService(store.NewOAuthAppStore(db), store.NewOAuthAuthorizationStore(db), store.NewUserStore(db))
	_ = svc

	app, _, err := svcLocal.CreateApp(context.Background(), ownerID, "URI App", "", "",
		[]string{"https://example.com/cb"})
	if err != nil {
		t.Fatalf("CreateApp: %v", err)
	}
	if !svcLocal.IsRedirectURIAllowed(app, "https://example.com/cb") {
		t.Error("registered redirect URI must be allowed")
	}
	if svcLocal.IsRedirectURIAllowed(app, "https://attacker.com/cb") {
		t.Error("unregistered redirect URI must be denied")
	}
}

// Apps registered before redirect URIs were required may have none.
func TestOAuthApp_IsRedirectURIAllowed_NoURIs_Denied(t *testing.T) {
	svc := service.NewOAuthAppService(nil, nil, nil)
	if svc.IsRedirectURIAllowed(&model.OAuthApp{}, "https://any.example.com/callback") {
		t.Error("app with no redirect URIs must not allow any URI")
	}
	if svc.IsRedirectURIAllowed(&model.OAuthApp{RedirectURIs: []string{testRedirectURI, ""}}, "") {
		t.Error("an empty redirect URI must never be allowed")
	}
}

func TestOAuthApp_Authorize_AppWithoutRedirectURIs_Error(t *testing.T) {
	db := testutil.OpenTestDB(t)
	suffix := testutil.UniqueSuffix(t)
	ownerID := testutil.SeedUser(t, db, suffix)
	svc := service.NewOAuthAppService(store.NewOAuthAppStore(db), store.NewOAuthAuthorizationStore(db), store.NewUserStore(db))

	legacy := &model.OAuthApp{OwnerID: ownerID, Name: "Legacy App", ClientID: "legacy_" + suffix, ClientSecret: "x"}
	if err := store.NewOAuthAppStore(db).Create(context.Background(), legacy); err != nil {
		t.Fatalf("Create: %v", err)
	}
	for _, uri := range []string{"https://attacker.example/cb", ""} {
		if _, err := svc.Authorize(context.Background(), legacy.ID, ownerID, uri, []string{model.ScopeRepoRead}, legacy); err == nil {
			t.Errorf("Authorize(%q) must fail for an app with no registered redirect URIs", uri)
		}
	}
}

// TestOAuthApp_AuthorizeAndExchange_FullFlow verifies the complete OAuth authorization
// code flow: Authorize produces a code, ExchangeCode validates it and returns a token.
func TestOAuthApp_AuthorizeAndExchange_FullFlow(t *testing.T) {
	db := testutil.OpenTestDB(t)
	suffix := testutil.UniqueSuffix(t)
	ownerID := testutil.SeedUser(t, db, suffix)
	userID := testutil.SeedUser(t, db, "oauth_user_"+suffix)

	svc := service.NewOAuthAppService(
		store.NewOAuthAppStore(db),
		store.NewOAuthAuthorizationStore(db),
		store.NewUserStore(db),
	)

	app, rawSecret, err := svc.CreateApp(context.Background(), ownerID,
		"Flow App", "", "", []string{testRedirectURI})
	if err != nil {
		t.Fatalf("CreateApp: %v", err)
	}

	// Step 1: Authorize — generate an auth code.
	code, err := svc.Authorize(context.Background(), app.ID, userID,
		testRedirectURI, []string{model.ScopeRepoRead}, app)
	if err != nil {
		t.Fatalf("Authorize: %v", err)
	}
	if code == "" {
		t.Fatal("Authorize must return a non-empty code")
	}

	// Step 2: Exchange the code for a bearer token.
	token, err := svc.ExchangeCode(context.Background(), app.ClientID, rawSecret, code, testRedirectURI)
	if err != nil {
		t.Fatalf("ExchangeCode: %v", err)
	}
	if token == "" {
		t.Error("ExchangeCode must return a non-empty bearer token")
	}
}

// TestOAuthApp_ExchangeCode_WrongSecret_Fails verifies that ExchangeCode rejects
// an incorrect client secret even when the code itself is valid.
func TestOAuthApp_ExchangeCode_WrongSecret_Fails(t *testing.T) {
	db := testutil.OpenTestDB(t)
	suffix := testutil.UniqueSuffix(t)
	ownerID := testutil.SeedUser(t, db, suffix)
	userID := testutil.SeedUser(t, db, "oauth_wrong_"+suffix)

	svc := service.NewOAuthAppService(
		store.NewOAuthAppStore(db),
		store.NewOAuthAuthorizationStore(db),
		store.NewUserStore(db),
	)

	app, _, err := svc.CreateApp(context.Background(), ownerID, "Secret App", "", "", []string{testRedirectURI})
	if err != nil {
		t.Fatalf("CreateApp: %v", err)
	}
	code, err := svc.Authorize(context.Background(), app.ID, userID, testRedirectURI, []string{model.ScopeRepoRead}, app)
	if err != nil {
		t.Fatalf("Authorize: %v", err)
	}

	_, err = svc.ExchangeCode(context.Background(), app.ClientID, "wrongsecret", code, testRedirectURI)
	if err == nil {
		t.Error("ExchangeCode must fail with wrong client_secret")
	}
}

// TestOAuthApp_ResolveOAuthToken verifies that after a successful token exchange,
// ResolveOAuthToken maps the raw token back to the original user and granted scopes.
func TestOAuthApp_ResolveOAuthToken(t *testing.T) {
	db := testutil.OpenTestDB(t)
	suffix := testutil.UniqueSuffix(t)
	ownerID := testutil.SeedUser(t, db, suffix)
	userID := testutil.SeedUser(t, db, "oauth_resolve_"+suffix)

	svc := service.NewOAuthAppService(
		store.NewOAuthAppStore(db),
		store.NewOAuthAuthorizationStore(db),
		store.NewUserStore(db),
	)

	app, rawSecret, err := svc.CreateApp(context.Background(), ownerID, "Resolve App", "", "", []string{testRedirectURI})
	if err != nil {
		t.Fatalf("CreateApp: %v", err)
	}
	code, err := svc.Authorize(context.Background(), app.ID, userID, testRedirectURI, []string{model.ScopeRepoRead}, app)
	if err != nil {
		t.Fatalf("Authorize: %v", err)
	}
	token, err := svc.ExchangeCode(context.Background(), app.ClientID, rawSecret, code, testRedirectURI)
	if err != nil {
		t.Fatalf("ExchangeCode: %v", err)
	}

	user, scopes, err := svc.ResolveOAuthToken(context.Background(), token)
	if err != nil {
		t.Fatalf("ResolveOAuthToken: %v", err)
	}
	if user.ID != userID || user.Username != "testuser_oauth_resolve_"+suffix {
		t.Errorf("want user %d, got %d (%q)", userID, user.ID, user.Username)
	}
	if !slices.Equal(scopes, []string{model.ScopeRepoRead}) {
		t.Errorf("want scopes [%s], got %v", model.ScopeRepoRead, scopes)
	}
}

// TestOAuthApp_Authorize_UnknownScope_Error verifies that a grant cannot be stored
// for a scope the middleware would not recognise.
func TestOAuthApp_Authorize_UnknownScope_Error(t *testing.T) {
	svc, ownerID := newOAuthSvc(t)
	app, _, err := svc.CreateApp(context.Background(), ownerID, "Scope App", "", "", []string{testRedirectURI})
	if err != nil {
		t.Fatalf("CreateApp: %v", err)
	}
	_, err = svc.Authorize(context.Background(), app.ID, ownerID, testRedirectURI, []string{model.ScopeRepoRead, "admin"}, app)
	if !errors.Is(err, service.ErrInvalidScope) {
		t.Errorf("want ErrInvalidScope, got %v", err)
	}
}

func TestOAuthApp_ParseScopes(t *testing.T) {
	svc := service.NewOAuthAppService(nil, nil, nil)
	got, err := svc.ParseScopes(context.Background(), " repo:read  issues:write repo:read ")
	if err != nil {
		t.Fatalf("ParseScopes: %v", err)
	}
	if want := []string{model.ScopeRepoRead, model.ScopeIssuesWrite}; !slices.Equal(got, want) {
		t.Errorf("want %v, got %v", want, got)
	}
	if _, err := svc.ParseScopes(context.Background(), "repo:read user"); !errors.Is(err, service.ErrInvalidScope) {
		t.Errorf("want ErrInvalidScope for unknown scope, got %v", err)
	}
}

// TestOAuthApp_Authorize_DisallowedRedirectURI_Error verifies that Authorize returns
// an error when the provided redirect URI is not in the app's registered list.
func TestOAuthApp_Authorize_DisallowedRedirectURI_Error(t *testing.T) {
	db := testutil.OpenTestDB(t)
	suffix := testutil.UniqueSuffix(t)
	ownerID := testutil.SeedUser(t, db, suffix)
	userID := testutil.SeedUser(t, db, "oauth_uri_"+suffix)

	svc := service.NewOAuthAppService(
		store.NewOAuthAppStore(db),
		store.NewOAuthAuthorizationStore(db),
		store.NewUserStore(db),
	)

	app, _, err := svc.CreateApp(context.Background(), ownerID, "Strict App", "", "",
		[]string{"https://allowed.example.com/cb"})
	if err != nil {
		t.Fatalf("CreateApp: %v", err)
	}

	_, err = svc.Authorize(context.Background(), app.ID, userID,
		"https://attacker.example.com/steal", []string{model.ScopeRepoRead}, app)
	if err == nil {
		t.Error("Authorize must reject a redirect URI not in the app's registered list")
	}
	if !strings.Contains(err.Error(), "redirect_uri") {
		t.Errorf("error must mention redirect_uri, got %q", err.Error())
	}
}

// Client credentials prove who is asking, not that the code was issued to them.
func TestOAuthApp_ExchangeCode_OtherAppsCode_Fails(t *testing.T) {
	db := testutil.OpenTestDB(t)
	suffix := testutil.UniqueSuffix(t)
	ownerID := testutil.SeedUser(t, db, suffix)
	userID := testutil.SeedUser(t, db, "oauth_bind_"+suffix)
	svc := service.NewOAuthAppService(store.NewOAuthAppStore(db), store.NewOAuthAuthorizationStore(db), store.NewUserStore(db))
	ctx := context.Background()

	victim, victimSecret, err := svc.CreateApp(ctx, ownerID, "Victim App", "", "", []string{testRedirectURI})
	if err != nil {
		t.Fatalf("CreateApp: %v", err)
	}
	// Same redirect URI as the victim, so only the app binding can refuse it.
	attacker, attackerSecret, err := svc.CreateApp(ctx, ownerID, "Attacker App", "", "", []string{testRedirectURI})
	if err != nil {
		t.Fatalf("CreateApp: %v", err)
	}
	code, err := svc.Authorize(ctx, victim.ID, userID, testRedirectURI, []string{model.ScopeRepoWrite}, victim)
	if err != nil {
		t.Fatalf("Authorize: %v", err)
	}

	_, err = svc.ExchangeCode(ctx, attacker.ClientID, attackerSecret, code, testRedirectURI)
	if err == nil || !strings.Contains(err.Error(), "invalid or expired") {
		t.Fatalf("want invalid or expired error, got %v", err)
	}
	if _, err := svc.ExchangeCode(ctx, victim.ClientID, victimSecret, code, testRedirectURI); err != nil {
		t.Errorf("the issuing app must still redeem its code: %v", err)
	}
}

// RFC 6749 §4.1.3: the token request must repeat the authorize request's redirect_uri.
func TestOAuthApp_ExchangeCode_RedirectURIMustMatch(t *testing.T) {
	db := testutil.OpenTestDB(t)
	suffix := testutil.UniqueSuffix(t)
	ownerID := testutil.SeedUser(t, db, suffix)
	userID := testutil.SeedUser(t, db, "oauth_redir_"+suffix)
	svc := service.NewOAuthAppService(store.NewOAuthAppStore(db), store.NewOAuthAuthorizationStore(db), store.NewUserStore(db))
	ctx := context.Background()

	other := "https://example.com/other"
	app, secret, err := svc.CreateApp(ctx, ownerID, "Two URI App", "", "", []string{testRedirectURI, other})
	if err != nil {
		t.Fatalf("CreateApp: %v", err)
	}
	code, err := svc.Authorize(ctx, app.ID, userID, testRedirectURI, []string{model.ScopeRepoRead}, app)
	if err != nil {
		t.Fatalf("Authorize: %v", err)
	}

	for _, uri := range []string{other, ""} {
		if _, err := svc.ExchangeCode(ctx, app.ClientID, secret, code, uri); err == nil {
			t.Errorf("ExchangeCode with redirect_uri %q must fail", uri)
		}
	}
	if _, err := svc.ExchangeCode(ctx, app.ClientID, secret, code, testRedirectURI); err != nil {
		t.Errorf("ExchangeCode with the authorized redirect_uri: %v", err)
	}
}
