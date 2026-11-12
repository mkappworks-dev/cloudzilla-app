package handler

import (
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/mkappworks-dev/cloudzilla-app/internal/middleware"
	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/service"
)

const additionalEmailFormError = "#additional-email-error"

// AddUserEmail handles POST /settings/emails.
func (h *Handler) AddUserEmail(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.ClaimsFromContext(r.Context())
	if !ok {
		settingsError(w, r, http.StatusUnauthorized, "unauthorized")
		return
	}
	if err := r.ParseForm(); err != nil {
		settingsError(w, r, http.StatusBadRequest, "bad request")
		return
	}
	email := strings.TrimSpace(r.FormValue("email"))
	err := h.Services.EmailVerifier.AddAddress(r.Context(), claims.UserID, email, confirmationFrom(r))
	if err != nil {
		refuseSettingsForm(w, r, additionalEmailFormError, "email", userEmailErrorCode(claims.UserID, err))
		return
	}
	h.Services.AuditLog.Record(r.Context(), r, claims.UserID, claims.Username, model.AuditActionEmailAdd, model.AuditTargetUser, claims.UserID, claims.Username,
		map[string]any{"email": email})
	redirectAfterSave(w, r, "/settings#email")
}

// RemoveUserEmail handles POST /settings/emails/{id}/remove.
func (h *Handler) RemoveUserEmail(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.ClaimsFromContext(r.Context())
	if !ok {
		settingsError(w, r, http.StatusUnauthorized, "unauthorized")
		return
	}
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		settingsError(w, r, http.StatusNotFound, "not found")
		return
	}
	if err := r.ParseForm(); err != nil {
		settingsError(w, r, http.StatusBadRequest, "bad request")
		return
	}
	slot := "#additional-email-error-" + strconv.FormatInt(id, 10)
	if err := h.Services.EmailVerifier.RemoveAddress(r.Context(), claims.UserID, id, confirmationFrom(r)); err != nil {
		refuseSettingsForm(w, r, slot, "email", userEmailErrorCode(claims.UserID, err))
		return
	}
	h.Services.AuditLog.Record(r.Context(), r, claims.UserID, claims.Username, model.AuditActionEmailRemove, model.AuditTargetUser, claims.UserID, claims.Username,
		map[string]any{"id": id})
	redirectAfterSave(w, r, "/settings#email")
}

// userEmailErrorCode maps a failed add or remove to a SettingsErrorMessage code.
func userEmailErrorCode(userID int64, err error) string {
	if _, code, refused := reauthRefusal(userID, err); refused {
		return code
	}
	switch {
	case errors.Is(err, service.ErrInvalidEmail):
		return "invalid_email"
	case errors.Is(err, service.ErrEmailTaken):
		return "email_taken"
	case errors.Is(err, service.ErrTooManyEmails):
		return "email_limit"
	case errors.Is(err, service.ErrVerificationCooldown):
		return "verification_cooldown"
	case errors.Is(err, service.ErrEmailVerificationUnavailable):
		return "verification_unavailable"
	case errors.Is(err, service.ErrNoSuchAdditionalEmail):
		return "email_missing"
	}
	slog.Error("additional email", "user_id", userID, "error", err)
	return "update_failed"
}
