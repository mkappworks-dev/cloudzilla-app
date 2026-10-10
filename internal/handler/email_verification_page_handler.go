package handler

import (
	"log/slog"
	"net/http"
)

// PageVerifyEmail handles GET /verify-email. It only checks the token: mail
// scanners fetch links, and a GET that spent it would leave the person a dead link.
func (h *Handler) PageVerifyEmail(w http.ResponseWriter, r *http.Request) {
	token := r.URL.Query().Get("token")
	link, err := h.Services.EmailVerifier.Check(r.Context(), token)
	if err != nil {
		slog.Error("check verification token", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	h.renderVerifyEmail(w, r, link, token)
}
