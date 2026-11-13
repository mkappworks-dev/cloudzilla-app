package service

import (
	"context"
	"errors"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/mkappworks-dev/cloudzilla-app/internal/config"
	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/store"
	"github.com/mkappworks-dev/cloudzilla-app/internal/testutil"
)

// signingStack wires the real stores to a user whose verified address is the fixtures' committer email.
func signingStack(t *testing.T) (*SigningKeyService, *SignatureService, *UserService, int64) {
	t.Helper()
	db := testutil.OpenTestDB(t)
	st := store.New(db)
	uid := testutil.SeedUser(t, db, "sig"+testutil.UniqueSuffix(t))
	testutil.Exec(t, db, `INSERT INTO user_emails (user_id, email, verified_at) VALUES ($1, 'rhea@example.com', NOW())`, uid)
	users := NewUserService(st.User, config.AuthConfig{})
	return NewSigningKeyService(st.GPGKey, st.SSHSigningKey), NewSignatureService(st.GPGKey, st.SSHSigningKey, users), users, uid
}

func TestSignatureService_KeyAddedAndDeletedFlipTheSameCommit(t *testing.T) {
	keys, sigs, _, uid := signingStack(t)
	ctx := context.Background()
	status := func(c *object.Commit) SignatureResult {
		t.Helper()
		got, err := sigs.Verify(ctx, []*object.Commit{c})
		if err != nil {
			t.Fatal(err)
		}
		return got[c.Hash]
	}

	gpgCommit, sshCommit := fixtureCommit(t, "gpg-subkey"), fixtureCommit(t, "ssh-ed25519")
	for _, c := range []*object.Commit{gpgCommit, sshCommit} {
		if got := status(c); got.Status != SignatureUnverified || got.Reason != ReasonNoKey {
			t.Fatalf("before the key: %s/%q, want unverified/no_key", got.Status, got.Reason)
		}
	}

	gpgKey, err := keys.AddGPGKey(ctx, uid, string(fixtureBytes(t, "rhea.pub.asc")))
	if err != nil {
		t.Fatal(err)
	}
	sshKey, err := keys.AddSSHKey(ctx, uid, "laptop", string(fixtureBytes(t, "ed25519.pub")))
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range []*object.Commit{gpgCommit, sshCommit} {
		if got := status(c); got.Status != SignatureVerified {
			t.Fatalf("with the key: %s/%q, want verified", got.Status, got.Reason)
		}
	}

	// The fixtures' committer dates are in the past, so a key removed now has been removed since they were made.
	if err := keys.RemoveGPGKey(ctx, gpgKey.ID, uid); err != nil {
		t.Fatal(err)
	}
	if err := keys.RemoveSSHKey(ctx, sshKey.ID, uid); err != nil {
		t.Fatal(err)
	}
	for _, c := range []*object.Commit{gpgCommit, sshCommit} {
		if got := status(c); got.Status != SignatureVerified {
			t.Fatalf("after removing the key: %s/%q, want the earlier commit still verified", got.Status, got.Reason)
		}
	}

	db := testutil.OpenTestDB(t)
	testutil.Exec(t, db, `DELETE FROM gpg_keys WHERE id = $1`, gpgKey.ID)
	testutil.Exec(t, db, `DELETE FROM ssh_signing_keys WHERE id = $1`, sshKey.ID)
	for _, c := range []*object.Commit{gpgCommit, sshCommit} {
		if got := status(c); got.Status != SignatureUnverified || got.Reason != ReasonNoKey {
			t.Fatalf("after deleting the key row: %s/%q, want unverified/no_key", got.Status, got.Reason)
		}
	}
}

func TestUserService_SigningIdentities(t *testing.T) {
	_, _, users, uid := signingStack(t)
	db := testutil.OpenTestDB(t)
	testutil.Exec(t, db, `INSERT INTO user_emails (user_id, email) VALUES ($1, 'pending@example.com')`, uid)
	other := testutil.SeedUser(t, db, "sig"+testutil.UniqueSuffix(t))
	ctx := context.Background()

	got, err := users.SigningIdentities(ctx, []int64{uid, other})
	if err != nil {
		t.Fatal(err)
	}
	id := got[uid]
	if !slices.Contains(id.VerifiedEmails, "rhea@example.com") {
		t.Errorf("verified emails %v lack the verified additional address", id.VerifiedEmails)
	}
	if slices.Contains(id.VerifiedEmails, "pending@example.com") {
		t.Errorf("verified emails %v include an unverified address", id.VerifiedEmails)
	}
	primary := id.Username + "@test.invalid"
	if slices.Contains(id.VerifiedEmails, primary) {
		t.Errorf("verified emails %v include the unverified primary address", id.VerifiedEmails)
	}
	if !slices.ContainsFunc(id.VerifiedEmails, func(e string) bool { _, _, ok := parseNoreplyEmail(e); return ok }) {
		t.Errorf("verified emails %v lack the noreply address", id.VerifiedEmails)
	}

	testutil.Exec(t, db, `UPDATE users SET email_verified_at = NOW() WHERE id = $1`, uid)
	got, err = users.SigningIdentities(ctx, []int64{uid})
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(got[uid].VerifiedEmails, primary) {
		t.Errorf("verified emails %v lack the verified primary address", got[uid].VerifiedEmails)
	}
	if empty, err := users.SigningIdentities(ctx, nil); err != nil || len(empty) != 0 {
		t.Errorf("no ids: %v, %v", empty, err)
	}
}

func TestSigningKeyLookups_ByKeyIDAndFingerprint(t *testing.T) {
	keys, _, _, uid := signingStack(t)
	db := testutil.OpenTestDB(t)
	st := store.New(db)
	ctx := context.Background()

	rhea, err := keys.AddGPGKey(ctx, uid, string(fixtureBytes(t, "rhea.pub.asc")))
	if err != nil {
		t.Fatal(err)
	}
	sshKey, err := keys.AddSSHKey(ctx, uid, "laptop", string(fixtureBytes(t, "ed25519.pub")))
	if err != nil {
		t.Fatal(err)
	}
	var subID string
	for _, s := range rhea.Subkeys {
		if s.CanSign {
			subID = s.KeyID
		}
	}

	got, err := st.GPGKey.ListByKeyIDs(ctx, []string{subID, "ffffffffffffffff"})
	if err != nil || len(got) != 1 {
		t.Fatalf("by subkey id: %v (%v), want the one key", got, err)
	}
	if got[0].Fingerprint != rhea.Fingerprint || len(got[0].Subkeys) != 1 || !slices.Equal(got[0].Emails, []string{"rhea@example.com"}) {
		t.Errorf("key = %+v, want its subkeys and UID emails loaded", got[0])
	}
	if got, err := st.GPGKey.ListByKeyIDs(ctx, []string{rhea.KeyID}); err != nil || len(got) != 1 {
		t.Errorf("by primary id: %v (%v)", got, err)
	}
	if got, err := st.GPGKey.ListByKeyIDs(ctx, []string{"ffffffffffffffff"}); err != nil || len(got) != 0 {
		t.Errorf("unknown id: %v (%v), want none", got, err)
	}

	bySSH, err := st.SSHSigningKey.ListByFingerprints(ctx, []string{sshKey.Fingerprint, "SHA256:nope"})
	if err != nil || len(bySSH) != 1 || bySSH[0].Title != "laptop" {
		t.Errorf("by fingerprint: %v (%v)", bySSH, err)
	}
}

// signedRepo is a repository whose main branch is a fixture-signed commit and whose base branch is an unrelated
// unsigned root, so a pull request from main to base lists the signed commit.
func signedRepo(t *testing.T, fixture string) (*CodeService, plumbing.Hash) {
	t.Helper()
	root := t.TempDir()
	dir := filepath.Join(root, "rhea", "signed.git")
	if err := os.MkdirAll(filepath.Dir(dir), 0o755); err != nil {
		t.Fatal(err)
	}
	repo := testutil.InitBareRepo(t, dir)
	base := testutil.WriteCommit(t, repo.Storer, "base")

	obj := repo.Storer.NewEncodedObject()
	obj.SetType(plumbing.CommitObject)
	w, err := obj.Writer()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write(fixtureBytes(t, fixture+".commit")); err != nil {
		t.Fatal(err)
	}
	_ = w.Close()
	head, err := repo.Storer.SetEncodedObject(obj)
	if err != nil {
		t.Fatal(err)
	}
	for name, h := range map[string]plumbing.Hash{"main": head, "base": base} {
		if err := repo.Storer.SetReference(plumbing.NewHashReference(plumbing.NewBranchReferenceName(name), h)); err != nil {
			t.Fatal(err)
		}
	}
	if err := repo.Storer.SetReference(plumbing.NewSymbolicReference(plumbing.HEAD, plumbing.NewBranchReferenceName("main"))); err != nil {
		t.Fatal(err)
	}
	return NewCodeService(config.GitConfig{ReposRoot: root}), head
}

func TestCodeService_CommitReadsCarryASignatureResult(t *testing.T) {
	svc, head := signedRepo(t, "ssh-ed25519")
	f := &fakeKeyStore{
		ssh:   []model.SSHSigningKey{fixtureSSHKey(t, rheaUser)},
		users: map[int64]SigningIdentity{rheaUser: rhea("rhea@example.com")},
	}
	svc.WithSignatures(f.service())

	log, err := svc.GetCommits("rhea", "signed", "main", 1, 30)
	if err != nil || len(log.Commits) != 1 {
		t.Fatalf("GetCommits: %v (%v)", log, err)
	}
	if got := log.Commits[0].Signature; got.Status != SignatureVerified || got.SignerUsername != "rhea" {
		t.Errorf("GetCommits signature = %+v, want verified by rhea", got)
	}
	detail, err := svc.GetVerifiedCommit("rhea", "signed", head.String())
	if err != nil {
		t.Fatal(err)
	}
	if detail.Signature.Status != SignatureVerified || detail.Signature.Kind != SignatureKindSSH {
		t.Errorf("GetCommit signature = %+v, want verified ssh", detail.Signature)
	}
	pr, err := svc.PullCommits("rhea", "signed", "base", "main")
	if err != nil || len(pr) != 1 {
		t.Fatalf("PullCommits: %v (%v)", pr, err)
	}
	if pr[0].Signature.Status != SignatureVerified {
		t.Errorf("PullCommits signature = %+v, want verified", pr[0].Signature)
	}
}

type failingKeys struct{ fakeKeyStore }

func (failingKeys) ListByFingerprints(context.Context, []string) ([]model.SSHSigningKey, error) {
	return nil, os.ErrClosed
}

func TestCodeService_FailedLookupLeavesTheCommitUnverifiedNotTheReadFailed(t *testing.T) {
	svc, head := signedRepo(t, "ssh-ed25519")
	f := &failingKeys{}
	svc.WithSignatures(NewSignatureService(f, f, f))

	log, err := svc.GetCommits("rhea", "signed", "main", 1, 30)
	if err != nil {
		t.Fatalf("GetCommits: %v", err)
	}
	if got := log.Commits[0].Signature; got.Status != SignatureUnverified || got.Reason != ReasonUnavailable {
		t.Errorf("signature = %+v, want unverified/unavailable", got)
	}
	detail, err := svc.GetVerifiedCommit("rhea", "signed", head.String())
	if err != nil || detail.Signature.Status != SignatureUnverified {
		t.Errorf("GetCommit: %+v (%v)", detail.Signature, err)
	}
}

func TestCodeService_WithoutSignaturesLeavesTheResultEmpty(t *testing.T) {
	svc, _ := signedRepo(t, "ssh-ed25519")
	log, err := svc.GetCommits("rhea", "signed", "main", 1, 30)
	if err != nil || log.Commits[0].Signature != (SignatureResult{}) {
		t.Errorf("signature = %+v (%v), want the zero value", log.Commits[0].Signature, err)
	}
}

func TestCodeService_PullCommitsUnverifiedSkipsKeyLookups(t *testing.T) {
	svc, _ := signedRepo(t, "ssh-ed25519")
	f := &fakeKeyStore{}
	svc.WithSignatures(f.service())

	commits, err := svc.PullCommitsUnverified("rhea", "signed", "base", "main")
	if err != nil || len(commits) != 1 {
		t.Fatalf("PullCommitsUnverified: %v (%v)", commits, err)
	}
	if f.calls.gpg+f.calls.ssh+f.calls.dir != 0 {
		t.Errorf("lookups = %+v, want none", f.calls)
	}
	if commits[0].Signature != (SignatureResult{}) {
		t.Errorf("signature = %+v, want the zero value", commits[0].Signature)
	}
	if _, err := svc.PullCommits("rhea", "signed", "base", "main"); err != nil || f.calls.ssh == 0 {
		t.Errorf("PullCommits did not verify: %+v (%v)", f.calls, err)
	}
}

func TestUserService_EmailOwners(t *testing.T) {
	_, _, users, uid := signingStack(t)
	db := testutil.OpenTestDB(t)
	testutil.Exec(t, db, `INSERT INTO user_emails (user_id, email) VALUES ($1, 'pending@example.com')`, uid)
	testutil.Exec(t, db, `UPDATE users SET email_verified_at = NOW() WHERE id = $1`, uid)
	ctx := context.Background()

	var username string
	if err := db.QueryRowContext(ctx, `SELECT username FROM users WHERE id = $1`, uid).Scan(&username); err != nil {
		t.Fatal(err)
	}
	noreply := strings.ToLower(noreplyEmail(users.noreplyHost, &model.User{ID: uid, Username: username}))
	wrongName := strings.Replace(noreply, "+"+username+"@", "+someone-else@", 1)

	got, err := users.EmailOwners(ctx, []string{"Rhea@Example.com", username + "@test.invalid", "pending@example.com", "nobody@example.com", noreply, wrongName})
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]int64{"rhea@example.com": uid, username + "@test.invalid": uid, noreply: uid}
	if !maps.Equal(got, want) {
		t.Errorf("owners = %v, want %v (unverified, unknown and wrong-username noreply addresses left out)", got, want)
	}
	if empty, err := users.EmailOwners(ctx, nil); err != nil || len(empty) != 0 {
		t.Errorf("no emails: %v, %v", empty, err)
	}
}

func TestSignatureService_SquattedKeyEndToEnd(t *testing.T) {
	keys, sigs, _, rheaID := signingStack(t)
	db := testutil.OpenTestDB(t)
	mallory := testutil.SeedUser(t, db, "mal"+testutil.UniqueSuffix(t))
	ctx := context.Background()

	if _, err := keys.AddSSHKey(ctx, mallory, "stolen", string(fixtureBytes(t, "ed25519.pub"))); err != nil {
		t.Fatal(err)
	}
	if _, err := keys.AddSSHKey(ctx, rheaID, "mine", string(fixtureBytes(t, "ed25519.pub"))); !errors.Is(err, ErrSigningKeyTaken) {
		t.Fatalf("victim adding her squatted key: got %v, want ErrSigningKeyTaken", err)
	}

	verify := func(name string) SignatureResult {
		t.Helper()
		c := fixtureCommit(t, name)
		got, err := sigs.Verify(ctx, []*object.Commit{c})
		if err != nil {
			t.Fatal(err)
		}
		return got[c.Hash]
	}
	if got := verify("ssh-ed25519"); got.Status != SignatureUnverified || got.Reason != ReasonOtherUser || got.SignerUsername != "" {
		t.Errorf("rhea@example.com is verified on rhea's account: got %s/%q signer %q, want unverified/other_user, unnamed", got.Status, got.Reason, got.SignerUsername)
	}
	if got := verify("ssh-other-email"); got.Status != SignaturePartiallyVerified || got.Reason != ReasonEmailNotVerified {
		t.Errorf("rhea@contractor.dev is verified nowhere: got %s/%q, want partially_verified/email_not_verified", got.Status, got.Reason)
	}
}
