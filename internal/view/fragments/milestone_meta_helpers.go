package fragments

import "time"

// milestoneDueInput formats a milestone due date for an <input type="date">,
// returning "" for an open-ended milestone.
func milestoneDueInput(t *time.Time) string {
	if t == nil {
		return ""
	}
	return t.Format("2006-01-02")
}
