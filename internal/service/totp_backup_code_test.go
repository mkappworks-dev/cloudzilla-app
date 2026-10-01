package service_test

// Integration tests for TOTP backup codes. All tests require TEST_DATABASE_DSN and skip otherwise.

import (
	"context"
	"testing"

	"github.com/mkappworks-dev/cloudzilla-app/internal/service"
	"github.com/mkappworks-dev/cloudzilla-app/internal/store"
	"github.com/mkappworks-dev/cloudzilla-app/internal/testutil"
)

func TestTOTPService_VerifyBackupCode_ConsumesOnlyThatCode(t *testing.T) {
	db := testutil.OpenTestDB(t)
	ctx := context.Background()
	users := store.NewUserStore(db)
	totp := service.NewTOTPService(users)
	userID := testutil.SeedUser(t, db, testutil.UniqueSuffix(t))

	raw, hashes, err := totp.GenerateBackupCodes()
	if err != nil {
		t.Fatalf("GenerateBackupCodes: %v", err)
	}
	if err := users.SetBackupCodes(ctx, userID, hashes); err != nil {
		t.Fatalf("SetBackupCodes: %v", err)
	}

	if err := totp.VerifyBackupCode(ctx, userID, raw[0]); err != nil {
		t.Fatalf("first use of a backup code: %v", err)
	}
	if err := totp.VerifyBackupCode(ctx, userID, raw[0]); err == nil {
		t.Fatal("second use of the same backup code: want an error, got nil")
	}
	for _, code := range raw[1:] {
		if err := totp.VerifyBackupCode(ctx, userID, code); err != nil {
			t.Errorf("unused backup code %q: %v", code, err)
		}
	}
}
