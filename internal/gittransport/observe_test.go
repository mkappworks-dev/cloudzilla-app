package gittransport_test

import (
	"errors"
	"io"
	"strings"
	"sync"
	"testing"

	"github.com/mkappworks-dev/cloudzilla-app/internal/gittransport"
)

func TestByteCounter_TracksReadsAndClose(t *testing.T) {
	const payload = "hello, thin pack receiver\n"
	src := io.NopCloser(strings.NewReader(payload))

	c := gittransport.NewByteCounter(src)

	if got := c.Bytes(); got != 0 {
		t.Fatalf("Bytes() before Read: want 0, got %d", got)
	}

	buf := make([]byte, 8)
	n, err := c.Read(buf)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if n != 8 {
		t.Fatalf("Read: want n=8, got %d", n)
	}
	if got := c.Bytes(); got != 8 {
		t.Fatalf("Bytes() after first Read: want 8, got %d", got)
	}

	rest, err := io.ReadAll(c)
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	if want := int64(len(payload)); c.Bytes() != want {
		t.Fatalf("Bytes() after ReadAll: want %d, got %d", want, c.Bytes())
	}
	if string(buf[:n])+string(rest) != payload {
		t.Fatalf("payload mismatch: want %q, got %q", payload, string(buf[:n])+string(rest))
	}

	if err := c.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
}

func TestByteCounter_ClosePropagatesError(t *testing.T) {
	sentinel := errors.New("close failed")
	src := &errCloser{Reader: strings.NewReader(""), closeErr: sentinel}

	c := gittransport.NewByteCounter(src)
	if err := c.Close(); !errors.Is(err, sentinel) {
		t.Fatalf("Close: want %v, got %v", sentinel, err)
	}
}

type errCloser struct {
	io.Reader
	closeErr error
}

func (e *errCloser) Close() error { return e.closeErr }

func TestByteCounter_ConcurrentBytesRead(t *testing.T) {
	const payload = "hello, thin pack receiver\n"
	src := io.NopCloser(strings.NewReader(strings.Repeat(payload, 100)))
	c := gittransport.NewByteCounter(src)

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for range 1000 {
			_ = c.Bytes()
		}
	}()

	if _, err := io.Copy(io.Discard, c); err != nil {
		t.Fatalf("Copy: %v", err)
	}
	wg.Wait()

	if want := int64(len(payload) * 100); c.Bytes() != want {
		t.Fatalf("Bytes() after concurrent run: want %d, got %d", want, c.Bytes())
	}
}
