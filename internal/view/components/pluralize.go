package components

import "strconv"

// Pluralize takes both forms because English plurals aren't regular
// (repository/repositories, reply/replies).
func Pluralize(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}

// Count renders n with its noun: "1 entry", "12 entries".
func Count(n int, one, many string) string {
	return strconv.Itoa(n) + " " + Pluralize(n, one, many)
}
