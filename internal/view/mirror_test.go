package view_test

import (
	"testing"
	"time"

	"github.com/mkappworks-dev/cloudzilla-app/internal/view"
)

func TestInAndAgo_RoundBeforeChoosingTheUnit(t *testing.T) {
	for _, tc := range []struct {
		d    time.Duration
		want string
	}{
		{30 * time.Second, "in a moment"},
		{5 * time.Minute, "in 5 minutes"},
		{40 * time.Minute, "in 40 minutes"},
		{100 * time.Minute, "in 2 hours"},
		{20 * time.Hour, "in 20 hours"},
		{time.Hour - 100*time.Millisecond, "in 1 hour"},
		{6*time.Hour - time.Second, "in 6 hours"},
		{24*time.Hour - time.Second, "in 1 day"},
		{40 * time.Hour, "in 2 days"},
	} {
		if got := view.In(time.Now().Add(tc.d)); got != tc.want {
			t.Errorf("In(+%v) = %q, want %q", tc.d, got, tc.want)
		}
	}
	if got := view.Ago(time.Now().Add(-59*time.Minute - 59*time.Second)); got != "1 hour ago" {
		t.Errorf("Ago(59m59s) = %q, want 1 hour ago", got)
	}
}

func TestMirrorRemoteLabel(t *testing.T) {
	if got := view.MirrorRemoteLabel("https://github.com/go-git/go-git.git"); got != "github.com/go-git/go-git" {
		t.Errorf("label = %q", got)
	}
}
