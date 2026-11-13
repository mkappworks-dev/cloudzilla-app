package service

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/testutil"
	"golang.org/x/crypto/ssh"
)

// The fixtures in testdata/signed are made by generate.sh with native git, gpg and ssh-keygen.
const (
	rheaUser = int64(7)
	samUser  = int64(8)
)

// fixtureTime is inside both GPG fixture keys' lifetimes (Sam's ends 2024-01-31).
var fixtureTime = time.Date(2024, 1, 10, 0, 0, 0, 0, time.UTC)

func fixtureBytes(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", "signed", name))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func fixtureCommit(t *testing.T, name string) *object.Commit {
	t.Helper()
	obj := &plumbing.MemoryObject{}
	obj.SetType(plumbing.CommitObject)
	if _, err := obj.Write(fixtureBytes(t, name+".commit")); err != nil {
		t.Fatal(err)
	}
	c := &object.Commit{}
	if err := c.Decode(obj); err != nil {
		t.Fatal(err)
	}
	return c
}

func fixtureGPGKey(t *testing.T, file string, userID int64) model.GPGKey {
	t.Helper()
	key, err := parseGPGKey(string(fixtureBytes(t, file)), fixtureTime)
	if err != nil {
		t.Fatal(err)
	}
	key.UserID = userID
	return *key
}

func fixtureSSHKey(t *testing.T, userID int64) model.SSHSigningKey {
	t.Helper()
	return sshKeyRow(t, strings.TrimSpace(string(fixtureBytes(t, "ed25519.pub"))), "laptop", userID)
}

func sshKeyRow(t *testing.T, authorized, title string, userID int64) model.SSHSigningKey {
	t.Helper()
	pub, _, _, _, err := ssh.ParseAuthorizedKey([]byte(authorized))
	if err != nil {
		t.Fatal(err)
	}
	return model.SSHSigningKey{UserID: userID, Title: title, PublicKey: authorized, Fingerprint: ssh.FingerprintSHA256(pub)}
}

// fakeKeyStore answers the three lookups from memory and counts them.
type fakeKeyStore struct {
	gpg   []model.GPGKey
	ssh   []model.SSHSigningKey
	users map[int64]SigningIdentity
	calls struct{ gpg, ssh, dir int }
}

func (f *fakeKeyStore) ListByKeyIDs(_ context.Context, ids []string) ([]model.GPGKey, error) {
	f.calls.gpg++
	var out []model.GPGKey
	for _, k := range f.gpg {
		hit := slices.Contains(ids, k.KeyID)
		for _, sk := range k.Subkeys {
			hit = hit || slices.Contains(ids, sk.KeyID)
		}
		if hit {
			out = append(out, k)
		}
	}
	return out, nil
}

func (f *fakeKeyStore) ListByFingerprints(_ context.Context, fps []string) ([]model.SSHSigningKey, error) {
	f.calls.ssh++
	var out []model.SSHSigningKey
	for _, k := range f.ssh {
		if slices.Contains(fps, k.Fingerprint) {
			out = append(out, k)
		}
	}
	return out, nil
}

func (f *fakeKeyStore) SigningIdentities(_ context.Context, ids []int64) (map[int64]SigningIdentity, error) {
	f.calls.dir++
	out := map[int64]SigningIdentity{}
	for _, id := range ids {
		if u, ok := f.users[id]; ok {
			out[id] = u
		}
	}
	return out, nil
}

func (f *fakeKeyStore) service() *SignatureService { return NewSignatureService(f, f, f) }

func verifyOne(t *testing.T, f *fakeKeyStore, c *object.Commit) SignatureResult {
	t.Helper()
	got, err := f.service().Verify(context.Background(), []*object.Commit{c})
	if err != nil {
		t.Fatal(err)
	}
	return got[c.Hash]
}

func rhea(emails ...string) SigningIdentity {
	return SigningIdentity{Username: "rhea", VerifiedEmails: emails}
}

func TestSignatureService_Verify(t *testing.T) {
	rheaGPG := fixtureGPGKey(t, "rhea.pub.asc", rheaUser)
	samGPG := fixtureGPGKey(t, "sam.pub.asc", samUser)
	rheaSSH := fixtureSSHKey(t, rheaUser)
	revoked := rheaGPG
	revoked.RevokedAt = &fixtureTime
	revokedSub := rheaGPG
	revokedSub.Subkeys = slices.Clone(rheaGPG.Subkeys)
	for i := range revokedSub.Subkeys {
		revokedSub.Subkeys[i].RevokedAt = &fixtureTime
	}
	noSigningSub := rheaGPG
	noSigningSub.Subkeys = slices.Clone(rheaGPG.Subkeys)
	for i := range noSigningSub.Subkeys {
		noSigningSub.Subkeys[i].CanSign = false
	}

	tamper := func(c *object.Commit) *object.Commit {
		c.Message += "tampered\n"
		return c
	}
	sigOf := func(name string) string { return fixtureCommit(t, name).PGPSignature }
	withSig := func(c *object.Commit, sig string) *object.Commit {
		c.PGPSignature = sig
		return c
	}

	tests := []struct {
		name       string
		commit     *object.Commit
		gpg        []model.GPGKey
		ssh        []model.SSHSigningKey
		users      map[int64]SigningIdentity
		wantStatus SignatureStatus
		wantReason SignatureReason
		wantUser   string
		wantKind   SignatureKind
	}{
		{name: "unsigned", commit: fixtureCommit(t, "unsigned"), wantStatus: SignatureUnsigned},
		{name: "unparseable", commit: withSig(fixtureCommit(t, "unsigned"), "not a signature"), wantStatus: SignatureUnverified, wantReason: ReasonUnsupported},
		{name: "unparseable gpg armor", commit: withSig(fixtureCommit(t, "unsigned"), "-----BEGIN PGP SIGNATURE-----\n\nAAAA\n-----END PGP SIGNATURE-----"), wantStatus: SignatureUnverified, wantReason: ReasonUnsupported},
		{name: "unparseable ssh armor", commit: withSig(fixtureCommit(t, "unsigned"), "-----BEGIN SSH SIGNATURE-----\nAAAA\n-----END SSH SIGNATURE-----"), wantStatus: SignatureUnverified, wantReason: ReasonUnsupported},
		{name: "two signature blocks", commit: withSig(fixtureCommit(t, "gpg-primary"), sigOf("gpg-primary")+"\n"+sigOf("ssh-ed25519")), gpg: []model.GPGKey{rheaGPG}, users: map[int64]SigningIdentity{rheaUser: rhea("rhea@example.com")}, wantStatus: SignatureUnverified, wantReason: ReasonMultipleSignatures},

		{name: "gpg unknown key", commit: fixtureCommit(t, "gpg-primary"), wantStatus: SignatureUnverified, wantReason: ReasonNoKey},
		{name: "gpg another key registered", commit: fixtureCommit(t, "gpg-primary"), gpg: []model.GPGKey{samGPG}, users: map[int64]SigningIdentity{samUser: rhea("rhea@example.com")}, wantStatus: SignatureUnverified, wantReason: ReasonNoKey},
		{name: "gpg tampered message", commit: tamper(fixtureCommit(t, "gpg-primary")), gpg: []model.GPGKey{rheaGPG}, users: map[int64]SigningIdentity{rheaUser: rhea("rhea@example.com")}, wantStatus: SignatureUnverified, wantReason: ReasonBadSignature},
		{name: "gpg primary key", commit: fixtureCommit(t, "gpg-primary"), gpg: []model.GPGKey{rheaGPG}, users: map[int64]SigningIdentity{rheaUser: rhea("rhea@example.com")}, wantStatus: SignatureVerified, wantUser: "rhea", wantKind: SignatureKindGPG},
		{name: "gpg signing subkey", commit: fixtureCommit(t, "gpg-subkey"), gpg: []model.GPGKey{rheaGPG}, users: map[int64]SigningIdentity{rheaUser: rhea("rhea@example.com")}, wantStatus: SignatureVerified, wantUser: "rhea", wantKind: SignatureKindGPG},
		{name: "gpg subkey without the signing flag", commit: fixtureCommit(t, "gpg-subkey"), gpg: []model.GPGKey{noSigningSub}, users: map[int64]SigningIdentity{rheaUser: rhea("rhea@example.com")}, wantStatus: SignatureUnverified, wantReason: ReasonNoKey},
		{name: "gpg email unverified and not on key", commit: fixtureCommit(t, "gpg-other-email"), gpg: []model.GPGKey{rheaGPG}, users: map[int64]SigningIdentity{rheaUser: rhea("rhea@example.com")}, wantStatus: SignaturePartiallyVerified, wantReason: ReasonEmailNotVerified, wantUser: "rhea"},
		{name: "gpg email verified but not on key", commit: fixtureCommit(t, "gpg-other-email"), gpg: []model.GPGKey{rheaGPG}, users: map[int64]SigningIdentity{rheaUser: rhea("rhea@example.com", "rhea.work@example.com")}, wantStatus: SignaturePartiallyVerified, wantReason: ReasonEmailNotOnKey, wantUser: "rhea"},
		{name: "gpg key expired before the commit date", commit: fixtureCommit(t, "gpg-expiring-dated-after"), gpg: []model.GPGKey{samGPG}, users: map[int64]SigningIdentity{samUser: {Username: "sam", VerifiedEmails: []string{"sam@example.com"}}}, wantStatus: SignatureUnverified, wantReason: ReasonKeyExpired, wantUser: "sam"},
		{name: "gpg key expired after the commit date", commit: fixtureCommit(t, "gpg-expiring-signed-inside"), gpg: []model.GPGKey{samGPG}, users: map[int64]SigningIdentity{samUser: {Username: "sam", VerifiedEmails: []string{"sam@example.com"}}}, wantStatus: SignatureVerified, wantUser: "sam", wantKind: SignatureKindGPG},
		{name: "gpg key revoked", commit: fixtureCommit(t, "gpg-primary"), gpg: []model.GPGKey{revoked}, users: map[int64]SigningIdentity{rheaUser: rhea("rhea@example.com")}, wantStatus: SignatureUnverified, wantReason: ReasonKeyRevoked, wantUser: "rhea"},
		{name: "gpg subkey revoked", commit: fixtureCommit(t, "gpg-subkey"), gpg: []model.GPGKey{revokedSub}, users: map[int64]SigningIdentity{rheaUser: rhea("rhea@example.com")}, wantStatus: SignatureUnverified, wantReason: ReasonKeyRevoked, wantUser: "rhea"},
		{name: "gpg owner gone", commit: fixtureCommit(t, "gpg-primary"), gpg: []model.GPGKey{rheaGPG}, wantStatus: SignatureUnverified, wantReason: ReasonNoKey},

		{name: "ssh unknown key", commit: fixtureCommit(t, "ssh-ed25519"), wantStatus: SignatureUnverified, wantReason: ReasonNoKey},
		{name: "ssh tampered message", commit: tamper(fixtureCommit(t, "ssh-ed25519")), ssh: []model.SSHSigningKey{rheaSSH}, users: map[int64]SigningIdentity{rheaUser: rhea("rhea@example.com")}, wantStatus: SignatureUnverified, wantReason: ReasonBadSignature},
		{name: "ssh email verified", commit: fixtureCommit(t, "ssh-ed25519"), ssh: []model.SSHSigningKey{rheaSSH}, users: map[int64]SigningIdentity{rheaUser: rhea("rhea@example.com")}, wantStatus: SignatureVerified, wantUser: "rhea", wantKind: SignatureKindSSH},
		{name: "ssh email unverified", commit: fixtureCommit(t, "ssh-other-email"), ssh: []model.SSHSigningKey{rheaSSH}, users: map[int64]SigningIdentity{rheaUser: rhea("rhea@example.com")}, wantStatus: SignaturePartiallyVerified, wantReason: ReasonEmailNotVerified, wantUser: "rhea", wantKind: SignatureKindSSH},
		{name: "ssh noreply email", commit: fixtureCommit(t, "ssh-noreply"), ssh: []model.SSHSigningKey{rheaSSH}, users: map[int64]SigningIdentity{rheaUser: rhea("7+rhea@users.noreply.example.org")}, wantStatus: SignatureVerified, wantUser: "rhea", wantKind: SignatureKindSSH},
		{name: "ssh noreply email of another account", commit: fixtureCommit(t, "ssh-noreply"), ssh: []model.SSHSigningKey{rheaSSH}, users: map[int64]SigningIdentity{rheaUser: {Username: "rhea2"}}, wantStatus: SignaturePartiallyVerified, wantReason: ReasonEmailNotVerified, wantUser: "rhea2", wantKind: SignatureKindSSH},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := &fakeKeyStore{gpg: tc.gpg, ssh: tc.ssh, users: tc.users}
			got := verifyOne(t, f, tc.commit)
			if got.Status != tc.wantStatus || got.Reason != tc.wantReason {
				t.Fatalf("got %s/%q, want %s/%q", got.Status, got.Reason, tc.wantStatus, tc.wantReason)
			}
			if got.SignerUsername != tc.wantUser {
				t.Errorf("signer = %q, want %q", got.SignerUsername, tc.wantUser)
			}
			if tc.wantKind != "" && got.Kind != tc.wantKind {
				t.Errorf("kind = %q, want %q", got.Kind, tc.wantKind)
			}
			if got.CommitterEmail != tc.commit.Committer.Email {
				t.Errorf("committer email = %q, want %q", got.CommitterEmail, tc.commit.Committer.Email)
			}
		})
	}
}

func TestSignatureService_VerifyResultDetails(t *testing.T) {
	rheaGPG := fixtureGPGKey(t, "rhea.pub.asc", rheaUser)
	f := &fakeKeyStore{
		gpg:   []model.GPGKey{rheaGPG},
		ssh:   []model.SSHSigningKey{fixtureSSHKey(t, rheaUser)},
		users: map[int64]SigningIdentity{rheaUser: rhea("rhea@example.com")},
	}
	sub := verifyOne(t, f, fixtureCommit(t, "gpg-subkey"))
	var wantSub string
	for _, s := range rheaGPG.Subkeys {
		if s.CanSign {
			wantSub = s.Fingerprint
		}
	}
	if sub.Fingerprint != wantSub || sub.KeyID == rheaGPG.KeyID {
		t.Errorf("subkey signature reports %s / %s, want the subkey %s", sub.Fingerprint, sub.KeyID, wantSub)
	}
	primary := verifyOne(t, f, fixtureCommit(t, "gpg-primary"))
	if primary.Fingerprint != rheaGPG.Fingerprint || primary.KeyID != rheaGPG.KeyID {
		t.Errorf("primary signature reports %s / %s, want %s / %s", primary.Fingerprint, primary.KeyID, rheaGPG.Fingerprint, rheaGPG.KeyID)
	}
	ed := verifyOne(t, f, fixtureCommit(t, "ssh-ed25519"))
	if ed.KeyLabel != "laptop" || !strings.HasPrefix(ed.Fingerprint, "SHA256:") {
		t.Errorf("ssh signature reports label %q fingerprint %q", ed.KeyLabel, ed.Fingerprint)
	}
}

func armorSSHSig(b64 string) string {
	var sb strings.Builder
	sb.WriteString("-----BEGIN SSH SIGNATURE-----\n")
	for len(b64) > 70 {
		sb.WriteString(b64[:70] + "\n")
		b64 = b64[70:]
	}
	sb.WriteString(b64 + "\n-----END SSH SIGNATURE-----")
	return sb.String()
}

// signedInGo makes a commit signed by sign over its payload, for keys native tools cannot make here.
func signedInGo(t *testing.T, email, msg string, sign func(payload []byte) string) *object.Commit {
	t.Helper()
	who := object.Signature{Name: "Rhea", Email: email, When: time.Date(2024, 1, 15, 12, 0, 0, 0, time.UTC)}
	c := &object.Commit{Author: who, Committer: who, Message: msg, TreeHash: plumbing.NewHash("4b825dc642cb6eb9a060e54bf8d69288fbee4904")}
	payload, err := signedPayload(c)
	if err != nil {
		t.Fatal(err)
	}
	c.PGPSignature = armorSSHSig(sign(payload))
	full := &plumbing.MemoryObject{}
	if err := c.Encode(full); err != nil {
		t.Fatal(err)
	}
	c.Hash = full.Hash()
	return c
}

func TestSignatureService_HardwareKeyVerifies(t *testing.T) {
	for _, touched := range []bool{true, false} {
		t.Run(fmt.Sprintf("touched=%v", touched), func(t *testing.T) {
			hw, pub := testutil.NewHardwareKey(t)
			c := signedInGo(t, "rhea@example.com", "signed on a security key\n", func(p []byte) string { return hw.SignSSHSigIn("git", p, touched) })
			f := &fakeKeyStore{
				ssh:   []model.SSHSigningKey{sshKeyRow(t, pub, "yubikey", rheaUser)},
				users: map[int64]SigningIdentity{rheaUser: rhea("rhea@example.com")},
			}
			got := verifyOne(t, f, c)
			if got.Status != SignatureVerified || got.KeyLabel != "yubikey" {
				t.Errorf("got %s/%q label %q, want verified by yubikey", got.Status, got.Reason, got.KeyLabel)
			}
		})
	}
}

func TestSignatureService_WrongNamespaceIsBadSignature(t *testing.T) {
	signer, pub := testutil.NewSigningKey(t)
	c := signedInGo(t, "rhea@example.com", "signed for something else\n", func(p []byte) string { return testutil.SignSSHSig(t, signer, "file", p) })
	f := &fakeKeyStore{
		ssh:   []model.SSHSigningKey{sshKeyRow(t, pub, "k", rheaUser)},
		users: map[int64]SigningIdentity{rheaUser: rhea("rhea@example.com")},
	}
	if got := verifyOne(t, f, c); got.Status != SignatureUnverified || got.Reason != ReasonBadSignature {
		t.Errorf("got %s/%q, want unverified/bad_signature", got.Status, got.Reason)
	}
}

func TestSignatureService_KeyChangesApplyOnTheNextRead(t *testing.T) {
	c := fixtureCommit(t, "ssh-ed25519")
	f := &fakeKeyStore{users: map[int64]SigningIdentity{rheaUser: rhea("rhea@example.com")}}
	if got := verifyOne(t, f, c); got.Status != SignatureUnverified || got.Reason != ReasonNoKey {
		t.Fatalf("before adding the key: %s/%q", got.Status, got.Reason)
	}
	f.ssh = []model.SSHSigningKey{fixtureSSHKey(t, rheaUser)}
	if got := verifyOne(t, f, c); got.Status != SignatureVerified {
		t.Fatalf("after adding the key: %s/%q", got.Status, got.Reason)
	}
	f.ssh = nil
	if got := verifyOne(t, f, c); got.Status != SignatureUnverified {
		t.Fatalf("after deleting the key: %s/%q", got.Status, got.Reason)
	}
}

func TestSignatureService_PageOfCommitsDoesConstantQueries(t *testing.T) {
	signer, pub := testutil.NewSigningKey(t)
	rheaGPG := fixtureGPGKey(t, "rhea.pub.asc", rheaUser)
	f := &fakeKeyStore{
		gpg:   []model.GPGKey{rheaGPG},
		ssh:   []model.SSHSigningKey{sshKeyRow(t, pub, "k", samUser), fixtureSSHKey(t, rheaUser)},
		users: map[int64]SigningIdentity{rheaUser: rhea("rhea@example.com"), samUser: {Username: "sam", VerifiedEmails: []string{"sam@example.com"}}},
	}
	var page []*object.Commit
	for i := range 26 {
		page = append(page, signedInGo(t, "sam@example.com", fmt.Sprintf("commit %d\n", i), func(p []byte) string { return testutil.SignSSHSig(t, signer, "git", p) }))
	}
	page = append(page, fixtureCommit(t, "unsigned"), fixtureCommit(t, "gpg-primary"), fixtureCommit(t, "gpg-subkey"), fixtureCommit(t, "ssh-ed25519"))

	got, err := f.service().Verify(context.Background(), page)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 30 {
		t.Fatalf("results = %d, want 30", len(got))
	}
	for _, c := range page {
		want := SignatureVerified
		if c.PGPSignature == "" {
			want = SignatureUnsigned
		}
		if got[c.Hash].Status != want {
			t.Errorf("%s: status %s/%q, want %s", c.Hash, got[c.Hash].Status, got[c.Hash].Reason, want)
		}
	}
	if f.calls.gpg != 1 || f.calls.ssh != 1 || f.calls.dir != 1 {
		t.Errorf("lookups gpg=%d ssh=%d owners=%d, want one each for 30 commits", f.calls.gpg, f.calls.ssh, f.calls.dir)
	}
}

func TestSignatureService_UnsignedPageDoesNoQueries(t *testing.T) {
	f := &fakeKeyStore{}
	if _, err := f.service().Verify(context.Background(), []*object.Commit{fixtureCommit(t, "unsigned")}); err != nil {
		t.Fatal(err)
	}
	if f.calls != (struct{ gpg, ssh, dir int }{}) {
		t.Errorf("lookups = %+v, want none", f.calls)
	}
}

func TestGPGRings_ParseEachStoredKeyOnce(t *testing.T) {
	key := fixtureGPGKey(t, "rhea.pub.asc", rheaUser)
	rings := &gpgRings{}
	for range 5 {
		if _, ok := rings.publicKey(&key, key.KeyID); !ok {
			t.Fatal("primary key not found")
		}
	}
	if rings.parses != 1 {
		t.Errorf("parses = %d, want 1", rings.parses)
	}
}
