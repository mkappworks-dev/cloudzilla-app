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

// parseGPGKey reads one armored public key and checks it can verify commits at now.
func parseGPGKey(armored string, now time.Time) (*model.GPGKey, error) {
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
	if e.Revoked(now) {
		return nil, ErrGPGKeyRevoked
	}

	self, _ := e.PrimarySelfSignature()
	key := &model.GPGKey{
		KeyID:       fmt.Sprintf("%016x", e.PrimaryKey.KeyId),
		Fingerprint: fmt.Sprintf("%x", e.PrimaryKey.Fingerprint),
		PublicKey:   armored,
		ExpiresAt:   keyExpiry(e.PrimaryKey.CreationTime, self),
		Emails:      uidEmails(e),
	}
	if key.ExpiresAt != nil && !now.Before(*key.ExpiresAt) {
		return nil, ErrGPGKeyExpired
	}

	canSign := self == nil || !self.FlagsValid || self.FlagSign
	for _, sk := range e.Subkeys {
		sub := model.GPGSubkey{
			KeyID:       fmt.Sprintf("%016x", sk.PublicKey.KeyId),
			Fingerprint: fmt.Sprintf("%x", sk.PublicKey.Fingerprint),
			CanSign:     sk.Sig != nil && sk.Sig.FlagsValid && sk.Sig.FlagSign,
			ExpiresAt:   keyExpiry(sk.PublicKey.CreationTime, sk.Sig),
		}
		if len(sk.Revocations) > 0 {
			t := sk.Revocations[0].CreationTime
			sub.RevokedAt = &t
		}
		live := sub.RevokedAt == nil && (sub.ExpiresAt == nil || now.Before(*sub.ExpiresAt))
		canSign = canSign || (sub.CanSign && live)
		key.Subkeys = append(key.Subkeys, sub)
	}
	if !canSign {
		return nil, ErrGPGNoSigningKey
	}
	return key, nil
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
