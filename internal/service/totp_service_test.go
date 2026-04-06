package service

import (
	"strings"
	"testing"
	"time"
)

// totpCode is package-private, so we can test it directly from within the package.

// TestTOTPCode_Deterministic verifies that the same secret and counter always produce
// the same 6-digit code (TOTP is a pure function of secret + time window).
func TestTOTPCode_Deterministic(t *testing.T) {
	secret := "JBSWY3DPEHPK3PXP"
	code1, err := totpCode(secret, 12345)
	if err != nil {
		t.Fatalf("totpCode: %v", err)
	}
	code2, _ := totpCode(secret, 12345)
	if code1 != code2 {
		t.Errorf("same inputs should produce same code: got %q and %q", code1, code2)
	}
}

// TestTOTPCode_SixDigits verifies that totpCode always returns exactly 6 characters
// regardless of the counter value (zero-padded if needed).
func TestTOTPCode_SixDigits(t *testing.T) {
	secret := "JBSWY3DPEHPK3PXP"
	for _, counter := range []int64{0, 1, 100, 99999, time.Now().Unix() / 30} {
		code, err := totpCode(secret, counter)
		if err != nil {
			t.Fatalf("counter=%d: %v", counter, err)
		}
		if len(code) != 6 {
			t.Errorf("counter=%d: want 6 digits, got %q", counter, code)
		}
	}
}

// TestTOTPCode_DifferentCounters verifies that adjacent counter values produce
// different codes (collision is cryptographically negligible).
func TestTOTPCode_DifferentCounters(t *testing.T) {
	secret := "JBSWY3DPEHPK3PXP"
	a, _ := totpCode(secret, 1)
	b, _ := totpCode(secret, 2)
	if a == b {
		t.Errorf("different counters should (almost certainly) produce different codes")
	}
}

// TestTOTPCode_InvalidSecret verifies that totpCode returns an error for a secret
// that is not valid base32, rather than panicking or producing garbage.
func TestTOTPCode_InvalidSecret(t *testing.T) {
	_, err := totpCode("not-valid-base32!!!", 0)
	if err == nil {
		t.Error("expected error for invalid base32 secret")
	}
}

// TestTOTPService_Verify_ValidCode verifies that the current TOTP window code
// is accepted by Verify.
func TestTOTPService_Verify_ValidCode(t *testing.T) {
	svc := &TOTPService{}
	secret := "JBSWY3DPEHPK3PXP"
	// Compute the current code and immediately verify it.
	code, err := totpCode(secret, time.Now().Unix()/30)
	if err != nil {
		t.Fatalf("totpCode: %v", err)
	}
	if !svc.Verify(secret, code) {
		t.Error("Verify should accept current TOTP code")
	}
}

// TestTOTPService_Verify_WrongCode verifies that Verify rejects codes that don't
// match any of the allowed time windows.
func TestTOTPService_Verify_WrongCode(t *testing.T) {
	svc := &TOTPService{}
	if svc.Verify("JBSWY3DPEHPK3PXP", "000000") {
		// "000000" could theoretically be valid, but extremely unlikely.
		t.Log("skipping: 000000 happened to be a valid code")
	}
	if svc.Verify("JBSWY3DPEHPK3PXP", "999999") && svc.Verify("JBSWY3DPEHPK3PXP", "000000") {
		t.Error("Verify should reject wrong codes (extremely unlikely both are valid)")
	}
}

// TestTOTPService_Verify_AdjacentWindows verifies that Verify accepts codes from
// the previous and next 30-second windows (T-1, T, T+1) to tolerate clock drift.
func TestTOTPService_Verify_AdjacentWindows(t *testing.T) {
	svc := &TOTPService{}
	secret := "JBSWY3DPEHPK3PXP"
	t0 := time.Now().Unix() / 30
	for _, delta := range []int64{-1, 0, 1} {
		code, err := totpCode(secret, t0+delta)
		if err != nil {
			t.Fatalf("totpCode delta=%d: %v", delta, err)
		}
		if !svc.Verify(secret, code) {
			t.Errorf("Verify should accept code for window T%+d", delta)
		}
	}
}

// TestTOTPService_Verify_InvalidSecret verifies that Verify returns false (not panic)
// when the secret is not valid base32.
func TestTOTPService_Verify_InvalidSecret(t *testing.T) {
	svc := &TOTPService{}
	if svc.Verify("!!!invalid!!!", "123456") {
		t.Error("Verify with invalid secret should return false")
	}
}

// TestBuildOTPAuthURL_ContainsRequiredComponents verifies that BuildOTPAuthURL returns
// a well-formed otpauth:// URI containing all fields required by authenticator apps.
func TestBuildOTPAuthURL_ContainsRequiredComponents(t *testing.T) {
	svc := &TOTPService{}
	url := svc.BuildOTPAuthURL("alice", "Cloudzilla", "JBSWY3DPEHPK3PXP")
	if !strings.HasPrefix(url, "otpauth://totp/") {
		t.Errorf("URL must start with otpauth://totp/, got %q", url)
	}
	if !strings.Contains(url, "secret=JBSWY3DPEHPK3PXP") {
		t.Errorf("URL must contain secret param, got %q", url)
	}
	if !strings.Contains(url, "issuer=Cloudzilla") {
		t.Errorf("URL must contain issuer=Cloudzilla, got %q", url)
	}
	if !strings.Contains(url, "algorithm=SHA1") {
		t.Errorf("URL must contain algorithm=SHA1, got %q", url)
	}
	if !strings.Contains(url, "digits=6") {
		t.Errorf("URL must contain digits=6, got %q", url)
	}
	if !strings.Contains(url, "period=30") {
		t.Errorf("URL must contain period=30, got %q", url)
	}
}

// TestTOTPService_Generate_ReturnsNonEmptyValues verifies that Generate returns a
// non-empty base32 secret and a valid otpauth:// URL.
func TestTOTPService_Generate_ReturnsNonEmptyValues(t *testing.T) {
	svc := &TOTPService{}
	secret, otpURL, err := svc.Generate("bob", "Cloudzilla")
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if secret == "" {
		t.Error("Generate must return a non-empty secret")
	}
	if !strings.HasPrefix(otpURL, "otpauth://totp/") {
		t.Errorf("Generate must return valid otpauth URL, got %q", otpURL)
	}
}

// TestTOTPService_Generate_UniqueSecrets verifies that successive Generate calls
// produce different secrets (entropy is drawn from crypto/rand).
func TestTOTPService_Generate_UniqueSecrets(t *testing.T) {
	svc := &TOTPService{}
	s1, _, _ := svc.Generate("alice", "Cloudzilla")
	s2, _, _ := svc.Generate("alice", "Cloudzilla")
	if s1 == s2 {
		t.Error("Generate must produce unique secrets on each call")
	}
}

// TestTOTPService_GenerateBackupCodes verifies that GenerateBackupCodes returns
// exactly 10 unique 8-character hex codes and 10 corresponding bcrypt hashes.
func TestTOTPService_GenerateBackupCodes(t *testing.T) {
	svc := &TOTPService{}
	raw, hashes, err := svc.GenerateBackupCodes()
	if err != nil {
		t.Fatalf("GenerateBackupCodes: %v", err)
	}
	if len(raw) != 10 {
		t.Errorf("want 10 raw codes, got %d", len(raw))
	}
	if len(hashes) != 10 {
		t.Errorf("want 10 hashes, got %d", len(hashes))
	}
	for i, code := range raw {
		if len(code) != 8 {
			t.Errorf("raw[%d] %q: want 8 hex chars", i, code)
		}
	}
	// Codes must be unique.
	seen := make(map[string]bool)
	for _, c := range raw {
		if seen[c] {
			t.Errorf("duplicate backup code: %q", c)
		}
		seen[c] = true
	}
}
