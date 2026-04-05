package handler

import (
	"net/http"

	"github.com/mkappworks/cloudzilla/internal/middleware"
	"github.com/mkappworks/cloudzilla/internal/model"
	"github.com/mkappworks/cloudzilla/internal/service"
	"github.com/mkappworks/cloudzilla/internal/view"
	"github.com/mkappworks/cloudzilla/internal/view/pages"
)

func basePage(r *http.Request, services *service.Services) BasePage {
	allowLogin := services.SiteSetting.AllowLogin(r.Context())
	claims, ok := middleware.ClaimsFromContext(r.Context())
	if !ok {
		return BasePage{AllowLogin: allowLogin}
	}
	count, _ := services.Notification.CountUnread(r.Context(), claims.UserID)
	return BasePage{CurrentUser: &claims, UnreadNotifCount: count, AllowLogin: allowLogin}
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
