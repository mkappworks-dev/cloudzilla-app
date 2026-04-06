package service

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"testing"
	"time"

	"github.com/mkappworks/cloudzilla/internal/model"
)

// TestWebhookBackoff verifies that webhookBackoff returns the correct delay for each
// retry attempt number and that attempts beyond 5 clamp to 8 hours.
func TestWebhookBackoff(t *testing.T) {
	tests := []struct {
		attempt int
		wantMin time.Duration
		wantMax time.Duration
	}{
		{attempt: 1, wantMin: 1 * time.Minute, wantMax: 1*time.Minute + time.Second},
		{attempt: 2, wantMin: 5 * time.Minute, wantMax: 5*time.Minute + time.Second},
		{attempt: 3, wantMin: 30 * time.Minute, wantMax: 30*time.Minute + time.Second},
		{attempt: 4, wantMin: 2 * time.Hour, wantMax: 2*time.Hour + time.Second},
		{attempt: 5, wantMin: 8 * time.Hour, wantMax: 8*time.Hour + time.Second},
		// Any attempt beyond 5 should clamp to 8 hours
		{attempt: 10, wantMin: 8 * time.Hour, wantMax: 8*time.Hour + time.Second},
	}
	for _, tt := range tests {
		d := webhookBackoff(tt.attempt)
		if d < tt.wantMin || d > tt.wantMax {
			t.Errorf("webhookBackoff(%d) = %v, want [%v, %v]", tt.attempt, d, tt.wantMin, tt.wantMax)
		}
	}
}

// TestComputeHMAC_MatchesReference verifies that computeHMAC produces an HMAC-SHA256 hex
// digest that matches an independently computed reference value.
func TestComputeHMAC_MatchesReference(t *testing.T) {
	payload := []byte(`{"event":"push"}`)
	secret := "mysecret"

	// Compute the expected value independently using stdlib.
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(payload)
	want := hex.EncodeToString(mac.Sum(nil))

	got := computeHMAC(payload, secret)
	if got != want {
		t.Errorf("computeHMAC mismatch\n got  %q\n want %q", got, want)
	}
}

// TestComputeHMAC_DifferentPayloads verifies that different payloads produce different
// HMAC digests with the same secret (the signature is payload-dependent).
func TestComputeHMAC_DifferentPayloads(t *testing.T) {
	secret := "testsecret"
	sig1 := computeHMAC([]byte("payload-one"), secret)
	sig2 := computeHMAC([]byte("payload-two"), secret)
	if sig1 == sig2 {
		t.Error("different payloads must produce different HMAC signatures")
	}
}

// TestComputeHMAC_DifferentSecrets verifies that the same payload produces different
// HMAC digests for different secrets (the signature is secret-dependent).
func TestComputeHMAC_DifferentSecrets(t *testing.T) {
	payload := []byte("same payload")
	sig1 := computeHMAC(payload, "secret-a")
	sig2 := computeHMAC(payload, "secret-b")
	if sig1 == sig2 {
		t.Error("different secrets must produce different HMAC signatures")
	}
}

// TestComputeHMAC_EmptySecret_Succeeds verifies that computeHMAC does not panic
// when the secret is empty (some webhooks are unsigned).
func TestComputeHMAC_EmptySecret_Succeeds(t *testing.T) {
	got := computeHMAC([]byte("payload"), "")
	if got == "" {
		t.Error("computeHMAC with empty secret must return a non-empty hex digest")
	}
}

// TestComputeHMAC_IsHex verifies that computeHMAC always returns a lowercase hex string
// (the format expected by the X-Hub-Signature-256 header).
func TestComputeHMAC_IsHex(t *testing.T) {
	got := computeHMAC([]byte("test"), "secret")
	for _, c := range got {
		if !strings.ContainsRune("0123456789abcdef", c) {
			t.Errorf("computeHMAC returned non-hex character %q in %q", c, got)
		}
	}
	// SHA-256 produces 32 bytes → 64 hex characters.
	if len(got) != 64 {
		t.Errorf("want 64 hex chars, got %d", len(got))
	}
}

// TestPushPayload_ContainsRequiredFields verifies that PushPayload returns a map
// containing the repository name, branch, pusher, and head SHA fields used by consumers.
func TestPushPayload_ContainsRequiredFields(t *testing.T) {
	svc := &WebhookService{}
	repo := model.Repository{
		Name:      "myrepo",
		OwnerName: "alice",
	}
	payload := svc.PushPayload(repo, "alice", "main", "abc123def456")

	if payload["ref"] == nil {
		t.Error("PushPayload must include 'ref' field")
	}
	if payload["pusher"] == nil {
		t.Error("PushPayload must include 'pusher' field")
	}
	if payload["repository"] == nil {
		t.Error("PushPayload must include 'repository' field")
	}
	if payload["after"] == nil {
		t.Error("PushPayload must include 'after' field (head commit SHA)")
	}
}

// TestPushPayload_RefContainsBranch verifies that the "ref" field encodes the branch
// name in the refs/heads/ format expected by CI systems and webhook consumers.
func TestPushPayload_RefContainsBranch(t *testing.T) {
	svc := &WebhookService{}
	repo := model.Repository{Name: "testrepo", OwnerName: "bob"}
	payload := svc.PushPayload(repo, "bob", "feature/my-branch", "deadbeef")

	ref, ok := payload["ref"].(string)
	if !ok {
		t.Fatalf("'ref' must be a string, got %T", payload["ref"])
	}
	if !strings.Contains(ref, "feature/my-branch") {
		t.Errorf("'ref' must contain branch name, got %q", ref)
	}
}
