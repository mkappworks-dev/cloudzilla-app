package gittransport_test

import (
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/mkappworks-dev/cloudzilla-app/internal/gittransport"
)

func TestLimitedReadCloser_UnderLimit(t *testing.T) {
	const payload = "small pack payload"
	l := gittransport.NewLimitedReadCloser(io.NopCloser(strings.NewReader(payload)), 1024)

	got, err := io.ReadAll(l)
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	if string(got) != payload {
		t.Fatalf("payload mismatch: want %q, got %q", payload, got)
	}
	if l.Exceeded() {
		t.Fatal("Exceeded() true for a stream within the cap")
	}
}

func TestLimitedReadCloser_OverLimitFailsLoudly(t *testing.T) {
	payload := strings.Repeat("x", 5000)
	l := gittransport.NewLimitedReadCloser(io.NopCloser(strings.NewReader(payload)), 1024)

	_, err := io.ReadAll(l)
	if !errors.Is(err, gittransport.ErrPackTooLarge) {
		t.Fatalf("ReadAll: want ErrPackTooLarge, got %v", err)
	}
	if !l.Exceeded() {
		t.Fatal("Exceeded() false after the cap was crossed")
	}
}

func TestLimitedReadCloser_CrossingReadYieldsNoBytes(t *testing.T) {
	// The read that crosses the cap must return (0, ErrPackTooLarge), not
	// (n, err) — otherwise io.ReadFull would treat it as a satisfied read
	// and swallow the error, letting an over-limit pack parse as success.
	l := gittransport.NewLimitedReadCloser(io.NopCloser(strings.NewReader(strings.Repeat("x", 20))), 10)

	buf := make([]byte, 8)
	if n, err := l.Read(buf); n != 8 || err != nil {
		t.Fatalf("first read: want (8, nil), got (%d, %v)", n, err)
	}
	n, err := l.Read(buf) // 8 -> 16, crosses the cap of 10
	if n != 0 || !errors.Is(err, gittransport.ErrPackTooLarge) {
		t.Fatalf("crossing read: want (0, ErrPackTooLarge), got (%d, %v)", n, err)
	}
}

func TestLimitedReadCloser_ExactLimitAllowed(t *testing.T) {
	payload := strings.Repeat("x", 1024)
	l := gittransport.NewLimitedReadCloser(io.NopCloser(strings.NewReader(payload)), 1024)

	got, err := io.ReadAll(l)
	if err != nil {
		t.Fatalf("ReadAll at exactly the cap: %v", err)
	}
	if len(got) != 1024 || l.Exceeded() {
		t.Fatalf("a stream exactly at the cap must pass: len=%d exceeded=%v", len(got), l.Exceeded())
	}
}

func TestLimitedReadCloser_ZeroMaxDisablesCap(t *testing.T) {
	payload := strings.Repeat("x", 100000)
	l := gittransport.NewLimitedReadCloser(io.NopCloser(strings.NewReader(payload)), 0)

	got, err := io.ReadAll(l)
	if err != nil {
		t.Fatalf("ReadAll with cap disabled: %v", err)
	}
	if len(got) != len(payload) || l.Exceeded() {
		t.Fatal("non-positive max must disable the cap")
	}
}

func TestLimitedReadCloser_ClosePropagates(t *testing.T) {
	sentinel := errors.New("close failed")
	src := &errCloser{Reader: strings.NewReader(""), closeErr: sentinel}

	l := gittransport.NewLimitedReadCloser(src, 16)
	if err := l.Close(); !errors.Is(err, sentinel) {
		t.Fatalf("Close: want %v, got %v", sentinel, err)
	}
}
