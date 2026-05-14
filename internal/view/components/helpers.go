package components

// Mirrors view.Percent; lives here to avoid an import cycle (view imports components).
func Percent(part, total int) int {
	if total == 0 {
		return 0
	}
	return part * 100 / total
}
