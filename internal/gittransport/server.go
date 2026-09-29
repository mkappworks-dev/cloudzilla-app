package gittransport

import (
	"context"
	"errors"
	"fmt"

	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/protocol/packp"
	"github.com/go-git/go-git/v5/plumbing/storer"
	"github.com/go-git/go-git/v5/plumbing/transport"
	"github.com/go-git/go-git/v5/plumbing/transport/server"

	"github.com/mkappworks-dev/cloudzilla-app/internal/gitref"
)

// NewServer is go-git's git server over s, serving every endpoint. Its
// receive-pack stores packs through WrapForReceive and applies a ref update
// only if the ref still holds the old value the client pushed from.
func NewServer(s storer.Storer) transport.Transport {
	return casServer{Transport: server.NewServer(loader{s}), s: s}
}

// Revert undoes an applied receive-pack command, unless the ref has moved
// since: then it fails with gitref.ErrMoved and the ref keeps its new value.
func Revert(st storer.ReferenceStorer, cmd *packp.Command) error {
	return gitref.Move(st, cmd.Name, cmd.New, cmd.Old)
}

type loader struct{ s storer.Storer }

func (l loader) Load(*transport.Endpoint) (storer.Storer, error) { return l.s, nil }

type casServer struct {
	transport.Transport
	s storer.Storer
}

func (c casServer) NewReceivePackSession(ep *transport.Endpoint, auth transport.AuthMethod) (transport.ReceivePackSession, error) {
	st := &casStorer{Storer: WrapForReceive(c.s)}
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

func (c casSession) ReceivePack(ctx context.Context, req *packp.ReferenceUpdateRequest) (*packp.ReportStatus, error) {
	c.st.expect(req.Commands)
	status, err := c.ReceivePackSession.ReceivePack(ctx, req)
	// go-git also returns the first refused ref as an error, though the pack
	// and the other refs landed and status reports each ref.
	if status != nil && status.UnpackStatus == "ok" {
		err = nil
	}
	return status, err
}

// casStorer turns receive-pack's ref writes into compare-and-swaps. go-git
// writes each pushed ref without comparing it to the command's old value, so a
// ref that moved after the client read it would be silently overwritten.
type casStorer struct {
	storer.Storer
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
	err := gitref.Move(c.Storer, name, c.old[name], to)
	if errors.Is(err, gitref.ErrMoved) {
		return fmt.Errorf("%w; fetch and push again", err)
	}
	return err
}
