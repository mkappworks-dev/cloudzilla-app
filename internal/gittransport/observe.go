package gittransport

import (
	"io"
	"sync/atomic"
)

// ByteCounter wraps an io.ReadCloser and atomically tracks the total
// number of bytes successfully read so far. Close is delegated to the
// underlying reader.
//
// Used by the receive-pack handlers to surface pack size in the
// post-push observability log line.
type ByteCounter struct {
	r io.ReadCloser
	n int64
}

// NewByteCounter wraps r. The returned counter exposes a running byte
// total via Bytes() and forwards Close() to r.
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

// Bytes returns the total number of bytes read so far. Safe to call
// concurrently with Read.
func (c *ByteCounter) Bytes() int64 { return atomic.LoadInt64(&c.n) }
