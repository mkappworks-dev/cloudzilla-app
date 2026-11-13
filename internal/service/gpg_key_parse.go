package service

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/ProtonMail/go-crypto/openpgp"
	"github.com/ProtonMail/go-crypto/openpgp/armor"
	"github.com/ProtonMail/go-crypto/openpgp/packet"
	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
)

var (
	ErrInvalidGPGKey   = errors.New("not a valid armored GPG public key")
	ErrGPGPrivateKey   = errors.New("that is a private key block; paste the public key")
	ErrGPGKeyExpired   = errors.New("this GPG key has expired")
	ErrGPGKeyRevoked   = errors.New("this GPG key has been revoked")
	ErrGPGNoSigningKey = errors.New("this GPG key has no key that can sign")
)

// parsedGPGKey is a readable key block before the add or update rules judge it.
type parsedGPGKey struct {
	key *model.GPGKey
	// revokedNow is go-crypto's verdict at the parse time, which counts a future-dated or unexpired revocation.
	revokedNow bool
	canSign    bool
}

// parseGPGKey reads one armored public key and checks a new key can verify commits at now.
func parseGPGKey(armored string, now time.Time) (*model.GPGKey, error) {
	p, err := readGPGKey(armored, now)
	if err != nil {
		return nil, err
	}
	if p.revokedNow || p.key.RevokedAt != nil || p.key.RemovedAt != nil {
		return nil, ErrGPGKeyRevoked
	}
	if p.key.ExpiresAt != nil && !now.Before(*p.key.ExpiresAt) {
		return nil, ErrGPGKeyExpired
	}
	if !p.canSign {
		return nil, ErrGPGNoSigningKey
	}
	return p.key, nil
}

// readGPGKey decodes the block and records its revocations, without refusing a key for being expired, revoked or
// unable to sign: updating a key the account already owns must accept such a block.
func readGPGKey(armored string, now time.Time) (*parsedGPGKey, error) {
	armored = strings.TrimSpace(armored)
	block, err := armor.Decode(strings.NewReader(armored))
	if err != nil {
		return nil, ErrInvalidGPGKey
	}
	if block.Type == openpgp.PrivateKeyType {
		return nil, ErrGPGPrivateKey
	}
	ring, err := openpgp.ReadArmoredKeyRing(strings.NewReader(armored))
	if err != nil || len(ring) != 1 {
		return nil, ErrInvalidGPGKey
	}
	e := ring[0]
	if e.PrivateKey != nil {
		return nil, ErrGPGPrivateKey
	}

	self, _ := e.PrimarySelfSignature()
	key := &model.GPGKey{
		KeyID:       fmt.Sprintf("%016x", e.PrimaryKey.KeyId),
		Fingerprint: fmt.Sprintf("%x", e.PrimaryKey.Fingerprint),
		PublicKey:   armored,
		ExpiresAt:   keyExpiry(e.PrimaryKey.CreationTime, self),
		Emails:      uidEmails(e),
	}
	key.RevokedAt, key.RemovedAt = revocationState(e.Revocations)

	canSign := self == nil || !self.FlagsValid || self.FlagSign
	for _, sk := range e.Subkeys {
		sub := model.GPGSubkey{
			KeyID:       fmt.Sprintf("%016x", sk.PublicKey.KeyId),
			Fingerprint: fmt.Sprintf("%x", sk.PublicKey.Fingerprint),
			CanSign:     sk.Sig != nil && sk.Sig.FlagsValid && sk.Sig.FlagSign,
			ExpiresAt:   keyExpiry(sk.PublicKey.CreationTime, sk.Sig),
		}
		sub.RevokedAt, sub.RemovedAt = revocationState(sk.Revocations)
		live := sub.RevokedAt == nil && sub.RemovedAt == nil && (sub.ExpiresAt == nil || now.Before(*sub.ExpiresAt))
		canSign = canSign || (sub.CanSign && live)
		key.Subkeys = append(key.Subkeys, sub)
	}
	return &parsedGPGKey{key: key, revokedNow: e.Revoked(now), canSign: canSign}, nil
}

// revocationState maps a key's verified revocation signatures onto our two states. The signer's stated reason decides:
// superseded or retired means the key was simply put away (removed at the signature's time), anything else, including
// no reason, is treated as compromised, because wrongly distrusting a key is the safe error.
func revocationState(revs []*packet.Signature) (revoked, removed *time.Time) {
	for _, r := range revs {
		t := r.CreationTime
		if r.RevocationReason != nil && (*r.RevocationReason == packet.KeySuperseded || *r.RevocationReason == packet.KeyRetired) {
			if removed == nil || t.Before(*removed) {
				removed = &t
			}
			continue
		}
		if revoked == nil || t.Before(*revoked) {
			revoked = &t
		}
	}
	return revoked, removed
}

func keyExpiry(created time.Time, sig *packet.Signature) *time.Time {
	if sig == nil || sig.KeyLifetimeSecs == nil || *sig.KeyLifetimeSecs == 0 {
		return nil
	}
	t := created.Add(time.Duration(*sig.KeyLifetimeSecs) * time.Second)
	return &t
}

func uidEmails(e *openpgp.Entity) []string {
	seen := map[string]bool{}
	var out []string
	for _, id := range e.Identities {
		if id.UserId == nil || len(id.Revocations) > 0 {
			continue
		}
		email := strings.ToLower(strings.TrimSpace(id.UserId.Email))
		if email != "" && !seen[email] {
			seen[email] = true
			out = append(out, email)
		}
	}
	sort.Strings(out)
	return out
}
