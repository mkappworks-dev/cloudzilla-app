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
	if !strings.Contains(out, "This branch has conflicts") {
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
	// With multiple strategies the merge action is a split button: one primary
	// button + a caret, plus the Close button — two hx-patch attrs to PatchURL.
	if patchCount := strings.Count(out, `hx-patch="/api/repos/owner/repo/pulls/1"`); patchCount != 2 {
		t.Errorf("expected 2 hx-patch attrs to PatchURL (primary merge + close), got %d. out: %s", patchCount, out)
	}
	// All three strategies are offered as radio items in the caret menu.
	if radioCount := strings.Count(out, `role="menuitemradio"`); radioCount != 3 {
		t.Errorf("expected 3 menuitemradio strategy items, got %d. out: %s", radioCount, out)
	}
	for _, label := range []string{"Squash and merge", "Create merge commit", "Fast-forward"} {
		if !strings.Contains(out, label) {
			t.Errorf("expected strategy %q in output, got: %s", label, out)
		}
	}
	// Each strategy item records its key into Alpine scope on click ('&#39;' is the escaped ').
	for _, key := range []string{"ff", "merge", "squash"} {
		if !strings.Contains(out, "strategy = &#39;"+key+"&#39;") {
			t.Errorf("expected click handler setting strategy %q, got: %s", key, out)
		}
	}
	// The selected strategy reaches the merge request through the Alpine-bound hx-vals.
	if !strings.Contains(out, "merge_strategy: strategy") {
		t.Errorf("expected :hx-vals binding carrying merge_strategy, got: %s", out)
	}
	if !strings.Contains(out, `hx-target="closest section"`) {
		t.Errorf("expected hx-target=closest section, got: %s", out)
	}
	if !strings.Contains(out, `hx-swap="outerHTML"`) {
		t.Errorf("expected hx-swap=outerHTML, got: %s", out)
	}
}

func TestMergeabilityBox_SingleStrategyPlainButton(t *testing.T) {
	d := MergeabilityBoxData{
		PatchURL:  "/api/repos/owner/repo/pulls/1",
		Mergeable: true,
		CanSquash: true, // only one strategy available
	}
	var buf bytes.Buffer
	if err := MergeabilityBox(d).Render(context.Background(), &buf); err != nil {
		t.Fatalf("render: %v", err)
	}
	out := buf.String()
	// One strategy → a plain button, no caret menu.
	if strings.Contains(out, `role="menuitemradio"`) {
		t.Errorf("expected no strategy menu with a single strategy, got: %s", out)
	}
	if strings.Contains(out, "Choose merge strategy") {
		t.Errorf("expected no caret trigger with a single strategy, got: %s", out)
	}
	if !strings.Contains(out, `merge_strategy&#34;:&#34;squash`) {
		t.Errorf("expected the squash strategy wired into hx-vals, got: %s", out)
	}
	if !strings.Contains(out, "Squash and merge") {
		t.Errorf("expected the Squash and merge label, got: %s", out)
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
		{0, 0, "0 commits ahead, 0 behind the base branch"},
		{1, 1, "1 commit ahead, 1 behind the base branch"},
		{3, 5, "3 commits ahead, 5 behind the base branch"},
	}
	for _, tc := range cases {
		if got := aheadBehindDescription(tc.ahead, tc.behind); got != tc.want {
			t.Errorf("aheadBehindDescription(%d,%d) = %q, want %q", tc.ahead, tc.behind, got, tc.want)
		}
	}
}
