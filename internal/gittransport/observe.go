package gittransport

import (
	"io"
	"sync/atomic"

	"github.com/go-git/go-git/v5/plumbing/protocol/packp"
)

// ByteCounter wraps an io.ReadCloser and atomically tracks bytes read,
// so receive-pack handlers can report pack size after a push.
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

// CountRefStatus tallies per-ref outcomes from a receive-pack report so
// the observability log can distinguish a fully-applied push from one
// where some ref updates were rejected. A nil report yields (0, 0).
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
