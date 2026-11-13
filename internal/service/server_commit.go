package service

import (
	"context"
	"errors"
	"log/slog"
	"sync/atomic"
	"time"

	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/go-git/go-git/v5/plumbing/storer"
	"github.com/mkappworks-dev/cloudzilla-app/internal/secretbox"
)

// serverCommitter stores the commits the server writes itself: the author stays the acting user, the committer is
// the instance, and the commit is signed with the instance key when one is available (ADR 0002). The zero value
// stores instance-committed, unsigned commits.
type serverCommitter struct {
	signer *InstanceSigner
	host   string
}

func newServerCommitter(signer *InstanceSigner, baseURL string) serverCommitter {
	return serverCommitter{signer: signer, host: noreplyHostFromBaseURL(baseURL)}
}

func (c serverCommitter) identity(when time.Time) object.Signature {
	host := c.host
	if host == "" {
		host = defaultNoreplyHost
	}
	return object.Signature{Name: instanceSignerName, Email: "noreply@" + host, When: when}
}

// undecryptableWarned keeps a sealed-under-another-key instance key from logging on every server-made commit.
var undecryptableWarned atomic.Bool

// noteInstanceKeyOutcome warns once per outage; a readable key re-arms the warning.
func noteInstanceKeyOutcome(err error) {
	if !errors.Is(err, secretbox.ErrUndecryptable) {
		undecryptableWarned.Store(false)
		return
	}
	if undecryptableWarned.CompareAndSwap(false, true) {
		slog.Warn("instance signing key is sealed under another security.secret_key; committing unsigned")
	}
}

// store sets commit's committer, signs it and writes it to st. Without a usable key the commit is stored unsigned,
// so a missing or foreign security.secret_key never blocks a merge or an edit.
func (c serverCommitter) store(st storer.EncodedObjectStorer, commit *object.Commit) (plumbing.Hash, error) {
	commit.Committer = c.identity(commit.Author.When)
	commit.PGPSignature = ""
	if c.signer != nil {
		payload, err := signedPayload(commit)
		if err != nil {
			return plumbing.ZeroHash, err
		}
		// The server-made call paths take no context.
		sig, err := c.signer.Sign(context.Background(), payload)
		noteInstanceKeyOutcome(err)
		switch {
		case err == nil:
			commit.PGPSignature = sig
		case errors.Is(err, ErrNoSecretKey):
		case errors.Is(err, secretbox.ErrUndecryptable):
		default:
			return plumbing.ZeroHash, err
		}
	}
	obj := st.NewEncodedObject()
	if err := commit.Encode(obj); err != nil {
		return plumbing.ZeroHash, err
	}
	return st.SetEncodedObject(obj)
}
