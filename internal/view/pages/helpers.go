package pages

import "strconv"

// pct returns the integer percentage of part out of total as a string.
// Returns "0" when total is zero to avoid division by zero.
func pct(part, total int) string {
	if total == 0 {
		return "0"
	}
	return strconv.Itoa(part * 100 / total)
}
