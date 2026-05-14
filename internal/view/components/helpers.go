package components

// Percent returns part*100/total, returning 0 if total is zero.
// This is a copy of view.Percent kept inside the components package so
// component templates can format progress ratios without importing the
// parent view package (which would create an import cycle once the
// view package imports components for richer HomeData fields).
func Percent(part, total int) int {
	if total == 0 {
		return 0
	}
	return part * 100 / total
}
