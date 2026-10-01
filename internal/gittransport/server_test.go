package gittransport_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
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

// advertise opens a receive-pack session that vets refs with vet, which may be
// nil, and sends the ref advertisement.
func (r *pushRepo) advertise(t *testing.T, vet func(*packp.Command) error) transport.ReceivePackSession {
	t.Helper()
	ep, err := transport.NewEndpoint("/")
	if err != nil {
		t.Fatalf("endpoint: %v", err)
	}
	sess, err := gittransport.NewServer(r.repo.Storer, vet).NewReceivePackSession(ep, nil)
	if err != nil {
		t.Fatalf("open session: %v", err)
	}
	if _, err := sess.AdvertisedReferences(); err != nil {
		t.Fatalf("advertise: %v", err)
	}
	return sess
}

// request is a push of cmds and the pack, without report-status.
func (r *pushRepo) request(cmds ...*packp.Command) *packp.ReferenceUpdateRequest {
	req := packp.NewReferenceUpdateRequest()
	req.Commands = cmds
	req.Packfile = io.NopCloser(bytes.NewReader(r.pack))
	return req
}

// receive sends cmds and the pack; it may run off the test goroutine.
func (r *pushRepo) receive(sess transport.ReceivePackSession, cmds ...*packp.Command) (*packp.ReportStatus, error) {
	req := r.request(cmds...)
	_ = req.Capabilities.Set(capability.ReportStatus)
	return sess.ReceivePack(context.Background(), req)
}

func captureLogs(t *testing.T) *bytes.Buffer {
	t.Helper()
	var logs bytes.Buffer
	defaultLogger := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, nil)))
	t.Cleanup(func() { slog.SetDefault(defaultLogger) })
	return &logs
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

func TestNewServer_UpdateFromCurrentTip(t *testing.T) {
	r := newPushRepo(t)

	status, err := r.receive(r.advertise(t, nil), &packp.Command{Name: mainRef, Old: r.base, New: r.pushed})
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
			sess := r.advertise(t, nil)
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
	sess := r.advertise(t, nil)

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
	sess := r.advertise(t, nil)
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

func TestNewServer_VetRefusal_ReportedAndOtherRefsApply(t *testing.T) {
	r := newPushRepo(t)
	errProtected := errors.New("protected branch")
	vet := func(cmd *packp.Command) error {
		if cmd.Name == mainRef {
			return errProtected
		}
		return nil
	}

	status, err := r.receive(r.advertise(t, vet),
		&packp.Command{Name: mainRef, Old: r.base, New: r.pushed},
		&packp.Command{Name: topicRef, Old: plumbing.ZeroHash, New: r.pushed},
	)

	if err != nil {
		t.Errorf("ReceivePack: %v; a refused ref belongs in the report status only", err)
	}
	if got := refStatus(t, status, mainRef); got != errProtected.Error() {
		t.Errorf("main status = %q, want %q", got, errProtected.Error())
	}
	if got := r.get(t, mainRef); got != r.base {
		t.Errorf("main = %s, want %s", got, r.base)
	}
	if got := refStatus(t, status, topicRef); got != "ok" {
		t.Errorf("topic status = %q, want ok", got)
	}
	if got := r.get(t, topicRef); got != r.pushed {
		t.Errorf("topic = %s, want pushed %s", got, r.pushed)
	}
}

// go-git's receive-pack doesn't check connectivity: it would point a ref at an
// object that neither the pack nor the repo holds. vet reads the pushed
// objects, so it isn't asked about one that is missing.
func TestNewServer_RefToMissingObject_Refused(t *testing.T) {
	missing := plumbing.NewHash("1234567890123456789012345678901234567890")
	for _, tc := range []struct {
		name string
		ref  plumbing.ReferenceName
		old  func(*pushRepo) plumbing.Hash
	}{
		{"update", mainRef, func(r *pushRepo) plumbing.Hash { return r.base }},
		{"create", topicRef, func(*pushRepo) plumbing.Hash { return plumbing.ZeroHash }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := newPushRepo(t)
			old := tc.old(r)
			vetted := false
			vet := func(*packp.Command) error {
				vetted = true
				return nil
			}

			status, err := r.receive(r.advertise(t, vet), &packp.Command{Name: tc.ref, Old: old, New: missing})

			if err != nil {
				t.Errorf("ReceivePack: %v; a refused ref belongs in the report status only", err)
			}
			if got := refStatus(t, status, tc.ref); got != gittransport.ErrMissingObjects.Error() {
				t.Errorf("%s status = %q, want %q", tc.ref, got, gittransport.ErrMissingObjects.Error())
			}
			if vetted {
				t.Error("vet called for a missing object")
			}
			got := plumbing.ZeroHash
			if ref, err := r.repo.Storer.Reference(tc.ref); err == nil {
				got = ref.Hash()
			} else if err != plumbing.ErrReferenceNotFound {
				t.Fatalf("read %s: %v", tc.ref, err)
			}
			if got != old {
				t.Errorf("%s = %s, want %s", tc.ref, got, old)
			}
		})
	}
}

// go-git reports a ref's error text to the pusher, and a storer error names
// paths on the server.
func TestNewServer_RefWriteFails_StatusHidesStorerError(t *testing.T) {
	r := newPushRepo(t)
	if err := os.Chmod(filepath.Join(r.dir, mainRef.String()), 0o444); err != nil {
		t.Fatalf("make main read-only: %v", err)
	}
	logs := captureLogs(t)

	status, err := r.receive(r.advertise(t, nil), &packp.Command{Name: mainRef, Old: r.base, New: r.pushed})

	if err != nil {
		t.Errorf("ReceivePack: %v; a refused ref belongs in the report status only", err)
	}
	if got := refStatus(t, status, mainRef); got != gittransport.ErrRefUpdateFailed.Error() {
		t.Errorf("main status = %q, want %q", got, gittransport.ErrRefUpdateFailed.Error())
	}
	if got := r.get(t, mainRef); got != r.base {
		t.Errorf("main = %s, want %s", got, r.base)
	}
	if !strings.Contains(logs.String(), "permission denied") {
		t.Errorf("storer error not logged:\n%s", logs.String())
	}
}

// go-git reports the unpack error's text to the pusher, and an error storing
// the pushed objects names paths on the server.
func TestNewServer_ObjectWriteFails_UnpackStatusHidesStorerError(t *testing.T) {
	r := newPushRepo(t)
	// go-git stages every received object in objects/pack before moving it
	// into place.
	packDir := filepath.Join(r.dir, "objects", "pack")
	if err := os.Chmod(packDir, 0o555); err != nil {
		t.Fatalf("make objects/pack read-only: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(packDir, 0o755) })
	logs := captureLogs(t)

	status, err := r.receive(r.advertise(t, nil), &packp.Command{Name: mainRef, Old: r.base, New: r.pushed})

	if err == nil || err.Error() != gittransport.ErrUnpackFailed.Error() {
		t.Errorf("ReceivePack err = %v, want %q", err, gittransport.ErrUnpackFailed.Error())
	}
	if status == nil || status.UnpackStatus != gittransport.ErrUnpackFailed.Error() {
		t.Errorf("unpack status = %+v, want %q", status, gittransport.ErrUnpackFailed.Error())
	}
	if got := r.get(t, mainRef); got != r.base {
		t.Errorf("main = %s, want %s", got, r.base)
	}
	if !strings.Contains(logs.String(), "permission denied") {
		t.Errorf("storer error not logged:\n%s", logs.String())
	}
}

// A malformed pack is the pusher's own data, so they still get the reason.
func TestNewServer_MalformedPack_UnpackStatusKeepsReason(t *testing.T) {
	missingBase := plumbing.NewHash("1234567890123456789012345678901234567890")
	for _, tc := range []struct {
		name string
		pack func(*pushRepo) []byte
		want string
	}{
		{"bad signature", func(r *pushRepo) []byte { return append([]byte("XACK"), r.pack[4:]...) }, "malformed pack file signature"},
		{"truncated", func(r *pushRepo) []byte { return r.pack[:len(r.pack)/2] }, "malformed PACK file: unexpected EOF"},
		{"thin pack on a base the repo lacks", func(*pushRepo) []byte {
			return buildThinRefDeltaPack(t, missingBase, 4, []byte("target\n"))
		}, "object not found"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := newPushRepo(t)
			r.pack = tc.pack(r)

			status, err := r.receive(r.advertise(t, nil), &packp.Command{Name: mainRef, Old: r.base, New: r.pushed})

			if err == nil || err.Error() != tc.want {
				t.Errorf("ReceivePack err = %v, want %q", err, tc.want)
			}
			if status == nil || status.UnpackStatus != tc.want {
				t.Errorf("unpack status = %+v, want %q", status, tc.want)
			}
		})
	}
}

func TestNewServer_DeleteRef_Applies(t *testing.T) {
	r := newPushRepo(t)
	r.set(t, topicRef, r.base)

	status, err := r.receive(r.advertise(t, nil), &packp.Command{Name: topicRef, Old: r.base, New: plumbing.ZeroHash})
	if err != nil {
		t.Fatalf("ReceivePack: %v", err)
	}
	if got := refStatus(t, status, topicRef); got != "ok" {
		t.Errorf("topic status = %q, want ok", got)
	}
	if _, err := r.repo.Storer.Reference(topicRef); err != plumbing.ErrReferenceNotFound {
		t.Errorf("topic lookup err = %v, want %v", err, plumbing.ErrReferenceNotFound)
	}
}

// idleStream is an SSH client's stdin in a delete-only push: git holds it open
// until it reads the status, so a read would block.
type idleStream struct{ t *testing.T }

func (s idleStream) Read([]byte) (int, error) {
	s.t.Error("read a delete-only push's pack stream, where git sends no pack")
	return 0, io.ErrNoProgress
}

func (idleStream) Close() error { return nil }

// git sends no pack when every command is a delete.
func TestNewServer_DeleteOnly_ReadsNoPack(t *testing.T) {
	for _, tc := range []struct {
		name string
		pack func(*testing.T) io.ReadCloser
	}{
		{"http", func(*testing.T) io.ReadCloser { return io.NopCloser(bytes.NewReader(nil)) }},
		{"ssh", func(t *testing.T) io.ReadCloser { return idleStream{t} }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := newPushRepo(t)
			r.set(t, topicRef, r.base)
			req := r.request(&packp.Command{Name: topicRef, Old: r.base, New: plumbing.ZeroHash})
			req.Packfile = tc.pack(t)
			_ = req.Capabilities.Set(capability.ReportStatus)

			status, err := r.advertise(t, nil).ReceivePack(context.Background(), req)

			if err != nil {
				t.Fatalf("ReceivePack: %v", err)
			}
			if got := refStatus(t, status, topicRef); got != "ok" {
				t.Errorf("topic status = %q, want ok", got)
			}
			if _, err := r.repo.Storer.Reference(topicRef); err != plumbing.ErrReferenceNotFound {
				t.Errorf("topic lookup err = %v, want %v", err, plumbing.ErrReferenceNotFound)
			}
		})
	}
}

// vet judges a push by its commits, so it must run once the pack is stored and
// before the ref moves.
func TestNewServer_VetSeesPushedCommitAndOldRef(t *testing.T) {
	r := newPushRepo(t)
	var vetted *packp.Command
	var commitErr error
	var refAtVet plumbing.Hash
	vet := func(cmd *packp.Command) error {
		vetted = cmd
		_, commitErr = r.repo.CommitObject(cmd.New)
		refAtVet = r.get(t, cmd.Name)
		return nil
	}

	if _, err := r.receive(r.advertise(t, vet), &packp.Command{Name: mainRef, Old: r.base, New: r.pushed}); err != nil {
		t.Fatalf("ReceivePack: %v", err)
	}

	if vetted == nil {
		t.Fatal("vet not called")
	}
	if vetted.Name != mainRef || vetted.Old != r.base || vetted.New != r.pushed {
		t.Errorf("vetted %s %s -> %s, want %s %s -> %s", vetted.Name, vetted.Old, vetted.New, mainRef, r.base, r.pushed)
	}
	if commitErr != nil {
		t.Errorf("read pushed commit in vet: %v", commitErr)
	}
	if refAtVet != r.base {
		t.Errorf("main = %s in vet, want %s", refAtVet, r.base)
	}
}

// Without report-status go-git reports nothing and turns a refused ref into an
// error for the whole push, though the other refs landed.
func TestNewServer_NoReportStatus_StillReportsEachRef(t *testing.T) {
	r := newPushRepo(t)
	sess := r.advertise(t, nil)
	r.set(t, mainRef, r.web)
	req := r.request(
		&packp.Command{Name: mainRef, Old: r.base, New: r.pushed},
		&packp.Command{Name: topicRef, Old: plumbing.ZeroHash, New: r.pushed},
	)

	status, err := sess.ReceivePack(context.Background(), req)

	assertRefused(t, status, err, mainRef)
	if got := refStatus(t, status, topicRef); got != "ok" {
		t.Errorf("topic status = %q, want ok", got)
	}
	if req.Capabilities.Supports(capability.ReportStatus) {
		t.Error("request asks for report-status after ReceivePack; the client didn't, so it must not be sent one")
	}
}
