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
	}
	var buf bytes.Buffer
	if err := ChecksList(rows).Render(context.Background(), &buf); err != nil {
		t.Fatalf("render: %v", err)
	}
	out := buf.String()
	for _, s := range []string{"ci/build", "ci/test", "ci/lint", "passed in 2m"} {
		if !strings.Contains(out, s) {
			t.Errorf("missing %q", s)
		}
	}
}
