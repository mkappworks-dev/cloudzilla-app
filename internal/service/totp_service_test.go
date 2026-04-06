package service

import (
	"strings"
	"testing"
	"time"
)

// totpCode is package-private, so we can test it directly from within the package.

func TestTOTPCode_Deterministic(t *testing.T) {
	// The same secret + counter must always produce the same 6-digit code.
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

func TestTOTPCode_DifferentCounters(t *testing.T) {
	secret := "JBSWY3DPEHPK3PXP"
	a, _ := totpCode(secret, 1)
	b, _ := totpCode(secret, 2)
	if a == b {
		t.Errorf("different counters should (almost certainly) produce different codes")
	}
}

func TestTOTPCode_InvalidSecret(t *testing.T) {
	_, err := totpCode("not-valid-base32!!!", 0)
	if err == nil {
		t.Error("expected error for invalid base32 secret")
	}
}

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

func TestTOTPService_Verify_WrongCode(t *testing.T) {
	svc := &TOTPService{}
	if svc.Verify("JBSWY3DPEHPK3PXP", "000000") {
		// "000000" could theoretically be valid, but extremely unlikely.
		// Use a clearly wrong code instead.
		t.Log("skipping: 000000 happened to be a valid code")
	}
	if svc.Verify("JBSWY3DPEHPK3PXP", "999999") && svc.Verify("JBSWY3DPEHPK3PXP", "000000") {
		t.Error("Verify should reject wrong codes (extremely unlikely both are valid)")
	}
}

func TestTOTPService_Verify_AdjacentWindows(t *testing.T) {
	svc := &TOTPService{}
	secret := "JBSWY3DPEHPK3PXP"
	// T-1 and T+1 should also be accepted.
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

func TestTOTPService_Verify_InvalidSecret(t *testing.T) {
	svc := &TOTPService{}
	// Invalid base32 secret — Verify should return false, not panic.
	if svc.Verify("!!!invalid!!!", "123456") {
		t.Error("Verify with invalid secret should return false")
	}
}

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

func TestTOTPService_Generate_UniqueSecrets(t *testing.T) {
	svc := &TOTPService{}
	s1, _, _ := svc.Generate("alice", "Cloudzilla")
	s2, _, _ := svc.Generate("alice", "Cloudzilla")
	if s1 == s2 {
		t.Error("Generate must produce unique secrets on each call")
	}
}

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
