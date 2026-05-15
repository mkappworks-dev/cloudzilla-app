package components

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

func TestSparkline_SevenValues(t *testing.T) {
	var buf bytes.Buffer
	if err := Sparkline([]int{1, 2, 3, 4, 5, 6, 7}, 0, 0).Render(context.Background(), &buf); err != nil {
		t.Fatalf("render: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "<polyline") {
		t.Errorf("expected <polyline in output, got: %s", out)
	}
	pts := ""
	if i := strings.Index(out, `points="`); i >= 0 {
		rest := out[i+len(`points="`):]
		if j := strings.Index(rest, `"`); j >= 0 {
			pts = rest[:j]
		}
	}
	if strings.Count(pts, ",") < 7 {
		t.Errorf("expected at least 7 coordinate pairs in points, got: %q", pts)
	}
}

func TestSparkline_Empty(t *testing.T) {
	var buf bytes.Buffer
	if err := Sparkline(nil, 0, 0).Render(context.Background(), &buf); err != nil {
		t.Fatalf("render: %v", err)
	}
	if !strings.Contains(buf.String(), "—") {
		t.Errorf("expected em-dash for empty values, got: %s", buf.String())
	}
}
