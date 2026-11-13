package service

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/mkappworks-dev/cloudzilla-app/internal/config"
	"github.com/mkappworks-dev/cloudzilla-app/internal/secretbox"
	"github.com/mkappworks-dev/cloudzilla-app/internal/store"
	"github.com/mkappworks-dev/cloudzilla-app/internal/testutil"
)

const testSecretKey = "0123456789abcdef0123456789abcdef"

func testBox(t *testing.T, key string) *secretbox.Box {
	t.Helper()
	box, err := secretbox.New([]byte(key))
	if err != nil {
		t.Fatal(err)
	}
	return box
}

type instanceStack struct {
	signer *InstanceSigner
	sigs   *SignatureService
	st     *store.Stores
	db     *sql.DB
}

func newInstanceStack(t *testing.T, box *secretbox.Box) instanceStack {
	t.Helper()
	db := testutil.OpenFreshTestDB(t)
	st := store.New(db)
	users := NewUserService(st.User, config.AuthConfig{})
	return instanceStack{
		signer: NewInstanceSigner(st.InstanceSigningKey, box),
		sigs:   NewSignatureService(st.GPGKey, st.SSHSigningKey, users).WithInstanceKeys(st.InstanceSigningKey),
		st:     st,
		db:     db,
	}
}

// signedCommit returns a commit signed through the signer, the way a server-made commit will be.
func signedCommit(t *testing.T, s *InstanceSigner, msg string) *object.Commit {
	t.Helper()
	sig := object.Signature{Name: "Cloudzilla", Email: "noreply@example.test", When: time.Unix(1_700_000_000, 0).UTC()}
	c := &object.Commit{Author: sig, Committer: sig, Message: msg, TreeHash: plumbing.NewHash("4b825dc642cb6eb9a060e54bf8d69288fbee4904")}
	payload, err := signedPayload(c)
	if err != nil {
		t.Fatal(err)
	}
	if c.PGPSignature, err = s.Sign(context.Background(), payload); err != nil {
		t.Fatal(err)
	}
	obj := &plumbing.MemoryObject{}
	if err := c.Encode(obj); err != nil {
		t.Fatal(err)
	}
	out := &object.Commit{}
	if err := out.Decode(obj); err != nil {
		t.Fatal(err)
	}
	return out
}

func TestInstanceSigner_FirstCallSealsTheKey(t *testing.T) {
	st := newInstanceStack(t, testBox(t, testSecretKey))
	ctx := context.Background()
	if _, err := st.signer.Sign(ctx, []byte("payload")); err != nil {
		t.Fatal(err)
	}
	key, err := st.st.InstanceSigningKey.Active(ctx)
	if err != nil || key == nil {
		t.Fatalf("active key: %v, %v", key, err)
	}
	if !strings.HasPrefix(key.PublicKey, "ssh-ed25519 ") || !strings.HasPrefix(key.Fingerprint, "SHA256:") {
		t.Fatalf("public key %q, fingerprint %q", key.PublicKey, key.Fingerprint)
	}
	plain, err := testBox(t, testSecretKey).Open(instanceSigningPurpose, key.SealedPrivate)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(key.SealedPrivate, plain) {
		t.Fatal("the stored private half contains the plaintext key")
	}
	if _, err := testBox(t, "another-secret-key-another-secret-key").Open(instanceSigningPurpose, key.SealedPrivate); !errors.Is(err, secretbox.ErrUndecryptable) {
		t.Fatalf("under another secret key: %v, want ErrUndecryptable", err)
	}
}

func TestInstanceSigner_ConcurrentFirstCallsMakeOneKey(t *testing.T) {
	st := newInstanceStack(t, testBox(t, testSecretKey))
	ctx := context.Background()
	var wg sync.WaitGroup
	errs := make(chan error, 12)
	for range 12 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := st.signer.Sign(ctx, []byte("payload"))
			errs <- err
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	var n int
	if err := st.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM instance_signing_keys`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("%d keys after concurrent first calls, want 1", n)
	}
}

func TestInstanceSigner_NoSecretKey(t *testing.T) {
	st := newInstanceStack(t, nil)
	if _, err := st.signer.Sign(context.Background(), []byte("payload")); !errors.Is(err, ErrNoSecretKey) {
		t.Fatalf("Sign without a secret key: %v, want ErrNoSecretKey", err)
	}
	if _, err := st.signer.Rotate(context.Background()); !errors.Is(err, ErrNoSecretKey) {
		t.Fatalf("Rotate without a secret key: %v, want ErrNoSecretKey", err)
	}
}

func TestInstanceSigner_RotateKeepsOldSignaturesVerified(t *testing.T) {
	st := newInstanceStack(t, testBox(t, testSecretKey))
	ctx := context.Background()
	before := signedCommit(t, st.signer, "before rotation")
	first, err := st.st.InstanceSigningKey.Active(ctx)
	if err != nil {
		t.Fatal(err)
	}
	rotated, err := st.signer.Rotate(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if rotated.Fingerprint == first.Fingerprint {
		t.Fatal("rotation kept the same key")
	}
	after := signedCommit(t, st.signer, "after rotation")

	got, err := st.sigs.Verify(ctx, []*object.Commit{before, after})
	if err != nil {
		t.Fatal(err)
	}
	for name, c := range map[string]*object.Commit{"retired key": before, "active key": after} {
		res := got[c.Hash]
		if res.Status != SignatureVerified || res.Kind != SignatureKindInstance || res.SignerUsername != "Cloudzilla" {
			t.Errorf("%s: %+v, want verified/instance/Cloudzilla", name, res)
		}
	}
	if got[before.Hash].Fingerprint != first.Fingerprint || got[after.Hash].Fingerprint != rotated.Fingerprint {
		t.Errorf("fingerprints %q, %q", got[before.Hash].Fingerprint, got[after.Hash].Fingerprint)
	}
	active, err := st.st.InstanceSigningKey.Active(ctx)
	if err != nil || active.Fingerprint != rotated.Fingerprint {
		t.Fatalf("active key after rotate: %+v, %v", active, err)
	}
}

func TestInstanceSigner_TamperedCommitIsNotVerified(t *testing.T) {
	st := newInstanceStack(t, testBox(t, testSecretKey))
	c := signedCommit(t, st.signer, "original")
	c.Message = "tampered"
	got, err := st.sigs.Verify(context.Background(), []*object.Commit{c})
	if err != nil {
		t.Fatal(err)
	}
	if res := got[c.Hash]; res.Status != SignatureUnverified || res.Reason != ReasonBadSignature {
		t.Fatalf("tampered: %s/%q, want unverified/bad_signature", res.Status, res.Reason)
	}
}

func TestInstanceSigner_UndecryptableAfterSecretKeyChange(t *testing.T) {
	st := newInstanceStack(t, testBox(t, testSecretKey))
	ctx := context.Background()
	if _, err := st.signer.Sign(ctx, []byte("payload")); err != nil {
		t.Fatal(err)
	}
	other := NewInstanceSigner(st.st.InstanceSigningKey, testBox(t, "another-secret-key-another-secret-key"))
	if _, err := other.Sign(ctx, []byte("payload")); !errors.Is(err, secretbox.ErrUndecryptable) {
		t.Fatalf("Sign under another secret key: %v, want ErrUndecryptable", err)
	}
	// Rotation is how an operator recovers: it needs no access to the lost private half.
	if _, err := other.Rotate(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := other.Sign(ctx, []byte("payload")); err != nil {
		t.Fatalf("Sign after rotating: %v", err)
	}
}

func TestInstanceSigner_SignatureVerifiesWithSSHKeygen(t *testing.T) {
	bin, err := exec.LookPath("ssh-keygen")
	if err != nil {
		t.Skip("ssh-keygen not installed")
	}
	st := newInstanceStack(t, testBox(t, testSecretKey))
	ctx := context.Background()
	payload := []byte("a payload\n")
	armored, err := st.signer.Sign(ctx, payload)
	if err != nil {
		t.Fatal(err)
	}
	key, err := st.st.InstanceSigningKey.Active(ctx)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	sigFile, signers := filepath.Join(dir, "sig"), filepath.Join(dir, "allowed_signers")
	if err := os.WriteFile(sigFile, []byte(armored+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(signers, []byte("noreply@example.test "+key.PublicKey+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	cmd := exec.Command(bin, "-Y", "verify", "-f", signers, "-I", "noreply@example.test", "-n", "git", "-s", sigFile)
	cmd.Stdin = bytes.NewReader(payload)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("ssh-keygen -Y verify: %v\n%s", err, out)
	}
}
