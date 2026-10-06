// Package secretbox encrypts secrets the server must read back later, such
// as a mirror's upstream token, under security.secret_key.
package secretbox

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hkdf"
	"crypto/rand"
	"crypto/sha256"
	"errors"
	"fmt"
)

// MinKeyLen is the shortest security.secret_key accepted.
const MinKeyLen = 32

const version1 byte = 0x01

var (
	// ErrNoKey means security.secret_key is not set.
	ErrNoKey = errors.New("security.secret_key is not set")
	// ErrUndecryptable covers a wrong key, a wrong purpose, tampering and
	// unknown formats alike, so callers can't tell them apart.
	ErrUndecryptable = errors.New("stored secret can't be decrypted")
)

// Box seals and opens secrets. A nil *Box is valid and returns ErrNoKey.
type Box struct {
	key []byte
}

// New returns nil for an empty key, so the server runs without one.
func New(key []byte) (*Box, error) {
	if len(key) == 0 {
		return nil, nil
	}
	if len(key) < MinKeyLen {
		return nil, fmt.Errorf("security.secret_key must be at least %d bytes, got %d", MinKeyLen, len(key))
	}
	return &Box{key: append([]byte(nil), key...)}, nil
}

// Seal encrypts plaintext for purpose. The result is a version byte, a nonce
// and the AES-256-GCM ciphertext; purpose is bound as additional data.
func (b *Box) Seal(purpose string, plaintext []byte) ([]byte, error) {
	aead, err := b.aead(purpose)
	if err != nil {
		return nil, err
	}
	out := make([]byte, 1+aead.NonceSize(), 1+aead.NonceSize()+len(plaintext)+aead.Overhead())
	out[0] = version1
	if _, err := rand.Read(out[1:]); err != nil {
		return nil, fmt.Errorf("nonce: %w", err)
	}
	return aead.Seal(out, out[1:], plaintext, []byte(purpose)), nil
}

// Open decrypts a value from Seal with the same purpose.
func (b *Box) Open(purpose string, sealed []byte) ([]byte, error) {
	aead, err := b.aead(purpose)
	if err != nil {
		return nil, err
	}
	if len(sealed) < 1+aead.NonceSize()+aead.Overhead() || sealed[0] != version1 {
		return nil, ErrUndecryptable
	}
	nonce, ct := sealed[1:1+aead.NonceSize()], sealed[1+aead.NonceSize():]
	plaintext, err := aead.Open(nil, nonce, ct, []byte(purpose))
	if err != nil {
		return nil, ErrUndecryptable
	}
	return plaintext, nil
}

// aead derives a separate key per purpose, so a leak of one use's key or a
// cross-purpose oracle can't reach another's secrets.
func (b *Box) aead(purpose string) (cipher.AEAD, error) {
	if b == nil {
		return nil, ErrNoKey
	}
	sub, err := hkdf.Key(sha256.New, b.key, nil, "cloudzilla/"+purpose, 32)
	if err != nil {
		return nil, fmt.Errorf("derive key: %w", err)
	}
	block, err := aes.NewCipher(sub)
	if err != nil {
		return nil, fmt.Errorf("cipher: %w", err)
	}
	return cipher.NewGCM(block)
}
