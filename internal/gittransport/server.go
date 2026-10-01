package gittransport

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/format/packfile"
	"github.com/go-git/go-git/v5/plumbing/protocol/packp"
	"github.com/go-git/go-git/v5/plumbing/protocol/packp/capability"
	"github.com/go-git/go-git/v5/plumbing/storer"
	"github.com/go-git/go-git/v5/plumbing/transport"
	"github.com/go-git/go-git/v5/plumbing/transport/server"

	"github.com/mkappworks-dev/cloudzilla-app/internal/gitref"
)

var (
	ErrMissingObjects  = errors.New("missing necessary objects")
	ErrNonCommitBranch = errors.New("trying to write non-commit object to branch")
	ErrRefUpdateFailed = errors.New("failed to update ref")
	ErrUnpackFailed    = errors.New("failed to store pushed objects")
)

// NewServer is go-git's git server over s, serving every endpoint. Its
// receive-pack stores packs through WrapForReceive, then applies a ref update
// only if s holds everything its new value reaches (a commit, for a branch),
// vet (when non-nil) accepts it, and the ref still holds the old value the
// client pushed from. A refused ref is reported in the status with its reason,
// and the rest of the push still applies.
func NewServer(s storer.Storer, vet func(*packp.Command) error) transport.Transport {
	return casServer{Transport: server.NewServer(loader{s}), s: s, vet: vet}
}

type loader struct{ s storer.Storer }

func (l loader) Load(*transport.Endpoint) (storer.Storer, error) { return l.s, nil }

type casServer struct {
	transport.Transport
	s   storer.Storer
	vet func(*packp.Command) error
}

func (c casServer) NewReceivePackSession(ep *transport.Endpoint, auth transport.AuthMethod) (transport.ReceivePackSession, error) {
	s := WrapForReceive(c.s)
	st := &casStorer{Storer: s, vet: c.vet, conn: newConnectivity(s)}
	sess, err := server.NewServer(loader{st}).NewReceivePackSession(ep, auth)
	if err != nil {
		return nil, err
	}
	return casSession{ReceivePackSession: sess, st: st}, nil
}

type casSession struct {
	transport.ReceivePackSession
	st *casStorer
}

// ReceivePack always returns the report status, so callers can tell which refs
// applied; they send it only if req asks for report-status.
func (c casSession) ReceivePack(ctx context.Context, req *packp.ReferenceUpdateRequest) (*packp.ReportStatus, error) {
	c.st.expect(req.Commands)
	// git sends no pack when every command is a delete, but Decode always sets
	// Packfile and go-git parses it: an HTTP body ends there ("empty packfile"),
	// and an SSH client keeps the stream open until it reads the status.
	if deleteOnly(req.Commands) && req.Packfile != nil {
		_ = req.Packfile.Close()
		req.Packfile = nil
	}
	if !req.Capabilities.Supports(capability.ReportStatus) {
		_ = req.Capabilities.Set(capability.ReportStatus)
		defer req.Capabilities.Delete(capability.ReportStatus)
	}
	status, err := c.ReceivePackSession.ReceivePack(ctx, req)
	if status == nil {
		return nil, err
	}
	switch {
	case status.UnpackStatus == "ok":
		// go-git also returns the first refused ref as an error, though the pack
		// and the other refs landed and status reports each ref.
		err = nil
	case !isPackError(err):
		// go-git reports the unpack error's text to the pusher, and an error
		// storing the pushed objects names paths on the server.
		slog.Error("gittransport: unpack failed", "error", err)
		err = ErrUnpackFailed
		status.UnpackStatus = err.Error()
	}
	return status, err
}

// isPackError reports whether err faults the pushed pack itself, which the
// pusher sent and may be told about. go-git flattens a zlib error into text it
// also uses for reads of the repo's own packs, so a corrupt zlib stream isn't
// one.
func isPackError(err error) bool {
	var packErr *packfile.Error
	return errors.As(err, &packErr) ||
		errors.Is(err, packfile.ErrMalformedPackFile) ||
		errors.Is(err, packfile.ErrReferenceDeltaNotFound) ||
		errors.Is(err, packfile.ErrInvalidDelta) ||
		errors.Is(err, packfile.ErrDeltaCmd) ||
		errors.Is(err, plumbing.ErrObjectNotFound) ||
		errors.Is(err, ErrPackTooLarge)
}

func deleteOnly(cmds []*packp.Command) bool {
	for _, cmd := range cmds {
		if cmd.Action() != packp.Delete {
			return false
		}
	}
	return true
}

// casStorer vets receive-pack's ref writes and turns them into
// compare-and-swaps. go-git writes each pushed ref without comparing it to the
// command's old value, so a ref that moved after the client read it would be
// silently overwritten. go-git writes refs only after storing the pack, so vet
// can read the pushed objects.
type casStorer struct {
	storer.Storer
	vet  func(*packp.Command) error
	old  map[plumbing.ReferenceName]plumbing.Hash
	conn *connectivity
}

func (c *casStorer) expect(cmds []*packp.Command) {
	c.old = make(map[plumbing.ReferenceName]plumbing.Hash, len(cmds))
	for _, cmd := range cmds {
		c.old[cmd.Name] = cmd.Old
	}
}

// SetEncodedObject notes the commits the pack stores: go-git's pack parser
// stores each object through it.
func (c *casStorer) SetEncodedObject(obj plumbing.EncodedObject) (plumbing.Hash, error) {
	h, err := c.Storer.SetEncodedObject(obj)
	if err == nil && obj.Type() == plumbing.CommitObject {
		c.conn.pushed[h] = struct{}{}
	}
	return h, err
}

func (c *casStorer) SetReference(ref *plumbing.Reference) error {
	return c.move(ref.Name(), ref.Hash())
}

func (c *casStorer) RemoveReference(name plumbing.ReferenceName) error {
	return c.move(name, plumbing.ZeroHash)
}

func (c *casStorer) move(name plumbing.ReferenceName, to plumbing.Hash) error {
	if !to.IsZero() {
		switch err := c.accept(name, to); {
		case errors.Is(err, plumbing.ErrObjectNotFound):
			return ErrMissingObjects
		case errors.Is(err, ErrNonCommitBranch):
			return err
		case err != nil:
			return refUpdateFailed(name, err)
		}
	}
	cmd := &packp.Command{Name: name, Old: c.old[name], New: to}
	if c.vet != nil {
		if err := c.vet(cmd); err != nil {
			return err
		}
	}
	err := gitref.Move(c.Storer, name, cmd.Old, cmd.New)
	switch {
	case err == nil:
		return nil
	case errors.Is(err, gitref.ErrMoved):
		return fmt.Errorf("%w; fetch and push again", err)
	default:
		return refUpdateFailed(name, err)
	}
}

// accept refuses what git's receive-pack would: a branch at anything but a
// commit, or a value whose history the repo doesn't wholly hold.
func (c *casStorer) accept(name plumbing.ReferenceName, to plumbing.Hash) error {
	obj, err := c.EncodedObject(plumbing.AnyObject, to)
	if err != nil {
		return err
	}
	if name.IsBranch() && obj.Type() != plumbing.CommitObject {
		return ErrNonCommitBranch
	}
	// A push names any old value it likes; only one the ref holds vouches for
	// the history below it.
	from := c.old[name]
	if ref, err := c.Reference(name); err != nil || ref.Hash() != from {
		from = plumbing.ZeroHash
	}
	return c.conn.check(obj, from)
}

// refUpdateFailed logs err and hides it: go-git reports the error's text to
// the pusher, and a storer error names paths on the server.
func refUpdateFailed(name plumbing.ReferenceName, err error) error {
	slog.Error("gittransport: ref update failed", "ref", name.String(), "error", err)
	return ErrRefUpdateFailed
}
