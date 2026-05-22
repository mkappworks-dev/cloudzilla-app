package gittransport

import (
	"errors"
	"io"
)

// ErrPackTooLarge is returned by a LimitedReadCloser once the wrapped
// stream crosses its ceiling. It is deliberately not io.EOF — which
// io.LimitReader would return — so an oversized push fails loudly
// instead of being silently truncated into a corrupt pack.
var ErrPackTooLarge = errors.New("pack exceeds maximum allowed size")

// LimitedReadCloser caps the total bytes readable from the wrapped
// reader. Once the cap is crossed every Read returns ErrPackTooLarge.
// A non-positive max disables the cap. Close is delegated.
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
	if l.Exceeded() {
		return n, ErrPackTooLarge
	}
	return n, err
}

func (l *LimitedReadCloser) Close() error { return l.r.Close() }

// Exceeded reports whether the cap has been crossed. Handlers use it to
// map the failure onto a "too large" response rather than a generic
// 500, independent of how go-git wraps the underlying read error.
func (l *LimitedReadCloser) Exceeded() bool {
	return l.max > 0 && l.n > l.max
}
