package components

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

func TestMergeabilityBox_ConflictsHidesButtons(t *testing.T) {
	d := MergeabilityBoxData{
		PatchURL:         "/api/repos/owner/repo/pulls/1",
		HasConflicts:     true,
		Mergeable:        false,
		CanFastForward:   true,
		CanThreeWayMerge: true,
		CanSquash:        true,
	}
	var buf bytes.Buffer
	if err := MergeabilityBox(d).Render(context.Background(), &buf); err != nil {
		t.Fatalf("render: %v", err)
	}
	out := buf.String()
	if strings.Contains(out, "Fast-forward") {
		t.Errorf("expected no Fast-forward button when HasConflicts")
	}
	if strings.Contains(out, ">Merge<") {
		t.Errorf("expected no Merge button when HasConflicts")
	}
	if strings.Contains(out, "Squash") {
		t.Errorf("expected no Squash button when HasConflicts")
	}
	if !strings.Contains(out, "conflicts that must be resolved") {
		t.Errorf("expected conflict status text, got: %s", out)
	}
}

func TestMergeabilityBox_RendersMergeStrategies(t *testing.T) {
	d := MergeabilityBoxData{
		PatchURL:         "/api/repos/owner/repo/pulls/1",
		Mergeable:        true,
		HasConflicts:     false,
		CanFastForward:   true,
		CanThreeWayMerge: true,
		CanSquash:        true,
	}
	var buf bytes.Buffer
	if err := MergeabilityBox(d).Render(context.Background(), &buf); err != nil {
		t.Fatalf("render: %v", err)
	}
	out := buf.String()
	patchCount := strings.Count(out, `hx-patch="/api/repos/owner/repo/pulls/1"`)
	if patchCount != 3 {
		t.Errorf("expected 3 hx-patch attrs to PatchURL, got %d. out: %s", patchCount, out)
	}
	for _, strat := range []string{`merge_strategy&#34;:&#34;ff`, `merge_strategy&#34;:&#34;merge`, `merge_strategy&#34;:&#34;squash`} {
		if !strings.Contains(out, strat) {
			t.Errorf("expected %q in output, got: %s", strat, out)
		}
	}
	if !strings.Contains(out, `hx-target="closest section"`) {
		t.Errorf("expected hx-target=closest section, got: %s", out)
	}
	if !strings.Contains(out, `hx-swap="outerHTML"`) {
		t.Errorf("expected hx-swap=outerHTML, got: %s", out)
	}
}

func TestMergeabilityBox_OmitsZeroRequiredCounts(t *testing.T) {
	d := MergeabilityBoxData{
		PatchURL:        "/api/repos/owner/repo/pulls/1",
		Mergeable:       true,
		RequiredChecks:  0,
		RequiredReviews: 0,
	}
	var buf bytes.Buffer
	if err := MergeabilityBox(d).Render(context.Background(), &buf); err != nil {
		t.Fatalf("render: %v", err)
	}
	out := buf.String()
	if strings.Contains(out, "required check") {
		t.Errorf("expected no checks <li> when RequiredChecks=0, got: %s", out)
	}
	if strings.Contains(out, "required review") {
		t.Errorf("expected no reviews <li> when RequiredReviews=0, got: %s", out)
	}
}

func TestAheadBehindDescription_Pluralizes(t *testing.T) {
	cases := []struct {
		ahead, behind int
		want          string
	}{
		{0, 0, "0 commits ahead, 0 behind"},
		{1, 1, "1 commit ahead, 1 behind"},
		{3, 5, "3 commits ahead, 5 behind"},
	}
	for _, tc := range cases {
		if got := aheadBehindDescription(tc.ahead, tc.behind); got != tc.want {
			t.Errorf("aheadBehindDescription(%d,%d) = %q, want %q", tc.ahead, tc.behind, got, tc.want)
		}
	}
}
