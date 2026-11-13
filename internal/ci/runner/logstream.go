package runner

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"sync"
	"time"

	"github.com/mkappworks-dev/cloudzilla-app/internal/ci/logmask"
)

const (
	// flushBytes is how much complete-line output waits before it is posted without the timer.
	flushBytes = 256 << 10
	// maxPartialLine is how long an unterminated line may grow before it is posted anyway, so a progress bar that
	// never prints a newline still shows up.
	maxPartialLine = 64 << 10
	postTries      = 3
)

// logSink is where a Job's log chunks go.
type logSink interface {
	PostLog(ctx context.Context, jobID int64, token string, step, seq int, data []byte) (bool, error)
}

// logStream posts a Job's output to the server in chunks of whole lines, masking secrets first. Lines are held until
// a newline arrives, so a secret cannot be split across two chunks. A failed or refused post is dropped after retries:
// a log is not worth failing a build for.
type logStream struct {
	sink   logSink
	jobID  int64
	token  string
	masker *logmask.Masker

	mu   sync.Mutex
	ctx  context.Context
	buf  []byte
	step int
	seq  int
	dead bool
	done chan struct{}
}

func newLogStream(ctx context.Context, sink logSink, jobID int64, token string, masker *logmask.Masker, every time.Duration) *logStream {
	l := &logStream{sink: sink, ctx: ctx, jobID: jobID, token: token, masker: masker, step: -1, done: make(chan struct{})}
	go l.tick(every)
	return l
}

func (l *logStream) tick(every time.Duration) {
	if every <= 0 {
		every = time.Second
	}
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		select {
		case <-l.done:
			return
		case <-t.C:
			l.mu.Lock()
			l.flush(false)
			l.mu.Unlock()
		}
	}
}

// Writer returns the writer for Step step; switching steps posts what the previous one left, so no chunk mixes two.
func (l *logStream) Writer(step int) io.Writer { return stepWriter{l, step} }

type stepWriter struct {
	l    *logStream
	step int
}

func (w stepWriter) Write(p []byte) (int, error) {
	l := w.l
	l.mu.Lock()
	defer l.mu.Unlock()
	if w.step != l.step {
		l.flush(true)
		l.step = w.step
	}
	l.buf = append(l.buf, p...)
	if len(l.buf) >= flushBytes || len(l.buf) >= maxPartialLine && !bytes.Contains(l.buf, []byte{'\n'}) {
		l.flush(false)
	}
	return len(p), nil
}

// Close posts what is left, including an unterminated last line, and stops the timer.
func (l *logStream) Close() {
	close(l.done)
	l.mu.Lock()
	defer l.mu.Unlock()
	l.flush(true)
}

// flush posts the complete lines in the buffer, or all of it with all, or an over-long partial line.
func (l *logStream) flush(all bool) {
	cut := len(l.buf)
	if !all {
		cut = bytes.LastIndexByte(l.buf, '\n') + 1
		if cut == 0 && len(l.buf) >= maxPartialLine {
			cut = len(l.buf)
		}
	}
	if cut == 0 {
		return
	}
	chunk := l.masker.MaskBytes(append([]byte(nil), l.buf[:cut]...))
	l.buf = append(l.buf[:0], l.buf[cut:]...)
	if l.dead {
		return
	}
	seq := l.seq
	l.seq++
	var err error
	for try := range postTries {
		var truncated bool
		if truncated, err = l.sink.PostLog(l.ctx, l.jobID, l.token, l.step, seq, chunk); err == nil {
			l.dead = truncated
			return
		}
		if errors.Is(err, ErrUnauthorized) || l.ctx.Err() != nil {
			break
		}
		time.Sleep(time.Duration(try+1) * 200 * time.Millisecond)
	}
	l.dead = errors.Is(err, ErrUnauthorized)
	slog.Warn("dropped a log chunk", "job", l.jobID, "seq", seq, "error", err)
}
