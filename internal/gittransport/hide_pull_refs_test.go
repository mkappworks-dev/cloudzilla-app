package gittransport_test

import (
	"testing"

	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/protocol/packp"
	"github.com/go-git/go-git/v5/plumbing/storer"
	"github.com/go-git/go-git/v5/plumbing/transport"

	"github.com/mkappworks-dev/cloudzilla-app/internal/gittransport"
	"github.com/mkappworks-dev/cloudzilla-app/internal/testutil"
)

func TestAdvertisement_HidesPullRefs(t *testing.T) {
	repo := testutil.InitBareRepo(t, t.TempDir())
	c := testutil.WriteCommit(t, repo.Storer, "c")
	for _, name := range []string{"refs/heads/main", "refs/pull/1/head"} {
		if err := repo.Storer.SetReference(plumbing.NewHashReference(plumbing.ReferenceName(name), c)); err != nil {
			t.Fatal(err)
		}
	}
	srv := gittransport.NewServer(repo.Storer, func(*packp.Command) error { return nil })
	ep, err := transport.NewEndpoint("/")
	if err != nil {
		t.Fatal(err)
	}

	up, err := srv.NewUploadPackSession(ep, nil)
	if err != nil {
		t.Fatal(err)
	}
	rp, err := srv.NewReceivePackSession(ep, nil)
	if err != nil {
		t.Fatal(err)
	}
	upAdv, err := up.AdvertisedReferences()
	if err != nil {
		t.Fatal(err)
	}
	rpAdv, err := rp.AdvertisedReferences()
	if err != nil {
		t.Fatal(err)
	}

	for name, adv := range map[string]*packp.AdvRefs{"upload-pack": upAdv, "receive-pack": rpAdv} {
		if _, ok := adv.References["refs/heads/main"]; !ok {
			t.Errorf("%s: refs/heads/main must be advertised", name)
		}
		if _, ok := adv.References["refs/pull/1/head"]; ok {
			t.Errorf("%s: refs/pull/1/head must not be advertised", name)
		}
	}
}

func TestHidePullRefs_KeepsDeltaReuse(t *testing.T) {
	repo := testutil.InitBareRepo(t, t.TempDir())

	if _, ok := gittransport.HidePullRefs(repo.Storer).(storer.DeltaObjectStorer); !ok {
		t.Error("the wrapper dropped DeltaObjectStorer, so every fetch would recompute its deltas")
	}
}
