package main

import (
	"fmt"
	"regexp"
	"time"
)

// heatmapRun matches the unbroken run of day cells a HeatmapLarge grid renders.
var heatmapRun = regexp.MustCompile(`(?:<div class="rounded-\[2px\] heatmap-cell-\d" title="[^"]*"></div>)+`)

var heatmapCell = regexp.MustCompile(`<div class="rounded-\[2px\] heatmap-cell-\d" title=`)

// freezeHeatmaps rewrites the day cells of every HeatmapLarge grid onto the fixture clock. The template passes
// time.Now() to HeatmapLarge, so its window and its day titles move with the wall clock and CI's stale check would fail
// the next day; the cell levels follow a fixed pattern because no fixture date can match a moving window.
func freezeHeatmaps(html []byte) []byte {
	return heatmapRun.ReplaceAllFunc(html, func(run []byte) []byte {
		cells := len(heatmapCell.FindAll(run, -1))
		start := fixedNow.UTC().Truncate(24*time.Hour).AddDate(0, 0, -7*(cells/7-1))
		for start.Weekday() != time.Sunday {
			start = start.AddDate(0, 0, -1)
		}
		out := make([]byte, 0, len(run))
		for i := 0; i < cells; i++ {
			commits := []int{0, 0, 1, 0, 4, 0, 7, 12, 0, 2, 0}[(i*i+i*5+i/7)%11]
			out = append(out, fmt.Sprintf(`<div class="rounded-[2px] %s" title="%s"></div>`, heatmapLevel(commits), heatmapDayTitle(start.AddDate(0, 0, i), commits))...)
		}
		return out
	})
}

// heatmapLevel and heatmapDayTitle repeat the unexported helpers in components/heatmap.templ.
func heatmapLevel(n int) string {
	switch {
	case n == 0:
		return "heatmap-cell-0"
	case n < 3:
		return "heatmap-cell-1"
	case n < 6:
		return "heatmap-cell-2"
	case n < 10:
		return "heatmap-cell-3"
	default:
		return "heatmap-cell-4"
	}
}

func heatmapDayTitle(day time.Time, n int) string {
	unit := "commits"
	if n == 1 {
		unit = "commit"
	}
	return fmt.Sprintf("%s — %d %s", day.Format("Mon, Jan 2 2006"), n, unit)
}

var compactAgo = regexp.MustCompile(`Updated \d+y ago`)

// freezeCompactAgo replaces account_repos.templ's compactRelative output, which reads time.Since and would tick from
// "1y ago" to "2y ago" in mid-2027; the replacements cycle so the rows do not all read the same.
func freezeCompactAgo(html []byte) []byte {
	ages := []string{"2d", "1w", "3w", "2mo", "5mo"}
	i := 0
	return compactAgo.ReplaceAllFunc(html, func([]byte) []byte {
		i++
		return []byte("Updated " + ages[(i-1)%len(ages)] + " ago")
	})
}
