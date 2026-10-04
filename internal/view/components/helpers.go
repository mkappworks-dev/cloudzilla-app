package components

import "encoding/hex"

// Mirrors view.Percent; lives here to avoid an import cycle (view imports components).
func Percent(part, total int) int {
	if total == 0 {
		return 0
	}
	return part * 100 / total
}

// RefLabel shortens a full commit SHA for display. Links keep the full ref,
// since the code browser can't resolve an abbreviated SHA.
func RefLabel(ref string) string {
	if _, err := hex.DecodeString(ref); err == nil && len(ref) == 40 {
		return ref[:7]
	}
	return ref
}
