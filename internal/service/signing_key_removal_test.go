package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/ProtonMail/go-crypto/openpgp/packet"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/testutil"
)

func TestReadGPGKey_RevocationReasonDecidesRevokeOrRemove(t *testing.T) {
	for _, tc := range []struct {
		name        string
		reason      packet.ReasonForRevocation
		wantRevoked bool
	}{
		{"no reason is treated as compromised", packet.NoReason, true},
		{"compromised", packet.KeyCompromised, true},
		{"superseded is a removal", packet.KeySuperseded, false},
		{"retired is a removal", packet.KeyRetired, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e := gpgFixture{created: time.Now().Add(-time.Hour), signSub: true}.entity(t, "rhea@example.com")
			if err := e.RevokeKey(tc.reason, "test", nil); err != nil {
				t.Fatal(err)
			}
			p, err := readGPGKey(armorKey(t, e, false), time.Now())
			if err != nil {
				t.Fatal(err)
			}
			if gotRevoked := p.key.RevokedAt != nil; gotRevoked != tc.wantRevoked || (p.key.RemovedAt != nil) == tc.wantRevoked {
				t.Errorf("revoked=%v removed=%v, want revoked=%v and the other state clear", p.key.RevokedAt, p.key.RemovedAt, tc.wantRevoked)
			}
			if _, err := parseGPGKey(armorKey(t, e, false), time.Now()); !errors.Is(err, ErrGPGKeyRevoked) {
				t.Errorf("a new key with this revocation: got %v, want ErrGPGKeyRevoked", err)
			}
		})
	}
}

func TestReadGPGKey_SubkeyRevocationIsPerSubkey(t *testing.T) {
	e := gpgFixture{created: time.Now().Add(-time.Hour), signSub: true}.entity(t, "rhea@example.com")
	if err := e.RevokeSubkey(&e.Subkeys[1], packet.KeyCompromised, "lost", nil); err != nil {
		t.Fatal(err)
	}
	p, err := readGPGKey(armorKey(t, e, false), time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if p.key.RevokedAt != nil || p.key.Subkeys[1].RevokedAt == nil || p.key.Subkeys[0].RevokedAt != nil {
		t.Errorf("primary revoked=%v, subkeys revoked=%v/%v, want only the signing subkey", p.key.RevokedAt, p.key.Subkeys[0].RevokedAt, p.key.Subkeys[1].RevokedAt)
	}
}

func TestSigningKeyService_UpsertGPGKey(t *testing.T) {
	svc, _, seed := newSigningKeyService(t)
	ctx := context.Background()
	me, other := seed("a"), seed("b")
	e := gpgFixture{created: time.Now().Add(-time.Hour), signSub: true}.entity(t, "rhea@example.com")
	stale := armorKey(t, e, false)

	key, created, err := svc.UpsertGPGKey(ctx, me, stale)
	if err != nil || !created {
		t.Fatalf("first post: created=%v (%v)", created, err)
	}

	if err := e.AddUserId("Rhea", "", "second@example.com", nil); err != nil {
		t.Fatal(err)
	}
	updated, created, err := svc.UpsertGPGKey(ctx, me, armorKey(t, e, false))
	if err != nil || created || updated.ID != key.ID || len(updated.Emails) != 2 {
		t.Fatalf("update: id %d created=%v emails=%v (%v), want the same row with both UIDs", updated.ID, created, updated.Emails, err)
	}
	if _, _, err := svc.UpsertGPGKey(ctx, other, stale); !errors.Is(err, ErrSigningKeyTaken) {
		t.Errorf("another account posting the key: got %v, want ErrSigningKeyTaken", err)
	}

	// A superseded certificate removes the key; the stale copy posted afterwards restores it, as the owner asked.
	if err := e.RevokeKey(packet.KeySuperseded, "new key", nil); err != nil {
		t.Fatal(err)
	}
	got, _, err := svc.UpsertGPGKey(ctx, me, armorKey(t, e, false))
	if err != nil || got.Status != model.SigningKeyRemoved || got.RemovedAt == nil || got.RevokedAt != nil {
		t.Fatalf("after a superseded certificate: %+v (%v), want removed", got, err)
	}
	if got, _, err = svc.UpsertGPGKey(ctx, me, stale); err != nil || got.Status != model.SigningKeyActive {
		t.Fatalf("stale copy of a removed key: %+v (%v), want it restored", got, err)
	}

	// A compromised certificate is final.
	e2 := gpgFixture{created: time.Now().Add(-time.Hour), signSub: true}.entity(t, "rhea@example.com")
	if err := e2.RevokeKey(packet.KeyCompromised, "stolen", nil); err != nil {
		t.Fatal(err)
	}
	if err := svc.RevokeGPGKey(ctx, key.ID, me); err != nil {
		t.Fatal(err)
	}
	if _, _, err := svc.UpsertGPGKey(ctx, me, stale); !errors.Is(err, ErrSigningKeyRevoked) {
		t.Errorf("stale copy of a revoked key: got %v, want ErrSigningKeyRevoked", err)
	}
	if err := svc.RestoreGPGKey(ctx, key.ID, me); !errors.Is(err, ErrSigningKeyRevoked) {
		t.Errorf("restore of a revoked key: got %v, want ErrSigningKeyRevoked", err)
	}
	if err := svc.RestoreGPGKey(ctx, key.ID, other); !errors.Is(err, ErrSigningKeyNotFound) {
		t.Errorf("restore of another account's key: got %v, want ErrSigningKeyNotFound", err)
	}
}

func TestSigningKeyService_UpsertGPGKey_SubkeyRevocationIsSticky(t *testing.T) {
	svc, _, seed := newSigningKeyService(t)
	ctx := context.Background()
	me := seed("a")
	e := gpgFixture{created: time.Now().Add(-time.Hour), signSub: true}.entity(t, "rhea@example.com")
	stale := armorKey(t, e, false)
	if _, err := svc.AddGPGKey(ctx, me, stale); err != nil {
		t.Fatal(err)
	}
	if err := e.RevokeSubkey(&e.Subkeys[1], packet.KeyCompromised, "lost", nil); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.AddGPGKey(ctx, me, armorKey(t, e, false)); err != nil {
		t.Fatal(err)
	}
	got, _, err := svc.UpsertGPGKey(ctx, me, stale)
	if err != nil {
		t.Fatal(err)
	}
	if got.Subkeys[1].RevokedAt == nil {
		t.Error("a stale block without the subkey revocation cleared it")
	}
}

func TestSigningKeyService_UpsertSSHKey(t *testing.T) {
	svc, _, seed := newSigningKeyService(t)
	ctx := context.Background()
	me := seed("a")
	_, pub := testutil.NewSigningKey(t)

	key, created, err := svc.UpsertSSHKey(ctx, me, "laptop", pub)
	if err != nil || !created {
		t.Fatal(created, err)
	}
	if err := svc.RemoveSSHKey(ctx, key.ID, me); err != nil {
		t.Fatal(err)
	}
	again, created, err := svc.UpsertSSHKey(ctx, me, "laptop", pub)
	if err != nil || created || again.ID != key.ID || again.Status != model.SigningKeyActive {
		t.Fatalf("posting a removed key: %+v created=%v (%v), want it restored", again, created, err)
	}
	if err := svc.RevokeSSHKey(ctx, key.ID, me); err != nil {
		t.Fatal(err)
	}
	if _, _, err := svc.UpsertSSHKey(ctx, me, "laptop", pub); !errors.Is(err, ErrSigningKeyRevoked) {
		t.Errorf("posting a revoked key: got %v, want ErrSigningKeyRevoked", err)
	}
	if keys, _ := svc.ListSSHKeys(ctx, me, false); len(keys) != 1 || keys[0].Status != model.SigningKeyRevoked {
		t.Errorf("default list = %+v, want the revoked key still visible", keys)
	}
}

// TestSignatureService_RemoveAndRevokeStates is the state matrix of decision 13: a removed key binds only commits dated
// at or after its removal, a revoked key binds all of them.
func TestSignatureService_RemoveAndRevokeStates(t *testing.T) {
	keys, sigs, _, uid := signingStack(t)
	ctx := context.Background()
	db := testutil.OpenTestDB(t)

	type kind struct {
		name   string
		commit *object.Commit
		add    func() int64
		table  string
		remove func(id int64) error
		revoke func(id int64) error
		restor func(id int64) error
	}
	kinds := []kind{
		{
			name: "gpg subkey", commit: fixtureCommit(t, "gpg-subkey"), table: "gpg_keys",
			add: func() int64 {
				k, err := keys.AddGPGKey(ctx, uid, string(fixtureBytes(t, "rhea.pub.asc")))
				if err != nil {
					t.Fatal(err)
				}
				return k.ID
			},
			remove: func(id int64) error { return keys.RemoveGPGKey(ctx, id, uid) },
			revoke: func(id int64) error { return keys.RevokeGPGKey(ctx, id, uid) },
			restor: func(id int64) error { return keys.RestoreGPGKey(ctx, id, uid) },
		},
		{
			name: "ssh", commit: fixtureCommit(t, "ssh-ed25519"), table: "ssh_signing_keys",
			add: func() int64 {
				k, err := keys.AddSSHKey(ctx, uid, "laptop", string(fixtureBytes(t, "ed25519.pub")))
				if err != nil {
					t.Fatal(err)
				}
				return k.ID
			},
			remove: func(id int64) error { return keys.RemoveSSHKey(ctx, id, uid) },
			revoke: func(id int64) error { return keys.RevokeSSHKey(ctx, id, uid) },
			restor: func(id int64) error { return keys.RestoreSSHKey(ctx, id, uid) },
		},
	}
	for _, k := range kinds {
		t.Run(k.name, func(t *testing.T) {
			id := k.add()
			at := k.commit.Committer.When
			verify := func() SignatureResult {
				t.Helper()
				got, err := sigs.Verify(ctx, []*object.Commit{k.commit})
				if err != nil {
					t.Fatal(err)
				}
				return got[k.commit.Hash]
			}
			setRemoved := func(when time.Time) {
				testutil.Exec(t, db, `UPDATE `+k.table+` SET removed_at = $1 WHERE id = $2`, when, id)
			}
			want := func(label string, status SignatureStatus, reason SignatureReason) {
				t.Helper()
				if got := verify(); got.Status != status || got.Reason != reason {
					t.Fatalf("%s: %s/%q, want %s/%q", label, got.Status, got.Reason, status, reason)
				}
			}

			want("active", SignatureVerified, "")
			setRemoved(at.Add(time.Hour))
			want("removed after the commit", SignatureVerified, "")
			setRemoved(at.Add(-time.Hour))
			want("removed before the commit", SignatureUnverified, ReasonKeyRemoved)
			setRemoved(at)
			want("removed at the commit's own second", SignatureUnverified, ReasonKeyRemoved)

			if err := k.restor(id); err != nil {
				t.Fatal(err)
			}
			want("restored", SignatureVerified, "")

			if err := k.remove(id); err != nil {
				t.Fatal(err)
			}
			if err := k.revoke(id); err != nil {
				t.Fatal(err)
			}
			setRemoved(at.Add(time.Hour))
			want("revoked and removed after the commit", SignatureUnverified, ReasonKeyRevoked)
			if err := k.restor(id); !errors.Is(err, ErrSigningKeyRevoked) {
				t.Fatalf("restore of a revoked key: %v, want ErrSigningKeyRevoked", err)
			}
		})
	}
}

func TestGPGKeyState_RemovalAndRevocationOfSigningSubkey(t *testing.T) {
	at := time.Now()
	past, future := at.Add(-time.Hour), at.Add(time.Hour)
	key := func(sub model.GPGSubkey) *model.GPGKey { return &model.GPGKey{Subkeys: []model.GPGSubkey{sub}} }
	for _, tc := range []struct {
		name string
		key  *model.GPGKey
		want SignatureReason
	}{
		{"subkey revoked after the commit still binds it", key(model.GPGSubkey{RevokedAt: &future}), ReasonKeyRevoked},
		{"subkey removed before the commit", key(model.GPGSubkey{RemovedAt: &past}), ReasonKeyRemoved},
		{"subkey removed after the commit", key(model.GPGSubkey{RemovedAt: &future}), ""},
		{"primary removed before the commit", &model.GPGKey{RemovedAt: &past, Subkeys: []model.GPGSubkey{{}}}, ReasonKeyRemoved},
	} {
		if got, _ := gpgKeyState(tc.key, 0, at); got != tc.want {
			t.Errorf("%s: %q, want %q", tc.name, got, tc.want)
		}
	}
}
