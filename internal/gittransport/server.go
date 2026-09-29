package gittransport

import (
	"context"
	"errors"
	"fmt"

	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/protocol/packp"
	"github.com/go-git/go-git/v5/plumbing/protocol/packp/capability"
	"github.com/go-git/go-git/v5/plumbing/storer"
	"github.com/go-git/go-git/v5/plumbing/transport"
	"github.com/go-git/go-git/v5/plumbing/transport/server"

	"github.com/mkappworks-dev/cloudzilla-app/internal/gitref"
)

// NewServer is go-git's git server over s, serving every endpoint. Its
// receive-pack stores packs through WrapForReceive, then applies a ref update
// only if vet (when non-nil) accepts it and the ref still holds the old value
// the client pushed from. A refused ref is reported in the status with its
// reason, and the rest of the push still applies.
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
	st := &casStorer{Storer: WrapForReceive(c.s), vet: c.vet}
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
	if !req.Capabilities.Supports(capability.ReportStatus) {
		_ = req.Capabilities.Set(capability.ReportStatus)
		defer req.Capabilities.Delete(capability.ReportStatus)
	}
	status, err := c.ReceivePackSession.ReceivePack(ctx, req)
	// go-git also returns the first refused ref as an error, though the pack
	// and the other refs landed and status reports each ref.
	if status != nil && status.UnpackStatus == "ok" {
		err = nil
	}
	return status, err
}

// casStorer vets receive-pack's ref writes and turns them into
// compare-and-swaps. go-git writes each pushed ref without comparing it to the
// command's old value, so a ref that moved after the client read it would be
// silently overwritten. go-git writes refs only after storing the pack, so vet
// can read the pushed objects.
type casStorer struct {
	storer.Storer
	vet func(*packp.Command) error
	old map[plumbing.ReferenceName]plumbing.Hash
}

func (c *casStorer) expect(cmds []*packp.Command) {
	c.old = make(map[plumbing.ReferenceName]plumbing.Hash, len(cmds))
	for _, cmd := range cmds {
		c.old[cmd.Name] = cmd.Old
	}
}

func (c *casStorer) SetReference(ref *plumbing.Reference) error {
	return c.move(ref.Name(), ref.Hash())
}

func (c *casStorer) RemoveReference(name plumbing.ReferenceName) error {
	return c.move(name, plumbing.ZeroHash)
}

func (c *casStorer) move(name plumbing.ReferenceName, to plumbing.Hash) error {
	cmd := &packp.Command{Name: name, Old: c.old[name], New: to}
	if c.vet != nil {
		if err := c.vet(cmd); err != nil {
			return err
		}
	}
	err := gitref.Move(c.Storer, name, cmd.Old, cmd.New)
	if errors.Is(err, gitref.ErrMoved) {
		return fmt.Errorf("%w; fetch and push again", err)
	}
	return err
}
