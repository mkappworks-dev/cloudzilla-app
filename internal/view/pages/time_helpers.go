package pages

import (
	"strconv"
	"time"
)

func relativeTime(t time.Time) string {
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		m := int(d / time.Minute)
		if m == 1 {
			return "1 minute ago"
		}
		return strconv.Itoa(m) + " minutes ago"
	case d < 24*time.Hour:
		h := int(d / time.Hour)
		if h == 1 {
			return "1 hour ago"
		}
		return strconv.Itoa(h) + " hours ago"
	case d < 48*time.Hour:
		return "yesterday"
	case d < 7*24*time.Hour:
		return strconv.Itoa(int(d/(24*time.Hour))) + " days ago"
	default:
		return t.Format("Jan 2, 2006")
	}
}
