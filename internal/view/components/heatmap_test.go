package components

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	"github.com/a-h/templ"
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
	if strings.Contains(out, `role="img"`) {
		t.Errorf("role=img would hide the day table from assistive tech")
	}
}

func TestHeatmap_TextAlternativeListsNonZeroDays(t *testing.T) {
	day := time.Date(2026, 5, 14, 0, 0, 0, 0, time.UTC)
	counts := map[time.Time]int{day: 5, day.AddDate(0, 0, -1): 0}
	for name, c := range map[string]templ.Component{
		"year":  Heatmap(counts, 2026),
		"large": HeatmapLarge(counts, day, 13),
	} {
		var buf bytes.Buffer
		if err := c.Render(context.Background(), &buf); err != nil {
			t.Fatalf("%s render: %v", name, err)
		}
		out := buf.String()
		if !strings.Contains(out, `<th scope="row">Thu, May 14 2026</th><td>5</td>`) {
			t.Errorf("%s: missing row for the active day: %s", name, out)
		}
		if strings.Contains(out, "<th scope=\"row\">Wed, May 13 2026") {
			t.Errorf("%s: zero-commit day listed", name)
		}
		if !strings.Contains(out, `aria-hidden="true"`) || !strings.Contains(out, `title="Thu, May 14 2026 — 5 commits"`) {
			t.Errorf("%s: pointer tooltips lost or grid not hidden from assistive tech", name)
		}
	}
}

func TestHeatmapLegend_ListsEveryLevel(t *testing.T) {
	var buf bytes.Buffer
	if err := HeatmapLegend().Render(context.Background(), &buf); err != nil {
		t.Fatalf("render: %v", err)
	}
	for _, class := range heatmapLevelClasses {
		if !strings.Contains(buf.String(), class) {
			t.Errorf("legend missing %s", class)
		}
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
