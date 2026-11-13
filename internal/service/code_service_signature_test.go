package service

import (
	"fmt"
	"slices"
	"testing"

	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/mkappworks-dev/cloudzilla-app/internal/testutil"
)

func TestCodeService_ThirtyCommitsCostOneLookupPerKeyKind(t *testing.T) {
	gpg, ssh := fixtureCommit(t, "gpg-subkey"), fixtureCommit(t, "ssh-ed25519")
	var commits []*object.Commit
	for i := 0; i < 30; i++ {
		c := *gpg
		if i%2 == 1 {
			c = *ssh
		}
		c.Hash[0] = byte(i + 1)
		commits = append(commits, &c)
	}
	f := &fakeKeyStore{}
	svc := &CodeService{}
	svc.WithSignatures(f.service())

	got := svc.summarizeCommits(commits)
	if len(got) != 30 || got[0].Signature.Status == "" {
		t.Fatalf("summaries = %d, first signature %+v", len(got), got[0].Signature)
	}
	if f.calls.gpg != 1 || f.calls.ssh != 1 || f.calls.dir > 1 {
		t.Errorf("lookups = %+v, want one per key kind and at most one for owners", f.calls)
	}
}

func TestCodeService_GetCommitSkipsVerification(t *testing.T) {
	svc, head := signedRepo(t, "ssh-ed25519")
	f := &fakeKeyStore{}
	svc.WithSignatures(f.service())

	detail, err := svc.GetCommit("rhea", "signed", head.String())
	if err != nil {
		t.Fatal(err)
	}
	if detail.Signature != (SignatureResult{}) || f.calls.gpg+f.calls.ssh+f.calls.dir != 0 {
		t.Errorf("GetCommit verified: %+v, lookups %+v", detail.Signature, f.calls)
	}
}

func TestCodeService_BlameOfManyCommitsDoesOneLookupPerKeyKind(t *testing.T) {
	base := fixtureBytes(t, "ssh-ed25519.commit")
	repo := testutil.InitBareRepo(t, t.TempDir())
	var hashes []plumbing.Hash
	for i := 0; i < 40; i++ {
		obj := repo.Storer.NewEncodedObject()
		obj.SetType(plumbing.CommitObject)
		w, _ := obj.Writer()
		_, _ = w.Write(append(slices.Clone(base), fmt.Sprintf("change %d\n", i)...))
		_ = w.Close()
		h, err := repo.Storer.SetEncodedObject(obj)
		if err != nil {
			t.Fatal(err)
		}
		hashes = append(hashes, h)
	}

	// 50 runs of 20 lines over 40 commits, so the last 10 runs revisit commits already seen.
	lines := make([]BlameLine, 1000)
	for i := range lines {
		lines[i] = BlameLine{LineNum: i + 1, FullHash: hashes[(i/20)%40].String(), ShowMeta: i%20 == 0}
	}
	f := &fakeKeyStore{}
	svc := &CodeService{}
	svc.WithSignatures(f.service())

	svc.attachBlameSignatures(repo, lines)

	if f.calls.ssh != 1 || f.calls.gpg > 1 || f.calls.dir > 1 {
		t.Errorf("lookups = %+v, want one batch for 40 distinct commits", f.calls)
	}
	if lines[0].Signature.Status == "" || lines[1].Signature.Status != "" {
		t.Errorf("signature on hunk start %+v, on a continuation line %+v", lines[0].Signature, lines[1].Signature)
	}
}

func TestCodeService_SignatureForCommit(t *testing.T) {
	svc, head := signedRepo(t, "ssh-ed25519")
	f := &fakeKeyStore{}
	svc.WithSignatures(f.service())

	if got := svc.SignatureForCommit("rhea", "signed", head.String()); got.Status == "" {
		t.Errorf("signed commit has no verdict: %+v", got)
	}
	if got := svc.SignatureForCommit("rhea", "signed", "0000000000000000000000000000000000000000"); got != (SignatureResult{}) {
		t.Errorf("missing commit verdict = %+v", got)
	}
}
