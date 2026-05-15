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
