package devreload

import "time"

// SetDebounce lets a test widen the window past the scheduling stalls of a loaded CI runner.
func (h *Hub) SetDebounce(d time.Duration) { h.debounce = d }
