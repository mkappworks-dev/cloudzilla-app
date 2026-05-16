package components

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

func TestChecksList_RendersEachState(t *testing.T) {
	rows := []CheckRow{
		{Context: "ci/build", State: "success", Description: "passed in 2m", URL: "/runs/1"},
		{Context: "ci/test", State: "failure", Description: "1 of 42 failed", URL: "/runs/2"},
		{Context: "ci/lint", State: "pending", Description: "running", URL: ""},
		{Context: "ci/deploy", State: "error", Description: "timeout", URL: "/runs/4"},
		{Context: "ci/unknown", State: "", Description: "", URL: ""},
	}
	var buf bytes.Buffer
	if err := ChecksList(rows).Render(context.Background(), &buf); err != nil {
		t.Fatalf("render: %v", err)
	}
	out := buf.String()

	// context names and descriptions must appear
	for _, s := range []string{"ci/build", "ci/test", "ci/lint", "ci/deploy", "ci/unknown", "passed in 2m", "timeout"} {
		if !strings.Contains(out, s) {
			t.Errorf("missing %q", s)
		}
	}

	// rows with a URL must produce a "Details" link
	if !strings.Contains(out, "Details") {
		t.Errorf("expected at least one Details link")
	}

	// ci/lint has no URL — ensure its context still renders but check "Details"
	// count: only rows with non-empty URL (ci/build, ci/test, ci/deploy = 3) emit links
	detailsCount := strings.Count(out, "Details")
	if detailsCount != 3 {
		t.Errorf("expected 3 Details links, got %d", detailsCount)
	}

	// description-less row (ci/unknown) must not emit its description paragraph
	if strings.Contains(out, `class="text-xs text-muted-foreground mt-0.5"></p>`) {
		t.Errorf("empty description paragraph should not be rendered")
	}
}
