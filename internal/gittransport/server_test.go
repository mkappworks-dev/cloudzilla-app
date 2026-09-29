package gittransport_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	gogit "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/format/packfile"
	"github.com/go-git/go-git/v5/plumbing/protocol/packp"
	"github.com/go-git/go-git/v5/plumbing/protocol/packp/capability"
	"github.com/go-git/go-git/v5/plumbing/transport"
	"github.com/go-git/go-git/v5/storage/memory"

	"github.com/mkappworks-dev/cloudzilla-app/internal/gitref"
	"github.com/mkappworks-dev/cloudzilla-app/internal/gittransport"
	"github.com/mkappworks-dev/cloudzilla-app/internal/testutil"
)

var (
	mainRef  = plumbing.NewBranchReferenceName("main")
	topicRef = plumbing.NewBranchReferenceName("topic")
)

// pushRepo is a bare repo with main at base. web is a commit on base that no
// ref points at: what a merge or web edit writes after a client read main.
// pack carries pushed, the client's commit on base, which the repo lacks.
type pushRepo struct {
	dir               string
	repo              *gogit.Repository
	base, web, pushed plumbing.Hash
	pack              []byte
}

func newPushRepo(t *testing.T) *pushRepo {
	t.Helper()
	r := &pushRepo{dir: t.TempDir()}
	repo, err := gogit.PlainInit(r.dir, true)
	if err != nil {
		t.Fatalf("init bare repo: %v", err)
	}
	r.repo = repo
	r.base = testutil.WriteCommit(t, repo.Storer, "base")
	r.web = testutil.WriteCommit(t, repo.Storer, "web", r.base)
	r.set(t, mainRef, r.base)

	client := memory.NewStorage()
	r.pushed = testutil.WriteCommit(t, client, "pushed", r.base)
	var pack bytes.Buffer
	if _, err := packfile.NewEncoder(&pack, client, false).Encode([]plumbing.Hash{r.pushed}, 0); err != nil {
		t.Fatalf("encode pack: %v", err)
	}
	r.pack = pack.Bytes()
	return r
}

func (r *pushRepo) set(t *testing.T, name plumbing.ReferenceName, h plumbing.Hash) {
	t.Helper()
	if err := r.repo.Storer.SetReference(plumbing.NewHashReference(name, h)); err != nil {
		t.Fatalf("set %s: %v", name, err)
	}
}

func (r *pushRepo) get(t *testing.T, name plumbing.ReferenceName) plumbing.Hash {
	t.Helper()
	ref, err := r.repo.Storer.Reference(name)
	if err != nil {
		t.Fatalf("read %s: %v", name, err)
	}
	return ref.Hash()
}

// advertise opens a receive-pack session and sends the ref advertisement.
func (r *pushRepo) advertise(t *testing.T) transport.ReceivePackSession {
	t.Helper()
	ep, err := transport.NewEndpoint("/")
	if err != nil {
		t.Fatalf("endpoint: %v", err)
	}
	sess, err := gittransport.NewServer(r.repo.Storer).NewReceivePackSession(ep, nil)
	if err != nil {
		t.Fatalf("open session: %v", err)
	}
	if _, err := sess.AdvertisedReferences(); err != nil {
		t.Fatalf("advertise: %v", err)
	}
	return sess
}

// receive sends cmds and the pack; it may run off the test goroutine.
func (r *pushRepo) receive(sess transport.ReceivePackSession, cmds ...*packp.Command) (*packp.ReportStatus, error) {
	req := packp.NewReferenceUpdateRequest()
	_ = req.Capabilities.Set(capability.ReportStatus)
	req.Commands = cmds
	req.Packfile = io.NopCloser(bytes.NewReader(r.pack))
	return sess.ReceivePack(context.Background(), req)
}

func refStatus(t *testing.T, status *packp.ReportStatus, name plumbing.ReferenceName) string {
	t.Helper()
	if status == nil {
		t.Fatal("no report status")
	}
	for _, cs := range status.CommandStatuses {
		if cs.ReferenceName == name {
			return cs.Status
		}
	}
	t.Fatalf("no status for %s", name)
	return ""
}

func assertRefused(t *testing.T, status *packp.ReportStatus, err error, name plumbing.ReferenceName) {
	t.Helper()
	if err != nil {
		t.Errorf("ReceivePack: %v; a refused ref belongs in the report status only", err)
	}
	if got := refStatus(t, status, name); !strings.HasPrefix(got, gitref.ErrMoved.Error()) {
		t.Errorf("%s status = %q, want %q", name, got, gitref.ErrMoved.Error())
	}
}

func TestRevert_UndoesUpdateAndCreate(t *testing.T) {
	r := newPushRepo(t)
	r.set(t, mainRef, r.web)
	r.set(t, topicRef, r.web)

	if err := gittransport.Revert(r.repo.Storer, &packp.Command{Name: mainRef, Old: r.base, New: r.web}); err != nil {
		t.Fatalf("revert update: %v", err)
	}
	if got := r.get(t, mainRef); got != r.base {
		t.Errorf("main = %s, want %s", got, r.base)
	}

	if err := gittransport.Revert(r.repo.Storer, &packp.Command{Name: topicRef, New: r.web}); err != nil {
		t.Fatalf("revert create: %v", err)
	}
	if _, err := r.repo.Storer.Reference(topicRef); !errors.Is(err, plumbing.ErrReferenceNotFound) {
		t.Errorf("read reverted topic: err = %v, want ErrReferenceNotFound", err)
	}
}

func TestRevert_RefMovedSinceApplied_Kept(t *testing.T) {
	r := newPushRepo(t)
	later := testutil.WriteCommit(t, r.repo.Storer, "later", r.web)
	r.set(t, mainRef, later)

	err := gittransport.Revert(r.repo.Storer, &packp.Command{Name: mainRef, Old: r.base, New: r.web})
	if !errors.Is(err, gitref.ErrMoved) {
		t.Errorf("err = %v, want ErrMoved", err)
	}
	if got := r.get(t, mainRef); got != later {
		t.Errorf("main = %s, want %s", got, later)
	}
}

func TestNewServer_UpdateFromCurrentTip(t *testing.T) {
	r := newPushRepo(t)

	status, err := r.receive(r.advertise(t), &packp.Command{Name: mainRef, Old: r.base, New: r.pushed})
	if err != nil {
		t.Fatalf("ReceivePack: %v", err)
	}
	if got := refStatus(t, status, mainRef); got != "ok" {
		t.Errorf("main status = %q, want ok", got)
	}
	if got := r.get(t, mainRef); got != r.pushed {
		t.Errorf("main = %s, want pushed %s", got, r.pushed)
	}
}

func TestNewServer_RefMovedSinceAdvertisement_Refused(t *testing.T) {
	for _, tc := range []struct {
		name string
		to   func(*pushRepo) plumbing.Hash
	}{
		{"update", func(r *pushRepo) plumbing.Hash { return r.pushed }},
		{"delete", func(*pushRepo) plumbing.Hash { return plumbing.ZeroHash }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := newPushRepo(t)
			sess := r.advertise(t)
			r.set(t, mainRef, r.web)

			status, err := r.receive(sess, &packp.Command{Name: mainRef, Old: r.base, New: tc.to(r)})

			assertRefused(t, status, err, mainRef)
			if got := r.get(t, mainRef); got != r.web {
				t.Errorf("main = %s, want web commit %s", got, r.web)
			}
		})
	}
}

// The ref moves while the pack is being stored, so checking the commands
// before the pack is read is not enough.
func TestNewServer_RefMovedDuringReceive_Refused(t *testing.T) {
	r := newPushRepo(t)
	sess := r.advertise(t)

	var status *packp.ReportStatus
	var err error
	testutil.PushDuringCommit(t, r.dir, mainRef, r.base, r.web, func() {
		status, err = r.receive(sess, &packp.Command{Name: mainRef, Old: r.base, New: r.pushed})
	})

	assertRefused(t, status, err, mainRef)
	if got := r.get(t, mainRef); got != r.web {
		t.Errorf("main = %s, want web commit %s", got, r.web)
	}
}

func TestNewServer_RefusedRefDoesNotStopOthers(t *testing.T) {
	r := newPushRepo(t)
	sess := r.advertise(t)
	r.set(t, mainRef, r.web)

	status, err := r.receive(sess,
		&packp.Command{Name: mainRef, Old: r.base, New: r.pushed},
		&packp.Command{Name: topicRef, Old: plumbing.ZeroHash, New: r.pushed},
	)

	assertRefused(t, status, err, mainRef)
	if got := refStatus(t, status, topicRef); got != "ok" {
		t.Errorf("topic status = %q, want ok", got)
	}
	if got := r.get(t, topicRef); got != r.pushed {
		t.Errorf("topic = %s, want pushed %s", got, r.pushed)
	}
}
