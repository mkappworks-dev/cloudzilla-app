package handler

import (
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/mkappworks/cloudzilla/internal/middleware"
	"github.com/mkappworks/cloudzilla/internal/model"
	"github.com/mkappworks/cloudzilla/internal/view"
	"github.com/mkappworks/cloudzilla/internal/view/fragments"
	"github.com/mkappworks/cloudzilla/internal/view/pages"
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
	}))
}

func (h *Handler) UpdateSiteSetting(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.ClaimsFromContext(r.Context())
	if !ok || !claims.IsSuperadmin {
		http.Error(w, "forbidden", http.StatusForbidden)
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
