package components

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

func TestMiniChart_ThreeBars(t *testing.T) {
	var buf bytes.Buffer
	if err := MiniChart([]int{10, 20, 30}, 0, 0).Render(context.Background(), &buf); err != nil {
		t.Fatalf("render: %v", err)
	}
	out := buf.String()
	if strings.Count(out, "<rect") != 3 {
		t.Errorf("expected 3 <rect elements, got: %s", out)
	}
}

func TestMiniChart_Empty(t *testing.T) {
	var buf bytes.Buffer
	if err := MiniChart(nil, 0, 0).Render(context.Background(), &buf); err != nil {
		t.Fatalf("render: %v", err)
	}
	if !strings.Contains(buf.String(), "no data") {
		t.Errorf("expected 'no data' for empty values, got: %s", buf.String())
	}
}

func TestMiniBars_EmptyWeekKeepsABaseline(t *testing.T) {
	bars := miniBars([]int{0, 4, 0}, 30, 32)
	if bars[0].H != 1 || bars[2].H != 1 || bars[1].H != 32 {
		t.Errorf("heights = %v, %v, %v; want 1, 32, 1", bars[0].H, bars[1].H, bars[2].H)
	}
}
