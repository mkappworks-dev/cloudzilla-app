package gittransport_test

import (
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/mkappworks-dev/cloudzilla-app/internal/gittransport"
)

func TestLimitedReadCloser_LimitToCountsOnlyWhatComesNext(t *testing.T) {
	l := gittransport.NewLimitedReadCloser(io.NopCloser(strings.NewReader(strings.Repeat("x", 100))), 1000)
	if _, err := io.ReadFull(l, make([]byte, 10)); err != nil {
		t.Fatal(err)
	}

	if !l.LimitTo(20) {
		t.Fatal("LimitTo(20) under a 1000 byte cap: want it to report that it lowered the cap")
	}
	if _, err := io.ReadFull(l, make([]byte, 20)); err != nil {
		t.Fatalf("20 more bytes fit: %v", err)
	}
	if _, err := l.Read(make([]byte, 1)); !errors.Is(err, gittransport.ErrPackTooLarge) {
		t.Fatalf("the 21st byte: want ErrPackTooLarge, got %v", err)
	}
}

func TestLimitedReadCloser_LimitToZeroRefusesAnyFurtherByte(t *testing.T) {
	l := gittransport.NewLimitedReadCloser(io.NopCloser(strings.NewReader("abcdef")), 0)
	if _, err := io.ReadFull(l, make([]byte, 3)); err != nil {
		t.Fatal(err)
	}

	l.LimitTo(0)

	if _, err := l.Read(make([]byte, 1)); !errors.Is(err, gittransport.ErrPackTooLarge) {
		t.Fatalf("want ErrPackTooLarge, got %v", err)
	}
}

func TestLimitedReadCloser_LimitToZeroLetsAnEmptyRemainderThrough(t *testing.T) {
	l := gittransport.NewLimitedReadCloser(io.NopCloser(strings.NewReader("abc")), 100)
	if _, err := io.ReadFull(l, make([]byte, 3)); err != nil {
		t.Fatal(err)
	}

	l.LimitTo(0)

	if _, err := io.ReadAll(l); err != nil {
		t.Fatalf("a stream with nothing left must still end cleanly: %v", err)
	}
	if l.Exceeded() {
		t.Fatal("Exceeded() true although nothing more was read")
	}
}

func TestLimitedReadCloser_LimitToKeepsTheTighterCap(t *testing.T) {
	l := gittransport.NewLimitedReadCloser(io.NopCloser(strings.NewReader("abc")), 50)

	if l.LimitTo(500) {
		t.Fatal("LimitTo(500) under a 50 byte cap: want false, the cap was already tighter")
	}
}
