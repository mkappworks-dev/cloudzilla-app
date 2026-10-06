package handler

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/mkappworks-dev/cloudzilla-app/internal/concurrency"
	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/service"
	"github.com/mkappworks-dev/cloudzilla-app/internal/view"
	"github.com/mkappworks-dev/cloudzilla-app/internal/view/pages"
)

const passwordResetNotice = "Your password was changed and every session was signed out. Sign in with the new password."

// PageForgotPassword handles GET /auth/password/forgot.
func (h *Handler) PageForgotPassword(w http.ResponseWriter, r *http.Request) {
	h.render(w, r, pages.ForgotPassword(view.ForgotPasswordData{
		BasePage:  basePage(r, h.Services),
		Available: h.Services.PasswordReset.Available(),
	}))
}

// ForgotPasswordSubmit handles POST /auth/password/forgot. Like requestSignup,
// it answers every valid address alike and mails in the background, so neither
// the response nor its timing shows whether the address has an account.
func (h *Handler) ForgotPasswordSubmit(w http.ResponseWriter, r *http.Request) {
	data := view.ForgotPasswordData{BasePage: basePage(r, h.Services), Available: h.Services.PasswordReset.Available()}
	if !data.Available {
		h.render(w, r, pages.ForgotPassword(data))
		return
	}
	email := strings.TrimSpace(r.FormValue("email"))
	if !validEmail(email) {
		data.Email, data.Error = email, "Enter a valid email address"
		h.render(w, r, pages.ForgotPassword(data))
		return
	}
	concurrency.Go("password_reset.request", func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()
		if err := h.Services.PasswordReset.Request(ctx, email); err != nil {
			slog.Error("password reset: request failed", "error", err)
		}
	})
	data.Sent = true
	h.render(w, r, pages.ForgotPassword(data))
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
	h.renderResetPassword(w, r, view.ResetPasswordData{Link: link, Token: token}, http.StatusOK)
}

// ResetPasswordSubmit handles POST /auth/password/reset/{token}. It never signs
// anyone in: sign-in stays the one path that applies allow_login and TOTP.
func (h *Handler) ResetPasswordSubmit(w http.ResponseWriter, r *http.Request) {
	token := chi.URLParam(r, "token")
	link, err := h.Services.PasswordReset.Check(r.Context(), token)
	if err != nil {
		slog.Error("check password reset link", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	data := view.ResetPasswordData{Link: link, Token: token}
	if link.State != model.PasswordResetPending {
		h.renderResetPassword(w, r, data, http.StatusOK)
		return
	}

	password := r.FormValue("password")
	if password != r.FormValue("confirm") {
		data.Error = "The passwords don't match"
		h.renderResetPassword(w, r, data, http.StatusOK)
		return
	}
	failStatus := http.StatusOK
	u, issuedBy, err := h.Services.PasswordReset.Reset(r.Context(), token, password, r.FormValue("code"))
	switch {
	case err == nil:
	case errors.Is(err, service.ErrPasswordResetExpired):
		h.renderResetPassword(w, r, view.ResetPasswordData{Link: model.PasswordResetLink{State: model.PasswordResetExpired}}, http.StatusOK)
		return
	case errors.Is(err, service.ErrPasswordResetInvalid):
		h.renderResetPassword(w, r, view.ResetPasswordData{Link: model.PasswordResetLink{State: model.PasswordResetInvalid}}, http.StatusOK)
		return
	case errors.Is(err, service.ErrPasswordTooShort):
		data.Error = "Password must be at least 8 characters"
	case errors.Is(err, service.ErrPasswordTooLong):
		data.Error = fmt.Sprintf("Password is too long (maximum %d bytes)", service.MaxPasswordBytes)
	default:
		status, _, refused := reauthRefusal(link.UserID, err)
		if !refused {
			slog.Error("reset password", "user_id", link.UserID, "error", err)
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		data.Error = "The two-factor code is incorrect"
		if status == http.StatusTooManyRequests {
			data.Error = "Too many incorrect codes. Try again in 15 minutes."
		}
		failStatus = status
	}
	if data.Error != "" {
		h.renderResetPassword(w, r, data, failStatus)
		return
	}

	h.Services.AuditLog.Record(r.Context(), r, u.ID, u.Username, model.AuditActionPasswordReset, model.AuditTargetUser, u.ID, u.Username,
		map[string]any{"issued_by": issuedBy})
	http.Redirect(w, r, "/login?reset=done", http.StatusSeeOther)
}

// status applies only while the link is still pending.
func (h *Handler) renderResetPassword(w http.ResponseWriter, r *http.Request, data view.ResetPasswordData, status int) {
	// The token rides in the URL: keep it out of Referer headers and caches.
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("Cache-Control", "no-store")
	data.BasePage = basePage(r, h.Services)
	switch {
	case data.Link.State == model.PasswordResetExpired:
		w.WriteHeader(http.StatusGone)
	case data.Link.State == model.PasswordResetInvalid:
		w.WriteHeader(http.StatusBadRequest)
	case status != http.StatusOK:
		w.WriteHeader(status)
	}
	h.render(w, r, pages.ResetPassword(data))
}
