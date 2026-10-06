package service

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
)

const linkTokenBytes = 32

// newLinkToken returns a token for a single-use link, and the SHA-256
// that is stored in its place.
func newLinkToken() (raw, hash string, err error) {
	b := make([]byte, linkTokenBytes)
	if _, err := rand.Read(b); err != nil {
		return "", "", fmt.Errorf("generate link token: %w", err)
	}
	raw = base64.RawURLEncoding.EncodeToString(b)
	hash, _ = hashLinkToken(raw)
	return raw, hash, nil
}

// Anything but a well-formed token is refused before it reaches the database.
func hashLinkToken(raw string) (string, bool) {
	if len(raw) != base64.RawURLEncoding.EncodedLen(linkTokenBytes) {
		return "", false
	}
	if _, err := base64.RawURLEncoding.DecodeString(raw); err != nil {
		return "", false
	}
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:]), true
}
