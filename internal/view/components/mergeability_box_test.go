package components

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

func TestMergeabilityBox_ConflictsHidesButtons(t *testing.T) {
	d := MergeabilityBoxData{
		CanWrite:         true,
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
		CanWrite:         true,
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
	// With multiple strategies the merge action is a split button: only the
	// primary button patches; the caret opens the menu.
	if patchCount := strings.Count(out, `hx-patch="/api/repos/owner/repo/pulls/1"`); patchCount != 1 {
		t.Errorf("expected 1 hx-patch attr to PatchURL (primary merge), got %d. out: %s", patchCount, out)
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
	// The response has no body: the page reloads, so the merge box is never replaced by a stale fragment.
	if !strings.Contains(out, `hx-swap="none"`) || !strings.Contains(out, "location.reload()") {
		t.Errorf("expected hx-swap=none and a reload after merging, got: %s", out)
	}
}

func TestMergeabilityBox_SingleStrategyPlainButton(t *testing.T) {
	d := MergeabilityBoxData{
		CanWrite:  true,
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

func TestMergeabilityBox_WriterCloseAsksFirst(t *testing.T) {
	d := MergeabilityBoxData{CanWrite: true, PatchURL: "/api/repos/owner/repo/pulls/1", Mergeable: true, CanSquash: true}
	var buf bytes.Buffer
	if err := MergeabilityBox(d).Render(context.Background(), &buf); err != nil {
		t.Fatalf("render: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "document.getElementById('close-pull-dialog').showModal()") {
		t.Errorf("expected Close to open close-pull-dialog, got: %s", out)
	}
	if strings.Contains(out, "&#34;state&#34;:&#34;closed&#34;") {
		t.Errorf("expected Close not to patch the pull directly, got: %s", out)
	}
}

func TestMergeabilityBox_ReaderSeesNoActions(t *testing.T) {
	d := MergeabilityBoxData{
		PatchURL:         "/api/repos/owner/repo/pulls/1",
		Mergeable:        true,
		CanFastForward:   true,
		CanThreeWayMerge: true,
		CanSquash:        true,
	}
	var buf bytes.Buffer
	if err := MergeabilityBox(d).Render(context.Background(), &buf); err != nil {
		t.Fatalf("render: %v", err)
	}
	out := buf.String()
	for _, unwanted := range []string{"<button", "hx-patch", "Close pull request"} {
		if strings.Contains(out, unwanted) {
			t.Errorf("expected no %q for a reader, got: %s", unwanted, out)
		}
	}
	if !strings.Contains(out, "Ready to merge") {
		t.Errorf("expected the mergeability status for a reader, got: %s", out)
	}
	if !strings.Contains(out, "Only collaborators with write access can merge or close this pull request.") {
		t.Errorf("expected the read-only line, got: %s", out)
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

func renderBox(t *testing.T, d MergeabilityBoxData) string {
	t.Helper()
	var buf bytes.Buffer
	if err := MergeabilityBox(d).Render(context.Background(), &buf); err != nil {
		t.Fatalf("render: %v", err)
	}
	return buf.String()
}

func writerBox() MergeabilityBoxData {
	return MergeabilityBoxData{
		CanWrite: true, PatchURL: "/api/repos/o/r/pulls/1", ChecksURL: "/o/r/pulls/1/checks",
		Mergeable: true, Ahead: 2, CanFastForward: true, CanThreeWayMerge: true, CanSquash: true,
	}
}

func TestMergeabilityBox_NeedsReviewOffersAutoMergeOnly(t *testing.T) {
	d := writerBox()
	d.RequiredReviews, d.ApprovedReviews = 2, 1
	out := renderBox(t, d)
	for _, want := range []string{"Review required", "1 of 2 required reviews approved", `auto_merge&#34;:&#34;enable`, "Enable auto-merge"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in %s", want, out)
		}
	}
	if strings.Contains(out, `state&#34;:&#34;merged`) {
		t.Error("merge must not be offered while a review is missing")
	}
}

func TestMergeabilityBox_ChecksFailingDisablesMergeAndLinksChecks(t *testing.T) {
	d := writerBox()
	d.RequiredChecks, d.PassingChecks, d.FailingChecks = 2, 1, 1
	out := renderBox(t, d)
	for _, want := range []string{"Checks failing", "View checks →", `href="/o/r/pulls/1/checks"`, "disabled"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q", want)
		}
	}
	if strings.Contains(out, "Enable auto-merge") || strings.Contains(out, "auto_merge") {
		t.Error("failing checks must not offer auto-merge")
	}
}

func TestMergeabilityBox_ChecksPendingOffersAutoMerge(t *testing.T) {
	d := writerBox()
	d.RequiredChecks, d.PassingChecks = 2, 1
	out := renderBox(t, d)
	if !strings.Contains(out, "Waiting on checks") || !strings.Contains(out, "Enable auto-merge") {
		t.Errorf("pending checks should offer auto-merge: %s", out)
	}
}

func TestMergeabilityBox_ChangesRequestedBlocksEverything(t *testing.T) {
	d := writerBox()
	d.ChangesRequested = true
	out := renderBox(t, d)
	if !strings.Contains(out, "Changes requested") {
		t.Error("missing the changes-requested header")
	}
	if strings.Contains(out, "hx-patch") || strings.Contains(out, "Enable auto-merge") {
		t.Errorf("no merge or auto-merge while changes are requested: %s", out)
	}
}

func TestMergeabilityBox_AutoMergeEnabledNamesWhoAndHow(t *testing.T) {
	d := writerBox()
	d.AutoMergeEnabled, d.AutoMergeStrategy, d.AutoMergeByName = true, "squash", "ada"
	out := renderBox(t, d)
	for _, want := range []string{"squash and merge", "armed by @ada", "Disable auto-merge", `auto_merge&#34;:&#34;disable`} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q", want)
		}
	}
}

func TestMergeabilityBox_ReadOnlyViewerHasNoActions(t *testing.T) {
	d := writerBox()
	d.CanWrite = false
	d.RequiredReviews, d.ApprovedReviews = 2, 1
	out := renderBox(t, d)
	if strings.Contains(out, "hx-patch") || !strings.Contains(out, "Only collaborators with write access") {
		t.Errorf("read-only viewer must get the lock line only: %s", out)
	}
}

func TestMergeabilityBox_DraftOffersReadyForReviewNotMerge(t *testing.T) {
	d := writerBox()
	d.Draft = true
	out := renderBox(t, d)
	for _, want := range []string{">Draft<", "Ready for review", `is_draft&#34;:&#34;false`} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in %s", want, out)
		}
	}
	if strings.Contains(out, "merge_strategy") || strings.Contains(out, "Enable auto-merge") {
		t.Error("a draft must not offer merge or auto-merge")
	}
}
