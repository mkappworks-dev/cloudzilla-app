package gittransport

import (
	"io"
	"sync/atomic"

	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/protocol/packp"
)

// ByteCounter wraps an io.ReadCloser and atomically tracks bytes read.
type ByteCounter struct {
	r io.ReadCloser
	n int64
}

func NewByteCounter(r io.ReadCloser) *ByteCounter {
	return &ByteCounter{r: r}
}

func (c *ByteCounter) Read(p []byte) (int, error) {
	n, err := c.r.Read(p)
	if n > 0 {
		atomic.AddInt64(&c.n, int64(n))
	}
	return n, err
}

func (c *ByteCounter) Close() error { return c.r.Close() }

// Bytes returns the running total; safe to call concurrently with Read.
func (c *ByteCounter) Bytes() int64 { return atomic.LoadInt64(&c.n) }

// CountRefStatus tallies per-ref ok/failed outcomes from a receive-pack
// report; a nil report yields (0, 0).
func CountRefStatus(status *packp.ReportStatus) (ok, failed int) {
	if status == nil {
		return 0, 0
	}
	for _, cs := range status.CommandStatuses {
		if cs.Error() == nil {
			ok++
		} else {
			failed++
		}
	}
	return ok, failed
}

// AppliedCommands returns the subset of commands that the receive-pack
// report did not mark as failed. go-git reports a per-ref failure (a
// create-race, a storer error) only in status, not as a ReceivePack
// error, so callers must filter before running push side effects.
func AppliedCommands(status *packp.ReportStatus, commands []*packp.Command) []*packp.Command {
	if status == nil {
		return commands
	}
	failed := make(map[plumbing.ReferenceName]struct{})
	for _, cs := range status.CommandStatuses {
		if cs.Error() != nil {
			failed[cs.ReferenceName] = struct{}{}
		}
	}
	if len(failed) == 0 {
		return commands
	}
	applied := make([]*packp.Command, 0, len(commands))
	for _, c := range commands {
		if _, bad := failed[c.Name]; !bad {
			applied = append(applied, c)
		}
	}
	return applied
}
