package service_test

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/mkappworks/cloudzilla/internal/service"
	"github.com/mkappworks/cloudzilla/internal/store"
	"github.com/mkappworks/cloudzilla/internal/testutil"
)

func newAccessTokenSvc(t *testing.T) (*service.AccessTokenService, int64) {
	t.Helper()
	db := testutil.OpenTestDB(t)
	suffix := fmt.Sprintf("%d_%d", os.Getpid(), 10)
	userID := testutil.SeedUser(t, db, suffix)
	svc := service.NewAccessTokenService(
		store.NewAccessTokenStore(db),
		store.NewUserStore(db),
	)
	return svc, userID
}

func TestAccessToken_Generate_HasCZPPrefix(t *testing.T) {
	svc, userID := newAccessTokenSvc(t)
	raw, tok, err := svc.Generate(context.Background(), userID, "test-token", nil, nil)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if !strings.HasPrefix(raw, "czp_") {
		t.Errorf("raw token must start with czp_, got %q", raw[:min(10, len(raw))])
	}
	if tok.ID == 0 {
		t.Error("generated token must have a non-zero ID")
	}
}

func TestAccessToken_Generate_HashNotExposed(t *testing.T) {
	svc, userID := newAccessTokenSvc(t)
	raw, tok, err := svc.Generate(context.Background(), userID, "test-hash", nil, nil)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if tok.TokenHash == raw {
		t.Error("stored hash must differ from raw token")
	}
	if tok.TokenHash == "" {
		t.Error("token hash must not be empty")
	}
}

func TestAccessToken_Validate_MatchesGenerated(t *testing.T) {
	svc, userID := newAccessTokenSvc(t)
	raw, _, err := svc.Generate(context.Background(), userID, "validate-test", nil, nil)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	tok, user, err := svc.Validate(context.Background(), raw)
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if tok == nil || user == nil {
		t.Fatal("Validate returned nil token or user")
	}
	if user.ID != userID {
		t.Errorf("validated user ID %d, want %d", user.ID, userID)
	}
}

func TestAccessToken_Validate_WrongToken_Fails(t *testing.T) {
	svc, _ := newAccessTokenSvc(t)
	_, _, err := svc.Validate(context.Background(), "czp_notarealtoken00000000000000000000000000000000000000000000000000")
	if err == nil {
		t.Error("Validate must fail for unknown token")
	}
}

func TestAccessToken_Validate_NoCZPPrefix_Fails(t *testing.T) {
	svc, _ := newAccessTokenSvc(t)
	_, _, err := svc.Validate(context.Background(), "not_a_pat_token")
	if err == nil {
		t.Error("Validate must reject tokens without czp_ prefix")
	}
}

func TestAccessToken_Validate_Expired_Fails(t *testing.T) {
	svc, userID := newAccessTokenSvc(t)
	past := time.Now().Add(-time.Hour)
	raw, _, err := svc.Generate(context.Background(), userID, "expired-test", nil, &past)
	if err != nil {
		t.Fatalf("Generate with expiry: %v", err)
	}
	_, _, err = svc.Validate(context.Background(), raw)
	if err == nil {
		t.Error("Validate must reject expired tokens")
	}
}

func TestAccessToken_Validate_FutureExpiry_Passes(t *testing.T) {
	// Re-use the same DB and user from a fresh call
	db := testutil.OpenTestDB(t)
	suffix := fmt.Sprintf("%d_expok", os.Getpid())
	userID := testutil.SeedUser(t, db, suffix)
	svc2 := service.NewAccessTokenService(store.NewAccessTokenStore(db), store.NewUserStore(db))
	future := time.Now().Add(24 * time.Hour)
	raw, _, err := svc2.Generate(context.Background(), userID, "future-expiry", nil, &future)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	_ = raw
	_, _, err = svc2.Validate(context.Background(), raw)
	if err != nil {
		t.Errorf("Validate must accept token with future expiry: %v", err)
	}
}

func TestAccessToken_Generate_UniqueTokens(t *testing.T) {
	svc, userID := newAccessTokenSvc(t)
	raw1, _, _ := svc.Generate(context.Background(), userID, "tok1", nil, nil)
	raw2, _, _ := svc.Generate(context.Background(), userID, "tok2", nil, nil)
	if raw1 == raw2 {
		t.Error("consecutive Generate calls must produce unique tokens")
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
