package handler

import (
	"errors"
	"log/slog"
	"net/http"

	"github.com/mkappworks-dev/cloudzilla-app/internal/middleware"
	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/service"
	"github.com/mkappworks-dev/cloudzilla-app/internal/view"
	"github.com/mkappworks-dev/cloudzilla-app/internal/view/pages"
)

// ResendVerificationEmail handles POST /settings/email/resend-verification.
func (h *Handler) ResendVerificationEmail(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.ClaimsFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	htmx := r.Header.Get("HX-Request") == "true"
	err := h.Services.EmailVerifier.Send(r.Context(), claims.UserID)
	if err == nil {
		if htmx {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		http.Redirect(w, r, "/settings#email", http.StatusSeeOther)
		return
	}

	status, code := http.StatusInternalServerError, "verification_failed"
	switch {
	case errors.Is(err, service.ErrVerificationCooldown):
		status, code = http.StatusTooManyRequests, "verification_cooldown"
	case errors.Is(err, service.ErrEmailAlreadyVerified):
		status, code = http.StatusConflict, "already_verified"
	case errors.Is(err, service.ErrEmailVerificationUnavailable):
		status, code = http.StatusServiceUnavailable, "verification_unavailable"
	default:
		slog.Error("resend verification email", "user_id", claims.UserID, "error", err)
	}
	if htmx {
		writeError(w, status, pages.SettingsErrorMessage(code))
		return
	}
	http.Redirect(w, r, "/settings?profile_error="+code+"#email", http.StatusSeeOther)
}

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

// VerifyEmailSubmit handles POST /verify-email.
func (h *Handler) VerifyEmailSubmit(w http.ResponseWriter, r *http.Request) {
	state, u, err := h.Services.EmailVerifier.Verify(r.Context(), r.FormValue("token"))
	if err != nil {
		slog.Error("verify email", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	if u != nil {
		h.Services.AuditLog.Record(r.Context(), r, u.ID, u.Username, model.AuditActionEmailVerify, model.AuditTargetUser, u.ID, u.Username,
			map[string]any{"email": u.Email, "method": "link"})
	}
	h.renderVerifyEmail(w, r, model.EmailVerificationLink{State: state}, "")
}

func (h *Handler) renderVerifyEmail(w http.ResponseWriter, r *http.Request, link model.EmailVerificationLink, token string) {
	// The token rides in the URL: keep it out of Referer headers and caches.
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("Cache-Control", "no-store")
	data := view.VerifyEmailData{BasePage: basePage(r, h.Services), Link: link}
	switch link.State {
	case model.EmailVerificationPending:
		data.Token = token
	case model.EmailVerificationExpired:
		w.WriteHeader(http.StatusGone)
	case model.EmailVerificationInvalid:
		w.WriteHeader(http.StatusBadRequest)
	}
	h.render(w, r, pages.VerifyEmail(data))
}
