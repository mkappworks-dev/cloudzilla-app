package store_test

import (
	"context"
	"testing"

	"github.com/mkappworks-dev/cloudzilla-app/internal/store"
	"github.com/mkappworks-dev/cloudzilla-app/internal/testutil"
)

// The service refuses first, but the store must not strand a passwordless account either.
func TestUserStore_UnlinkOAuth_KeepsPasswordlessAccountsLinked(t *testing.T) {
	db := testutil.OpenTestDB(t)
	suffix := testutil.UniqueSuffix(t)
	userID := testutil.SeedPasswordlessUser(t, db, suffix, "g_store_"+suffix)

	unlinked, err := store.NewUserStore(db).UnlinkOAuth(context.Background(), userID, "google")
	if err != nil {
		t.Fatalf("UnlinkOAuth: %v", err)
	}
	if unlinked {
		t.Fatal("UnlinkOAuth removed the only sign-in of an account without a password")
	}
}
