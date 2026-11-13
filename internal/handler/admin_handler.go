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
	"github.com/mkappworks-dev/cloudzilla-app/internal/view"
	"github.com/mkappworks-dev/cloudzilla-app/internal/view/fragments"
)

func (h *Handler) UpdateSiteSetting(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.ClaimsFromContext(r.Context())
	if !ok || !claims.IsSuperadmin {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}

	if !h.confirmAction(w, r, claims.UserID, confirmationFrom(r), "") {
		return
	}
	key := r.FormValue("key")
	value := r.FormValue("value")

	if err := h.Services.SiteSetting.Set(r.Context(), key, value); err != nil {
		http.Error(w, "failed to update setting", http.StatusInternalServerError)
		return
	}

	if r.Header.Get("HX-Request") == "true" {
		settings, _ := h.Services.SiteSetting.GetAll(r.Context())
		if settings == nil {
			settings = []model.SiteSetting{}
		}
		h.render(w, r, fragments.AdminSettings(view.AdminSettingsFragData{Settings: settings}))
		return
	}

	http.Redirect(w, r, "/admin/settings", http.StatusSeeOther)
}

// UpdateTerms saves the Markdown behind /terms; an empty body restores the default text.
func (h *Handler) UpdateTerms(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.ClaimsFromContext(r.Context())
	if !ok || !claims.IsSuperadmin {
		writeError(w, http.StatusForbidden, "forbidden")
		return
	}
	if !h.confirmAction(w, r, claims.UserID, confirmationFrom(r), "") {
		return
	}
	err := h.Services.SiteSetting.SetTerms(r.Context(), r.FormValue("terms"))
	if errors.Is(err, service.ErrTermsTooLong) {
		writeError(w, http.StatusBadRequest, "The terms are too long.")
		return
	}
	if err != nil {
		slog.Error("admin update terms", "error", err)
		writeError(w, http.StatusInternalServerError, "Couldn't save the terms.")
		return
	}
	if r.Header.Get("HX-Request") == "true" {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	http.Redirect(w, r, "/admin/settings", http.StatusSeeOther)
}

func (h *Handler) CreateInvitation(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.ClaimsFromContext(r.Context())
	if !ok || !claims.IsSuperadmin {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}

	email := r.FormValue("email")
	if email == "" {
		http.Error(w, "email is required", http.StatusBadRequest)
		return
	}
	if !h.confirmAction(w, r, claims.UserID, confirmationFrom(r), "") {
		return
	}

	_, err := h.Services.Invitation.Create(r.Context(), claims.UserID, email)
	if err != nil {
		http.Error(w, "failed to create invitation", http.StatusInternalServerError)
		return
	}

	if r.Header.Get("HX-Request") == "true" {
		invitations, _ := h.Services.Invitation.List(r.Context())
		if invitations == nil {
			invitations = []model.Invitation{}
		}
		h.render(w, r, fragments.AdminInvitations(view.AdminInvitationsFragData{Invitations: invitations}))
		return
	}

	http.Redirect(w, r, "/admin/settings", http.StatusSeeOther)
}

// AdminVerifyEmail handles POST /api/admin/users/verify-email: a superadmin
// vouches for a user's address when no SMTP server can send the link.
func (h *Handler) AdminVerifyEmail(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.ClaimsFromContext(r.Context())
	if !ok || !claims.IsSuperadmin {
		writeError(w, http.StatusForbidden, "forbidden")
		return
	}
	username := strings.TrimSpace(r.FormValue("username"))
	email := strings.TrimSpace(r.FormValue("email"))
	if username == "" || email == "" {
		writeError(w, http.StatusBadRequest, "Username and email are required.")
		return
	}
	if !h.confirmAction(w, r, claims.UserID, confirmationFrom(r), "") {
		return
	}
	u, err := h.Services.EmailVerifier.MarkVerified(r.Context(), username, email)
	if errors.Is(err, service.ErrNoSuchUserEmail) {
		writeError(w, http.StatusNotFound, "No user has that username and email.")
		return
	}
	if err != nil {
		slog.Error("admin verify email", "username", username, "error", err)
		writeError(w, http.StatusInternalServerError, "Couldn't mark the email verified.")
		return
	}
	h.Services.AuditLog.Record(r.Context(), r, claims.UserID, claims.Username, model.AuditActionEmailVerify, model.AuditTargetUser, u.ID, u.Username,
		map[string]any{"email": email, "method": "admin"})
	if r.Header.Get("HX-Request") == "true" {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	http.Redirect(w, r, "/admin/settings", http.StatusSeeOther)
}

func (h *Handler) DeleteInvitation(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.ClaimsFromContext(r.Context())
	if !ok || !claims.IsSuperadmin {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}

	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return
	}

	if err := h.Services.Invitation.Delete(r.Context(), id); err != nil {
		http.Error(w, "failed to delete invitation", http.StatusInternalServerError)
		return
	}

	if r.Header.Get("HX-Request") == "true" {
		invitations, _ := h.Services.Invitation.List(r.Context())
		if invitations == nil {
			invitations = []model.Invitation{}
		}
		h.render(w, r, fragments.AdminInvitations(view.AdminInvitationsFragData{Invitations: invitations}))
		return
	}

	http.Redirect(w, r, "/admin/settings", http.StatusSeeOther)
}

// RotateInstanceSigningKey handles POST /api/admin/signing-key/rotate.
func (h *Handler) RotateInstanceSigningKey(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.ClaimsFromContext(r.Context())
	if !ok || !claims.IsSuperadmin {
		writeError(w, http.StatusForbidden, "forbidden")
		return
	}
	if !h.confirmAction(w, r, claims.UserID, confirmationFrom(r), "") {
		return
	}
	key, err := h.Services.InstanceSigner.Rotate(r.Context())
	if errors.Is(err, service.ErrNoSecretKey) {
		writeError(w, http.StatusConflict, "Set security.secret_key before generating a signing key.")
		return
	}
	if err != nil {
		slog.Error("admin rotate instance signing key", "error", err)
		writeError(w, http.StatusInternalServerError, "Couldn't rotate the signing key.")
		return
	}
	// Synchronous: a rotation with no trace would be worse than a slow response.
	if err := h.Services.AuditLog.RecordNow(r.Context(), r, claims.UserID, claims.Username, model.AuditActionAdminSigningKeyRotate,
		model.AuditTargetInstanceSigningKey, key.ID, key.Fingerprint, nil); err != nil {
		slog.Error("audit instance signing key rotation", "error", err)
	}
	if r.Header.Get("HX-Request") == "true" {
		// A swap would replace the form the identity dialog is waiting on and leave the dialog open.
		w.Header().Set("HX-Refresh", "true")
		w.WriteHeader(http.StatusNoContent)
		return
	}
	http.Redirect(w, r, "/admin/settings#commit-signing", http.StatusSeeOther)
}
