package handler

import (
	"log/slog"
	"net/http"

	"github.com/mkappworks-dev/cloudzilla-app/internal/middleware"
	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/service"
	"github.com/mkappworks-dev/cloudzilla-app/internal/view"
	"github.com/mkappworks-dev/cloudzilla-app/internal/view/pages"
)

func basePage(r *http.Request, services *service.Services) BasePage {
	allowLogin := services.SiteSetting.AllowLogin(r.Context())
	allowRegistration := services.SiteSetting.AllowRegistration(r.Context())
	claims, ok := middleware.ClaimsFromContext(r.Context())
	if !ok {
		return BasePage{AllowLogin: allowLogin, AllowRegistration: allowRegistration}
	}
	count, _ := services.Notification.CountUnread(r.Context(), claims.UserID)
	page := BasePage{CurrentUser: &claims, UnreadNotifCount: count, AllowLogin: allowLogin, AllowRegistration: allowRegistration}
	orgs, err := services.Org.ListOwnedByUser(r.Context(), claims.UserID)
	if err != nil {
		// Workspace switcher just falls back to showing "Personal" only.
		slog.Warn("listing user orgs for workspace switcher failed", "error", err, "user_id", claims.UserID)
	} else {
		page.UserOrgs = orgs
	}
	return page
}

// PageHome renders the home feed page.
func (h *Handler) PageHome(w http.ResponseWriter, r *http.Request) {
	if _, ok := middleware.ClaimsFromContext(r.Context()); ok {
		http.Redirect(w, r, "/feed", http.StatusSeeOther)
		return
	}
	repos, err := h.Services.Repo.List(r.Context())
	if err != nil {
		http.Error(w, "failed to list repos", http.StatusInternalServerError)
		return
	}
	if repos == nil {
		repos = []model.Repository{}
	}
	templates, _ := h.Services.Repo.ListTemplates(r.Context())
	if templates == nil {
		templates = []model.Repository{}
	}
	h.render(w, r, pages.Home(view.HomeData{BasePage: basePage(r, h.Services), Repos: repos, Templates: templates}))
}
