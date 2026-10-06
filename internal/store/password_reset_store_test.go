package store_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"testing"
	"time"

	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/store"
	"github.com/mkappworks-dev/cloudzilla-app/internal/testutil"
)

func TestPasswordResetStore_ConsumeRefusesSuspendedAccount(t *testing.T) {
	db := testutil.OpenTestDB(t)
	ctx := context.Background()
	resets := store.NewPasswordResetStore(db)
	userID, _ := testutil.SeedUserWithPassword(t, db, testutil.UniqueSuffix(t), "password1")
	sum := sha256.Sum256([]byte(testutil.UniqueSuffix(t)))
	hash := hex.EncodeToString(sum[:])
	if _, err := resets.Issue(ctx, userID, hash, model.PasswordResetByAdmin, time.Hour, 0); err != nil {
		t.Fatalf("Issue: %v", err)
	}
	if link, err := resets.Lookup(ctx, hash); err != nil || link.State != model.PasswordResetPending {
		t.Fatalf("Lookup = %+v, %v; want pending", link, err)
	}

	// Suspended after the page loaded but before the form was sent.
	testutil.Exec(t, db, `UPDATE users SET suspended_at = NOW() WHERE id = $1`, userID)
	state, u, _, err := resets.Consume(ctx, hash, "new-hash")
	if err != nil {
		t.Fatalf("Consume: %v", err)
	}
	if state != model.PasswordResetInvalid || u != nil {
		t.Errorf("Consume = %s, %v; want invalid and no user", state, u)
	}
	var passwordHash string
	if err := db.QueryRow(`SELECT password_hash FROM users WHERE id = $1`, userID).Scan(&passwordHash); err != nil {
		t.Fatal(err)
	}
	if passwordHash == "new-hash" {
		t.Error("Consume changed a suspended account's password")
	}
}
