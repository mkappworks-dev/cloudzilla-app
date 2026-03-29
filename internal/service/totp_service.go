package service

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1"
	"database/sql"
	"encoding/base32"
	"encoding/binary"
	"fmt"
	"math"
	"net/url"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/mkappworks/cloudzilla/internal/model"
	"github.com/mkappworks/cloudzilla/internal/store"
	"golang.org/x/crypto/bcrypt"
)

// TOTPService handles all TOTP (RFC 6238) operations.
type TOTPService struct {
	store *store.UserStore
}

// NewTOTPService creates a new TOTPService.
func NewTOTPService(s *store.UserStore) *TOTPService {
	return &TOTPService{store: s}
}

// Generate creates a new TOTP secret for the given user/issuer and returns the
// base32-encoded secret and an otpauth:// URL suitable for QR code display.
func (s *TOTPService) Generate(username, issuer string) (secret, otpAuthURL string, err error) {
	raw := make([]byte, 20)
	if _, err = rand.Read(raw); err != nil {
		return "", "", fmt.Errorf("generate totp secret: %w", err)
	}
	secret = base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(raw)
	otpAuthURL = buildOTPAuthURL(username, issuer, secret)
	return secret, otpAuthURL, nil
}

// BuildOTPAuthURL constructs the otpauth:// URL from a known secret.
func (s *TOTPService) BuildOTPAuthURL(username, issuer, secret string) string {
	return buildOTPAuthURL(username, issuer, secret)
}

func buildOTPAuthURL(username, issuer, secret string) string {
	label := issuer + ":" + username
	return "otpauth://totp/" + url.PathEscape(label) +
		"?secret=" + url.QueryEscape(secret) +
		"&issuer=" + url.QueryEscape(issuer) +
		"&algorithm=SHA1&digits=6&period=30"
}

// totpCode computes the 6-digit TOTP code for the given base32 secret and T counter.
func totpCode(secret string, t int64) (string, error) {
	key, err := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(
		strings.ToUpper(secret),
	)
	if err != nil {
		return "", fmt.Errorf("decode totp secret: %w", err)
	}

	msg := make([]byte, 8)
	binary.BigEndian.PutUint64(msg, uint64(t))

	mac := hmac.New(sha1.New, key)
	mac.Write(msg)
	h := mac.Sum(nil)

	offset := h[len(h)-1] & 0x0f
	code := (int64(h[offset]&0x7f)<<24 |
		int64(h[offset+1])<<16 |
		int64(h[offset+2])<<8 |
		int64(h[offset+3])) % int64(math.Pow10(6))

	return fmt.Sprintf("%06d", code), nil
}

// Verify checks whether code matches the current TOTP window (T-1, T, T+1).
func (s *TOTPService) Verify(secret, code string) bool {
	t := time.Now().Unix() / 30
	for _, delta := range []int64{-1, 0, 1} {
		expected, err := totpCode(secret, t+delta)
		if err != nil {
			continue
		}
		if hmac.Equal([]byte(expected), []byte(code)) {
			return true
		}
	}
	return false
}

// Enable verifies the provided code against the pending secret, then persists
// the secret, enables TOTP, generates backup codes, and stores their hashes.
// Returns the 10 raw backup codes to be shown to the user once.
func (s *TOTPService) Enable(ctx context.Context, userID int64, secret, code string) ([]string, error) {
	if !s.Verify(secret, code) {
		return nil, fmt.Errorf("invalid totp code")
	}

	rawCodes, hashes, err := s.GenerateBackupCodes()
	if err != nil {
		return nil, err
	}

	if err := s.store.SetTOTPEnabled(ctx, userID, true, secret); err != nil {
		return nil, fmt.Errorf("enable totp: %w", err)
	}
	if err := s.store.SetBackupCodes(ctx, userID, hashes); err != nil {
		return nil, fmt.Errorf("store backup codes: %w", err)
	}
	return rawCodes, nil
}

// Disable verifies the provided code and then disables TOTP, clearing the secret.
func (s *TOTPService) Disable(ctx context.Context, userID int64, code string) error {
	u, err := s.store.GetByIDWithTOTP(ctx, userID)
	if err != nil {
		return fmt.Errorf("get user: %w", err)
	}
	if !u.TOTPEnabled {
		return fmt.Errorf("totp not enabled")
	}
	if !s.Verify(u.TOTPSecret.String, code) {
		return fmt.Errorf("invalid totp code")
	}
	return s.store.SetTOTPEnabled(ctx, userID, false, "")
}

// GenerateBackupCodes creates 10 random 8-hex-char backup codes and returns both
// the raw codes (to show the user) and their bcrypt hashes (to store).
func (s *TOTPService) GenerateBackupCodes() (rawCodes, hashes []string, err error) {
	rawCodes = make([]string, 10)
	hashes = make([]string, 10)
	buf := make([]byte, 4)
	for i := 0; i < 10; i++ {
		if _, err = rand.Read(buf); err != nil {
			return nil, nil, fmt.Errorf("generate backup code: %w", err)
		}
		rawCodes[i] = fmt.Sprintf("%08x", buf)
		hash, err := bcrypt.GenerateFromPassword([]byte(rawCodes[i]), bcrypt.MinCost)
		if err != nil {
			return nil, nil, fmt.Errorf("hash backup code: %w", err)
		}
		hashes[i] = string(hash)
	}
	return rawCodes, hashes, nil
}

// VerifyBackupCode checks if the given raw backup code matches one of the stored
// hashes. On match it removes that code from the DB and returns nil.
func (s *TOTPService) VerifyBackupCode(ctx context.Context, userID int64, code string) error {
	u, err := s.store.GetByIDWithTOTP(ctx, userID)
	if err != nil {
		return fmt.Errorf("get user: %w", err)
	}
	matchIdx := -1
	for i, hash := range u.TOTPBackupCodes {
		if bcrypt.CompareHashAndPassword([]byte(hash), []byte(code)) == nil {
			matchIdx = i
			break
		}
	}
	if matchIdx < 0 {
		return fmt.Errorf("invalid backup code")
	}
	remaining := make([]string, 0, len(u.TOTPBackupCodes)-1)
	for i, h := range u.TOTPBackupCodes {
		if i != matchIdx {
			remaining = append(remaining, h)
		}
	}
	return s.store.SetBackupCodes(ctx, userID, remaining)
}

// StoreSecret persists a newly generated secret without enabling TOTP yet.
func (s *TOTPService) StoreSecret(ctx context.Context, userID int64, secret string) error {
	return s.store.SetTOTPSecret(ctx, userID, secret)
}

// GetUserTOTPState returns whether TOTP is enabled and the stored secret for userID.
func (s *TOTPService) GetUserTOTPState(ctx context.Context, userID int64) (enabled bool, secret sql.NullString, err error) {
	u, err := s.store.GetByIDWithTOTP(ctx, userID)
	if err != nil {
		return false, sql.NullString{}, err
	}
	return u.TOTPEnabled, u.TOTPSecret, nil
}

// StoreGetUser retrieves a user with TOTP fields by ID.
func (s *TOTPService) StoreGetUser(ctx context.Context, userID int64) (*model.User, error) {
	return s.store.GetByIDWithTOTP(ctx, userID)
}

// GeneratePendingToken creates a short-lived JWT (5 minutes) encoding the userID.
func (s *TOTPService) GeneratePendingToken(userID int64, jwtSecret string) (string, error) {
	claims := jwt.MapClaims{
		"sub": userID,
		"exp": time.Now().Add(5 * time.Minute).Unix(),
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	signed, err := token.SignedString([]byte(jwtSecret))
	if err != nil {
		return "", fmt.Errorf("sign pending token: %w", err)
	}
	return signed, nil
}
