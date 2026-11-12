package timefmt

import (
	"testing"
	"time"
)

func TestRelativeAt(t *testing.T) {
	now := time.Date(2026, 11, 12, 12, 0, 0, 0, time.UTC)
	cases := []struct {
		name string
		age  time.Duration
		want string
	}{
		{"seconds", 30 * time.Second, "just now"},
		{"future skew", -time.Minute, "just now"},
		{"one minute", time.Minute, "1 minute ago"},
		{"minutes", 3 * time.Minute, "3 minutes ago"},
		{"one hour", time.Hour, "1 hour ago"},
		{"hours", 5 * time.Hour, "5 hours ago"},
		{"yesterday", 30 * time.Hour, "yesterday"},
		{"days", 3 * 24 * time.Hour, "3 days ago"},
		{"past the window", 8 * 24 * time.Hour, "Nov 4, 2026"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := relativeAt(now.Add(-tc.age), now); got != tc.want {
				t.Errorf("relativeAt(-%s) = %q, want %q", tc.age, got, tc.want)
			}
		})
	}
	if got := relativeAt(time.Time{}, now); got != "" {
		t.Errorf("zero time = %q, want empty", got)
	}
}

func TestAbsoluteIsUTC(t *testing.T) {
	cest := time.FixedZone("CET", 3600)
	ts := time.Date(2026, 11, 3, 17, 55, 0, 0, cest)
	if got, want := Date(ts), "Nov 3, 2026"; got != want {
		t.Errorf("Date = %q, want %q", got, want)
	}
	if got, want := DateTime(ts), "Nov 3, 2026 16:55 UTC"; got != want {
		t.Errorf("DateTime = %q, want %q", got, want)
	}
	if got, want := MonthYear(ts), "Nov 2026"; got != want {
		t.Errorf("MonthYear = %q, want %q", got, want)
	}
}
