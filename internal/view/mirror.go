package view

import (
	"math"
	"strconv"
	"strings"
	"time"
)

// MirrorBanner is the strip a pull mirror's repo page opens with.
type MirrorBanner struct {
	RemoteURL   string
	RemoteLabel string
	// SyncedAgo is "" before the first sync.
	SyncedAgo string
	Failed    bool
	Error     string
	NextTry   string
	// SyncURL is "" when the viewer can't sync; SettingsURL when they can't manage.
	SyncURL     string
	SettingsURL string
}

// MirrorSettings is the settings page's pull-mirror section. It never
// holds the token, only whether one is stored.
type MirrorSettings struct {
	RemoteURL, AuthUsername string
	HasToken                bool
	Intervals               []IntervalOption
	LastSynced, NextSync    string
	Failing                 bool
	LastError               string
	APIURL, SyncURL         string
}

// MirrorRemoteLabel shows a remote URL without its scheme or .git suffix.
func MirrorRemoteLabel(remote string) string {
	label := remote
	if i := strings.Index(label, "://"); i >= 0 {
		label = label[i+3:]
	}
	return strings.TrimSuffix(label, ".git")
}

// Ago is t relative to now in its largest unit, rounded: "5 minutes ago".
func Ago(t time.Time) string {
	d := time.Since(t)
	if d < time.Minute {
		return "just now"
	}
	return roundedSpan(d) + " ago"
}

// In is a future t relative to now: "in 6 hours".
func In(t time.Time) string {
	d := time.Until(t)
	if d < time.Minute {
		return "in a moment"
	}
	return "in " + roundedSpan(d)
}

// roundedSpan rounds to the minute, then moves up a unit only when the
// rounded count fills it: 59m59s reads "1 hour", 40m stays "40 minutes".
func roundedSpan(d time.Duration) string {
	if m := round(d, time.Minute); m < 60 {
		return plural(max(m, 1), "minute")
	}
	if h := round(d, time.Hour); h < 24 {
		return plural(h, "hour")
	}
	return plural(round(d, 24*time.Hour), "day")
}

func round(d, unit time.Duration) int {
	return int(math.Round(float64(d) / float64(unit)))
}

func plural(n int, name string) string {
	if n == 1 {
		return "1 " + name
	}
	return strconv.Itoa(n) + " " + name + "s"
}
