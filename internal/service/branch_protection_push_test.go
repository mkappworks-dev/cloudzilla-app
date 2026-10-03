package service

import (
	"testing"
	"time"
)

func TestIsFastForward_PushShapes(t *testing.T) {
	dates := []struct {
		name string
		step time.Duration
	}{
		{"a minute apart", time.Minute},
		{"in one second", 0},
	}
	for _, shape := range pushShapes {
		for _, d := range dates {
			t.Run(shape.name+", commits "+d.name, func(t *testing.T) {
				h := newPushHistory(t, t.TempDir(), "tester@example.com", d.step)
				p := shape.build(h, h.line(3))

				if got := isFastForward(h.repo, p.commands()[0]); got == p.forced {
					t.Errorf("isFastForward = %v, want %v", got, !p.forced)
				}
			})
		}
	}
}

// Commits from a machine whose clock is behind are dated before the tip they
// build on, so the walk can't stop at commits older than the old tip.
func TestIsFastForward_PushDatedBeforeTheOldTip(t *testing.T) {
	h := newPushHistory(t, t.TempDir(), "tester@example.com", time.Minute)
	old := h.line(3)
	tip := old
	for i := range 10 {
		tip = h.commitAt(h.when.Add(time.Duration(i-60)*time.Minute), "Pushed", tip)
	}

	if !isFastForward(h.repo, testPush{old: old, new: tip}.commands()[0]) {
		t.Error("isFastForward = false, want true")
	}
}
