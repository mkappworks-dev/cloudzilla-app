package service

import (
	"bytes"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/ProtonMail/go-crypto/openpgp"
	"github.com/ProtonMail/go-crypto/openpgp/armor"
	"github.com/ProtonMail/go-crypto/openpgp/packet"
)

type gpgFixture struct {
	lifetime uint32
	created  time.Time
	signSub  bool
	revoke   bool
	noSign   bool
}

func (f gpgFixture) entity(t *testing.T, email string) *openpgp.Entity {
	t.Helper()
	cfg := &packet.Config{
		Algorithm:       packet.PubKeyAlgoEdDSA,
		KeyLifetimeSecs: f.lifetime,
		Time:            func() time.Time { return f.created },
	}
	e, err := openpgp.NewEntity("Rhea", "", email, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if f.signSub {
		if err := e.AddSigningSubkey(cfg); err != nil {
			t.Fatal(err)
		}
	}
	if f.noSign {
		// Re-flag the primary as certify-only, as keys with a dedicated signing subkey are.
		for _, id := range e.Identities {
			id.SelfSignature.FlagSign = false
			if err := id.SelfSignature.SignUserId(id.UserId.Id, e.PrimaryKey, e.PrivateKey, cfg); err != nil {
				t.Fatal(err)
			}
		}
	}
	if f.revoke {
		if err := e.RevokeKey(packet.NoReason, "test", cfg); err != nil {
			t.Fatal(err)
		}
	}
	return e
}

func armorKey(t *testing.T, e *openpgp.Entity, private bool) string {
	t.Helper()
	var buf bytes.Buffer
	typ := openpgp.PublicKeyType
	if private {
		typ = openpgp.PrivateKeyType
	}
	w, err := armor.Encode(&buf, typ, nil)
	if err != nil {
		t.Fatal(err)
	}
	if private {
		err = e.SerializePrivate(w, nil)
	} else {
		err = e.Serialize(w)
	}
	if err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.String()
}

func TestParseGPGKey_Valid(t *testing.T) {
	e := gpgFixture{created: time.Now().Add(-time.Hour), lifetime: 86400 * 30, signSub: true}.entity(t, "Rhea@Example.com")
	got, err := parseGPGKey(armorKey(t, e, false), time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if len(got.KeyID) != 16 || len(got.Fingerprint) != 40 {
		t.Errorf("key id %q fingerprint %q, want 16 and 40 hex chars", got.KeyID, got.Fingerprint)
	}
	if !slices.Equal(got.Emails, []string{"rhea@example.com"}) {
		t.Errorf("emails = %v, want the lower-cased UID email", got.Emails)
	}
	if got.ExpiresAt == nil || got.ExpiresAt.Before(time.Now()) {
		t.Errorf("expires_at = %v, want a future time", got.ExpiresAt)
	}
	var signing int
	for _, s := range got.Subkeys {
		if s.CanSign {
			signing++
		}
	}
	if signing != 1 {
		t.Errorf("signing subkeys = %d (of %d), want 1", signing, len(got.Subkeys))
	}
}

func TestParseGPGKey_Refusals(t *testing.T) {
	now := time.Now()
	fresh := now.Add(-time.Hour)
	tests := []struct {
		name string
		in   string
		want error
	}{
		{"not a key", "hello", ErrInvalidGPGKey},
		{"private block", armorKey(t, gpgFixture{created: fresh, signSub: true}.entity(t, "a@example.com"), true), ErrGPGPrivateKey},
		{"expired", armorKey(t, gpgFixture{created: now.Add(-48 * time.Hour), lifetime: 3600, signSub: true}.entity(t, "a@example.com"), false), ErrGPGKeyExpired},
		{"revoked", armorKey(t, gpgFixture{created: fresh, signSub: true, revoke: true}.entity(t, "a@example.com"), false), ErrGPGKeyRevoked},
		{"nothing can sign", armorKey(t, gpgFixture{created: fresh, noSign: true}.entity(t, "a@example.com"), false), ErrGPGNoSigningKey},
	}
	for _, tt := range tests {
		if _, err := parseGPGKey(tt.in, now); !errors.Is(err, tt.want) {
			t.Errorf("%s: err = %v, want %v", tt.name, err, tt.want)
		}
	}
}
