package handler

import "net/http"

// Healthz is the liveness probe: it touches neither the database nor the disk.
func (h *Handler) Healthz(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write([]byte("ok\n"))
}

// Readyz is the readiness probe: 503 until the database, schema and repos
// volume are all usable.
func (h *Handler) Readyz(w http.ResponseWriter, r *http.Request) {
	report := h.Services.Health.Readiness(r.Context())
	status := http.StatusOK
	if !report.Ready() {
		status = http.StatusServiceUnavailable
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, status, report)
}
