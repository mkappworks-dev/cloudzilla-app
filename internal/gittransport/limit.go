package gittransport

import (
	"errors"
	"io"
)

// ErrPackTooLarge is returned by a LimitedReadCloser once the wrapped
// stream crosses its ceiling. It is deliberately not io.EOF, so an
// oversized push fails loudly instead of truncating into a corrupt pack.
var ErrPackTooLarge = errors.New("pack exceeds maximum allowed size")

// LimitedReadCloser caps the total bytes readable from the wrapped
// reader. Once the cap is crossed every Read returns (0, ErrPackTooLarge);
// a non-positive max disables the cap.
type LimitedReadCloser struct {
	r   io.ReadCloser
	max int64
	n   int64
}

func NewLimitedReadCloser(r io.ReadCloser, max int64) *LimitedReadCloser {
	return &LimitedReadCloser{r: r, max: max}
}

func (l *LimitedReadCloser) Read(p []byte) (int, error) {
	if l.Exceeded() {
		return 0, ErrPackTooLarge
	}
	n, err := l.r.Read(p)
	l.n += int64(n)
	// Return (0, ...) on the crossing read, not (n, ...): io.ReadFull
	// clears the error of a read that filled its buffer, which would let
	// go-git finish parsing an over-limit pack as if it had succeeded.
	if l.Exceeded() {
		return 0, ErrPackTooLarge
	}
	return n, err
}

func (l *LimitedReadCloser) Close() error { return l.r.Close() }

// Exceeded reports whether the cap has been crossed.
func (l *LimitedReadCloser) Exceeded() bool {
	return l.max > 0 && l.n > l.max
}
