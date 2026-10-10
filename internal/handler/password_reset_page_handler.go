package handler

import (
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/mkappworks-dev/cloudzilla-app/internal/view"
	"github.com/mkappworks-dev/cloudzilla-app/internal/view/pages"
)

// PageForgotPassword handles GET /auth/password/forgot.
func (h *Handler) PageForgotPassword(w http.ResponseWriter, r *http.Request) {
	h.render(w, r, pages.ForgotPassword(view.ForgotPasswordData{
		BasePage:  basePage(r, h.Services),
		Available: h.Services.PasswordReset.Available(),
	}))
}

// PageResetPassword handles GET /auth/password/reset/{token}. It only checks
// the link: mail scanners fetch links, and a GET that spent it would leave a dead one.
func (h *Handler) PageResetPassword(w http.ResponseWriter, r *http.Request) {
	token := chi.URLParam(r, "token")
	link, err := h.Services.PasswordReset.Check(r.Context(), token)
	if err != nil {
		slog.Error("check password reset link", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	h.renderResetPassword(w, r, view.ResetPasswordData{Link: link}, http.StatusOK)
}
