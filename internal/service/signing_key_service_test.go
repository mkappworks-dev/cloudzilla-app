package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/store"
	"github.com/mkappworks-dev/cloudzilla-app/internal/testutil"
)

func newSigningKeyService(t *testing.T) (*SigningKeyService, *SSHKeyService, func(string) int64) {
	t.Helper()
	db := testutil.OpenTestDB(t)
	st := store.New(db)
	seed := func(tag string) int64 { return testutil.SeedUser(t, db, tag+testutil.UniqueSuffix(t)) }
	return NewSigningKeyService(st.GPGKey, st.SSHSigningKey), NewSSHKeyService(st.SSHKey, st.User, st.DeployKey), seed
}

func TestSigningKeyService_AddGPGKey(t *testing.T) {
	svc, _, seed := newSigningKeyService(t)
	ctx := context.Background()
	me, other := seed("a"), seed("b")
	armored := armorKey(t, gpgFixture{created: time.Now().Add(-time.Hour), signSub: true}.entity(t, "rhea@example.com"), false)

	key, err := svc.AddGPGKey(ctx, me, armored)
	if err != nil {
		t.Fatal(err)
	}
	list, err := svc.ListGPGKeys(ctx, me, false)
	if err != nil || len(list) != 1 {
		t.Fatalf("list = %v (%v), want the one key", list, err)
	}
	got := list[0]
	if got.Fingerprint != key.Fingerprint || len(got.Subkeys) != 2 || len(got.Emails) != 1 || got.Emails[0] != "rhea@example.com" {
		t.Errorf("stored key = %+v, want fingerprint, 2 subkeys (encrypt + sign) and the UID email", got)
	}

	if _, err := svc.AddGPGKey(ctx, other, armored); !errors.Is(err, ErrSigningKeyTaken) {
		t.Errorf("same key on another account: got %v, want ErrSigningKeyTaken", err)
	}
	if again, created, err := svc.UpsertGPGKey(ctx, me, armored); err != nil || created || again.ID != key.ID {
		t.Errorf("same key twice: got id %d created=%v (%v), want the existing row updated in place", again.ID, created, err)
	}
	if theirs, _ := svc.ListGPGKeys(ctx, other, false); len(theirs) != 0 {
		t.Errorf("the refused add left %d keys on the other account", len(theirs))
	}

	if err := svc.RemoveGPGKey(ctx, key.ID, other); !errors.Is(err, ErrSigningKeyNotFound) {
		t.Errorf("remove another user's key: got %v, want ErrSigningKeyNotFound", err)
	}
	if err := svc.RemoveGPGKey(ctx, key.ID, me); err != nil {
		t.Fatal(err)
	}
	if visible, _ := svc.ListGPGKeys(ctx, me, false); len(visible) != 0 {
		t.Errorf("a removed key is listed by default: %v", visible)
	}
	all, _ := svc.ListGPGKeys(ctx, me, true)
	if len(all) != 1 || all[0].Status != model.SigningKeyRemoved || len(all[0].Subkeys) != 2 || len(all[0].Emails) != 1 {
		t.Errorf("removed key with include = %+v, want the row kept with status removed, subkeys and emails", all)
	}
}

func TestSigningKeyService_AccountDeletionCascades(t *testing.T) {
	svc, _, seed := newSigningKeyService(t)
	ctx := context.Background()
	user := seed("a")
	_, pub := testutil.NewSigningKey(t)
	if _, err := svc.AddSSHKey(ctx, user, "k", pub); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.AddGPGKey(ctx, user, armorKey(t, gpgFixture{created: time.Now().Add(-time.Hour), signSub: true}.entity(t, "a@example.com"), false)); err != nil {
		t.Fatal(err)
	}

	db := testutil.OpenTestDB(t)
	testutil.Exec(t, db, `DELETE FROM users WHERE id = $1`, user)
	for _, table := range []string{"gpg_keys", "ssh_signing_keys"} {
		var n int
		if err := db.QueryRow(`SELECT COUNT(*) FROM `+table+` WHERE user_id = $1`, user).Scan(&n); err != nil || n != 0 {
			t.Errorf("%s rows after account deletion = %d (%v), want 0", table, n, err)
		}
	}
}

func TestSigningKeyService_AddSSHKey(t *testing.T) {
	svc, auth, seed := newSigningKeyService(t)
	ctx := context.Background()
	me, other := seed("a"), seed("b")
	_, pub := testutil.NewSigningKey(t)

	if _, err := auth.AddKey(ctx, me, "auth", pub); err != nil {
		t.Fatal(err)
	}
	key, err := svc.AddSSHKey(ctx, me, "signing", pub)
	if err != nil {
		t.Fatalf("a key that is also an auth key: %v", err)
	}
	if len(key.Fingerprint) < 8 || key.Fingerprint[:7] != "SHA256:" {
		t.Errorf("fingerprint = %q, want SHA256:...", key.Fingerprint)
	}
	if keys, _ := auth.ListByUser(ctx, me); len(keys) != 1 {
		t.Errorf("ssh_keys has %d rows, want the auth key untouched", len(keys))
	}
	if again, created, err := svc.UpsertSSHKey(ctx, me, "again", pub); err != nil || created || again.ID != key.ID {
		t.Errorf("same key twice: got id %d created=%v (%v), want the existing row", again.ID, created, err)
	}
	if _, err := svc.AddSSHKey(ctx, other, "theirs", pub); !errors.Is(err, ErrSigningKeyTaken) {
		t.Errorf("same key on another account: got %v, want ErrSigningKeyTaken", err)
	}
	if _, err := svc.AddSSHKey(ctx, me, "bad", "ssh-ed25519 nope"); !errors.Is(err, ErrInvalidSigningKey) {
		t.Errorf("garbage key: got %v, want ErrInvalidSigningKey", err)
	}
	if err := svc.RemoveSSHKey(ctx, key.ID, other); !errors.Is(err, ErrSigningKeyNotFound) {
		t.Errorf("remove another user's key: got %v, want ErrSigningKeyNotFound", err)
	}
	if err := svc.RemoveSSHKey(ctx, key.ID, me); err != nil {
		t.Fatal(err)
	}
}
