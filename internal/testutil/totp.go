package testutil

import (
	"context"
	"crypto/hmac"
	"crypto/sha1"
	"database/sql"
	"encoding/base32"
	"encoding/binary"
	"fmt"
	"testing"
	"time"
)

// TestTOTPSecret is a valid base32 TOTP secret for EnableTOTP.
const TestTOTPSecret = "JBSWY3DPEHPK3PXPJBSWY3DPEHPK3PXP"

// EnableTOTP turns on two-factor authentication for userID with TestTOTPSecret.
func EnableTOTP(t *testing.T, db *sql.DB, userID int64) {
	t.Helper()
	Exec(t, db, `UPDATE users SET totp_enabled = TRUE, totp_secret = $1 WHERE id = $2`, TestTOTPSecret, userID)
}

// TOTPCode returns the current RFC 6238 code for secret. It is written apart
// from TOTPService so a bug there cannot make the tests agree with it.
func TOTPCode(t *testing.T, secret string) string {
	t.Helper()
	key, err := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(secret)
	if err != nil {
		t.Fatalf("decode totp secret: %v", err)
	}
	msg := make([]byte, 8)
	binary.BigEndian.PutUint64(msg, uint64(time.Now().Unix()/30))
	mac := hmac.New(sha1.New, key)
	mac.Write(msg)
	sum := mac.Sum(nil)
	offset := sum[len(sum)-1] & 0x0f
	code := binary.BigEndian.Uint32(sum[offset:offset+4]) & 0x7fffffff
	return fmt.Sprintf("%06d", code%1_000_000)
}

// SeedPasswordlessUser inserts a user with no password, as Google sign-up creates
// them, linked to googleID. The user is deleted when the test ends.
func SeedPasswordlessUser(t *testing.T, db *sql.DB, suffix, googleID string) int64 {
	t.Helper()
	var id int64
	err := db.QueryRowContext(context.Background(),
		`INSERT INTO users (username, email, password_hash, oauth_provider, oauth_id, is_invited)
		 VALUES ($1, $2, '', 'google', $3, true) RETURNING id`,
		"testnopw_"+suffix, "testnopw_"+suffix+"@test.invalid", googleID,
	).Scan(&id)
	if err != nil {
		t.Fatalf("SeedPasswordlessUser: %v", err)
	}
	deleteLast(t, db, id)
	return id
}
