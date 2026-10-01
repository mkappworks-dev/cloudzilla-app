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
	"github.com/mkappworks-dev/cloudzilla-app/internal/view/pages"
)

func (h *Handler) PageAdminSettings(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.ClaimsFromContext(r.Context())
	if !ok || !claims.IsSuperadmin {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}

	settings, _ := h.Services.SiteSetting.GetAll(r.Context())
	if settings == nil {
		settings = []model.SiteSetting{}
	}

	invitations, _ := h.Services.Invitation.List(r.Context())
	if invitations == nil {
		invitations = []model.Invitation{}
	}

	h.render(w, r, pages.AdminSettings(view.AdminSettingsData{
		BasePage:    basePage(r, h.Services),
		Settings:    settings,
		Invitations: invitations,
		Confirm:     h.confirmFactors(r, claims.UserID),
	}))
}

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
