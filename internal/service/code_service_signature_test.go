package service

import (
	"testing"

	"github.com/go-git/go-git/v5/plumbing/object"
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
