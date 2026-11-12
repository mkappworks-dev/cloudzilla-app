// Package timefmt is the one place a page turns a time.Time into text. Absolute output is always UTC so the same
// instant reads the same on every page, whatever zone the server or a git commit carries.
package timefmt

import (
	"strconv"
	"time"
)

const (
	dateLayout      = "Jan 2, 2006"
	dateTimeLayout  = "Jan 2, 2006 15:04 UTC"
	monthYearLayout = "Jan 2006"
	relativeWindow  = 7 * 24 * time.Hour
)

// Relative is for activity: "3 minutes ago" within a week, then the absolute Date.
func Relative(t time.Time) string {
	return relativeAt(t, time.Now())
}

func relativeAt(t, now time.Time) string {
	if t.IsZero() {
		return ""
	}
	d := now.Sub(t)
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return plural(int(d/time.Minute), "minute") + " ago"
	case d < 24*time.Hour:
		return plural(int(d/time.Hour), "hour") + " ago"
	case d < 48*time.Hour:
		return "yesterday"
	case d < relativeWindow:
		return plural(int(d/(24*time.Hour)), "day") + " ago"
	default:
		return Date(t)
	}
}

// Date is for a calendar day that stays true forever: "Nov 6, 2026".
func Date(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(dateLayout)
}

// DateTime is for moments where the clock matters, such as expiries, audit rows and commits: "Nov 6, 2026 17:55 UTC".
func DateTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(dateTimeLayout)
}

// MonthYear is for coarse milestones such as "Joined Nov 2026".
func MonthYear(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(monthYearLayout)
}

func plural(n int, unit string) string {
	if n == 1 {
		return "1 " + unit
	}
	return strconv.Itoa(n) + " " + unit + "s"
}
