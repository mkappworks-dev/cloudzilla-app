package components

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"
)

func TestHeatmap_RendersWeeksAndCells(t *testing.T) {
	today := time.Date(2026, 5, 14, 0, 0, 0, 0, time.UTC)
	counts := map[time.Time]int{
		today:                   5,
		today.AddDate(0, 0, -1): 2,
		today.AddDate(0, 0, -7): 0,
		today.AddDate(0, -1, 0): 4,
	}
	var buf bytes.Buffer
	if err := Heatmap(counts, 2026).Render(context.Background(), &buf); err != nil {
		t.Fatalf("render: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "heatmap-cell-") {
		t.Errorf("expected heatmap-cell-* class, got: %s", out)
	}
	if !strings.Contains(out, `role="img"`) {
		t.Errorf("expected role=img for a11y, got: %s", out)
	}
}

func TestHeatmap_AriaLabelIncludesTotal(t *testing.T) {
	today := time.Date(2026, 5, 14, 0, 0, 0, 0, time.UTC)
	counts := map[time.Time]int{
		today:                   3,
		today.AddDate(0, 0, -1): 4,
	}
	var buf bytes.Buffer
	if err := Heatmap(counts, 2026).Render(context.Background(), &buf); err != nil {
		t.Fatalf("render: %v", err)
	}
	if !strings.Contains(buf.String(), "7 commits") {
		t.Errorf("expected aria-label to mention total (7 commits), got: %s", buf.String())
	}
}

func TestHeatmap_CellIntensityScale(t *testing.T) {
	cases := []struct {
		n    int
		want string
	}{
		{0, "heatmap-cell-0"},
		{1, "heatmap-cell-1"},
		{2, "heatmap-cell-1"},
		{3, "heatmap-cell-2"},
		{5, "heatmap-cell-2"},
		{6, "heatmap-cell-3"},
		{9, "heatmap-cell-3"},
		{10, "heatmap-cell-4"},
		{99, "heatmap-cell-4"},
	}
	for _, tc := range cases {
		if got := heatmapCellClass(tc.n); got != tc.want {
			t.Errorf("heatmapCellClass(%d) = %s, want %s", tc.n, got, tc.want)
		}
	}
}
