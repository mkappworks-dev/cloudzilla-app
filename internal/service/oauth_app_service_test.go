package service_test

// Integration tests for OAuthAppService. All tests require TEST_DATABASE_DSN and skip otherwise.

import (
	"context"
	"strings"
	"testing"

	"github.com/mkappworks-dev/cloudzilla-app/internal/service"
	"github.com/mkappworks-dev/cloudzilla-app/internal/store"
	"github.com/mkappworks-dev/cloudzilla-app/internal/testutil"
)

// newOAuthSvc builds an OAuthAppService backed by the test database and seeds an owner user.
func newOAuthSvc(t *testing.T) (*service.OAuthAppService, int64) {
	t.Helper()
	db := testutil.OpenTestDB(t)
	suffix := testutil.UniqueSuffix(t)
	ownerID := testutil.SeedUser(t, db, suffix)
	svc := service.NewOAuthAppService(
		store.NewOAuthAppStore(db),
		store.NewOAuthAuthorizationStore(db),
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

	app1, _, _ := svc.CreateApp(context.Background(), ownerID, "App1", "", "", nil)
	app2, _, _ := svc.CreateApp(context.Background(), ownerID, "App2", "", "", nil)
	if app1 != nil && app2 != nil && app1.ClientID == app2.ClientID {
		t.Error("consecutive CreateApp calls must produce unique client IDs")
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
	svcLocal := service.NewOAuthAppService(store.NewOAuthAppStore(db), store.NewOAuthAuthorizationStore(db))
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

// TestOAuthApp_IsRedirectURIAllowed_NoURIs verifies that when an app has no registered
// redirect URIs, any URI is allowed (unrestricted app).
func TestOAuthApp_IsRedirectURIAllowed_NoURIs(t *testing.T) {
	svc, ownerID := newOAuthSvc(t)

	app, _, err := svc.CreateApp(context.Background(), ownerID, "Open App", "", "", nil)
	if err != nil {
		t.Fatalf("CreateApp: %v", err)
	}
	// No URIs registered — anything is allowed.
	if !svc.IsRedirectURIAllowed(app, "https://any.example.com/callback") {
		t.Error("app with no redirect URIs must allow any URI")
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
	)

	app, rawSecret, err := svc.CreateApp(context.Background(), ownerID,
		"Flow App", "", "", []string{"https://example.com/cb"})
	if err != nil {
		t.Fatalf("CreateApp: %v", err)
	}

	// Step 1: Authorize — generate an auth code.
	code, err := svc.Authorize(context.Background(), app.ID, userID,
		"https://example.com/cb", []string{"read"}, app)
	if err != nil {
		t.Fatalf("Authorize: %v", err)
	}
	if code == "" {
		t.Fatal("Authorize must return a non-empty code")
	}

	// Step 2: Exchange the code for a bearer token.
	token, err := svc.ExchangeCode(context.Background(), app.ClientID, rawSecret, code)
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
	)

	app, _, err := svc.CreateApp(context.Background(), ownerID, "Secret App", "", "", nil)
	if err != nil {
		t.Fatalf("CreateApp: %v", err)
	}
	code, err := svc.Authorize(context.Background(), app.ID, userID, "", []string{"read"}, app)
	if err != nil {
		t.Fatalf("Authorize: %v", err)
	}

	_, err = svc.ExchangeCode(context.Background(), app.ClientID, "wrongsecret", code)
	if err == nil {
		t.Error("ExchangeCode must fail with wrong client_secret")
	}
}

// TestOAuthApp_ResolveOAuthUserID verifies that after a successful token exchange,
// ResolveOAuthUserID maps the raw token back to the original user ID.
func TestOAuthApp_ResolveOAuthUserID(t *testing.T) {
	db := testutil.OpenTestDB(t)
	suffix := testutil.UniqueSuffix(t)
	ownerID := testutil.SeedUser(t, db, suffix)
	userID := testutil.SeedUser(t, db, "oauth_resolve_"+suffix)

	svc := service.NewOAuthAppService(
		store.NewOAuthAppStore(db),
		store.NewOAuthAuthorizationStore(db),
	)

	app, rawSecret, err := svc.CreateApp(context.Background(), ownerID, "Resolve App", "", "", nil)
	if err != nil {
		t.Fatalf("CreateApp: %v", err)
	}
	code, err := svc.Authorize(context.Background(), app.ID, userID, "", []string{"read"}, app)
	if err != nil {
		t.Fatalf("Authorize: %v", err)
	}
	token, err := svc.ExchangeCode(context.Background(), app.ClientID, rawSecret, code)
	if err != nil {
		t.Fatalf("ExchangeCode: %v", err)
	}

	resolvedUID, err := svc.ResolveOAuthUserID(context.Background(), token)
	if err != nil {
		t.Fatalf("ResolveOAuthUserID: %v", err)
	}
	if resolvedUID != userID {
		t.Errorf("want userID %d, got %d", userID, resolvedUID)
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
	)

	app, _, err := svc.CreateApp(context.Background(), ownerID, "Strict App", "", "",
		[]string{"https://allowed.example.com/cb"})
	if err != nil {
		t.Fatalf("CreateApp: %v", err)
	}

	_, err = svc.Authorize(context.Background(), app.ID, userID,
		"https://attacker.example.com/steal", []string{"read"}, app)
	if err == nil {
		t.Error("Authorize must reject a redirect URI not in the app's registered list")
	}
	if !strings.Contains(err.Error(), "redirect_uri") {
		t.Errorf("error must mention redirect_uri, got %q", err.Error())
	}
}
