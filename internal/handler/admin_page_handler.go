package handler

import (
	"net/http"

	"github.com/mkappworks-dev/cloudzilla-app/internal/middleware"
	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/view"
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
