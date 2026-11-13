package components

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/mkappworks-dev/cloudzilla-app/internal/service"
)

func renderBadge(t *testing.T, r service.SignatureResult) string {
	t.Helper()
	var buf bytes.Buffer
	if err := SignatureBadge(r).Render(context.Background(), &buf); err != nil {
		t.Fatalf("render: %v", err)
	}
	return buf.String()
}

func TestSignatureBadge(t *testing.T) {
	cases := []struct {
		name string
		res  service.SignatureResult
		want []string
	}{
		{"verified ssh", service.SignatureResult{Status: service.SignatureVerified, Kind: service.SignatureKindSSH, SignerUsername: "malith", KeyLabel: "MacBook Pro", Fingerprint: "SHA256:abc", CommitterEmail: "m@example.com"},
			[]string{">Verified<", "Signed by the committer with a key on their account", `href="/malith"`, "MacBook Pro", "SHA256:abc", `aria-expanded="false"`, "/settings#signing-keys"}},
		{"verified gpg", service.SignatureResult{Status: service.SignatureVerified, Kind: service.SignatureKindGPG, SignerUsername: "daisy", KeyID: "3AA5C34371567BD2", Fingerprint: "4AEE18F8"},
			[]string{">Verified<", "GPG", "3AA5C34371567BD2", "4AEE18F8"}},
		{"partial", service.SignatureResult{Status: service.SignaturePartiallyVerified, Reason: service.ReasonEmailNotVerified, Kind: service.SignatureKindSSH, SignerUsername: "rhea", CommitterEmail: "rhea@contractor.dev"},
			[]string{">Partially verified<", "isn&#39;t one of rhea&#39;s verified emails", "Verifying rhea@contractor.dev"}},
		{"unverified no key", service.SignatureResult{Status: service.SignatureUnverified, Reason: service.ReasonNoKey, Kind: service.SignatureKindGPG, KeyID: "B5690EEEBB952194", CommitterEmail: "kiyo@example.com"},
			[]string{">Unverified<", "no account on this server has the key", "B5690EEEBB952194", "Settings"}},
		{"instance", service.SignatureResult{Status: service.SignatureVerified, Kind: "instance", SignerUsername: "Cloudzilla", Fingerprint: "SHA256:inst"},
			[]string{">Verified<", "made on the web and signed by this server", "Cloudzilla", "SHA256:inst"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out := renderBadge(t, tc.res)
			for _, w := range tc.want {
				if !strings.Contains(out, w) {
					t.Errorf("missing %q in %s", w, out)
				}
			}
		})
	}
}

func TestSignatureBadge_InstanceHasNoAccountLink(t *testing.T) {
	out := renderBadge(t, service.SignatureResult{Status: service.SignatureVerified, Kind: "instance", SignerUsername: "Cloudzilla"})
	if strings.Contains(out, `href="/Cloudzilla"`) {
		t.Errorf("instance signer must not link to an account: %s", out)
	}
}

func TestSignatureBadge_UnsignedRendersNothing(t *testing.T) {
	for _, r := range []service.SignatureResult{{}, {Status: service.SignatureUnsigned}} {
		if out := renderBadge(t, r); out != "" {
			t.Errorf("status %q rendered %q, want nothing", r.Status, out)
		}
	}
}
