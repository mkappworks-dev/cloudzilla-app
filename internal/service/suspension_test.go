package service_test

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/mkappworks-dev/cloudzilla-app/internal/config"
	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/service"
	"github.com/mkappworks-dev/cloudzilla-app/internal/store"
	"github.com/mkappworks-dev/cloudzilla-app/internal/testutil"
	gossh "golang.org/x/crypto/ssh"
)

func setSuspended(t *testing.T, db *sql.DB, userID int64, suspended bool) {
	t.Helper()
	if suspended {
		testutil.Exec(t, db, `UPDATE users SET suspended_at = NOW() WHERE id = $1`, userID)
	} else {
		testutil.Exec(t, db, `UPDATE users SET suspended_at = NULL WHERE id = $1`, userID)
	}
}

// Each way in is refused while suspended and works again once the flag clears.
func TestSuspension_RefusesEveryWayIn(t *testing.T) {
	db := testutil.OpenTestDB(t)
	ctx := context.Background()
	suffix := testutil.UniqueSuffix(t)
	userID, email := testutil.SeedUserWithPassword(t, db, suffix, "password1")
	users := store.NewUserStore(db)
	userSvc := service.NewUserService(users, config.AuthConfig{JWTSecret: "test-secret-32bytes-minimum-len!", JWTExpiry: time.Hour})

	tokens := service.NewAccessTokenService(store.NewAccessTokenStore(db), users)
	rawPAT, _, err := tokens.Generate(ctx, userID, "suspension", []string{model.ScopeRepoRead}, nil)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}

	sshKeys := service.NewSSHKeyService(store.NewSSHKeyStore(db), users)
	authorized := generateTestPublicKey(t)
	if _, err := sshKeys.AddKey(ctx, userID, "suspension", authorized); err != nil {
		t.Fatalf("AddKey: %v", err)
	}
	pubKey, _, _, _, err := gossh.ParseAuthorizedKey([]byte(authorized))
	if err != nil {
		t.Fatalf("parse key: %v", err)
	}

	oauth := service.NewOAuthAppService(store.NewOAuthAppStore(db), store.NewOAuthAuthorizationStore(db), users)
	app, secret, err := oauth.CreateApp(ctx, testutil.SeedUser(t, db, "oauth_owner_"+suffix), "Suspension App", "", "", []string{testRedirectURI})
	if err != nil {
		t.Fatalf("CreateApp: %v", err)
	}
	code, err := oauth.Authorize(ctx, app.ID, userID, testRedirectURI, []string{model.ScopeRepoRead}, app)
	if err != nil {
		t.Fatalf("Authorize: %v", err)
	}
	oauthToken, err := oauth.ExchangeCode(ctx, app.ClientID, secret, code, testRedirectURI)
	if err != nil {
		t.Fatalf("ExchangeCode: %v", err)
	}

	ways := map[string]func() error{
		"password":  func() error { _, _, err := userSvc.Authenticate(ctx, email, "password1"); return err },
		"totp step": func() error { _, err := userSvc.GenerateTokenForUser(ctx, userID); return err },
		"pat":       func() error { _, _, err := tokens.Validate(ctx, rawPAT); return err },
		"ssh key":   func() error { _, err := sshKeys.AuthenticatePublicKey(ctx, pubKey); return err },
		"oauth app": func() error { _, _, err := oauth.ResolveOAuthToken(ctx, oauthToken); return err },
		"session":   func() error { _, err := userSvc.SessionState(ctx, userID); return err },
	}

	setSuspended(t, db, userID, true)
	for name, try := range ways {
		err := try()
		if name == "session" {
			if !errors.Is(err, sql.ErrNoRows) {
				t.Errorf("%s while suspended: got %v, want sql.ErrNoRows", name, err)
			}
			continue
		}
		if !errors.Is(err, service.ErrAccountSuspended) {
			t.Errorf("%s while suspended: got %v, want ErrAccountSuspended", name, err)
		}
	}
	// The suspension shows only once the password is right.
	if _, _, err := userSvc.Authenticate(ctx, email, "wrong-password"); err == nil || errors.Is(err, service.ErrAccountSuspended) {
		t.Errorf("wrong password while suspended: got %v, want invalid credentials", err)
	}

	setSuspended(t, db, userID, false)
	for name, try := range ways {
		if err := try(); err != nil {
			t.Errorf("%s after unsuspension: %v", name, err)
		}
	}
}
