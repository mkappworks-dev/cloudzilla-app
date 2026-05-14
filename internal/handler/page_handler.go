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

// basePage assembles the shared chrome data for every authenticated page.
// It is best-effort: each optional field (UnreadNotifCount, UserOrgs) degrades
// to its zero value on backend failure so a single sub-service outage cannot
// take the whole layout down. Failures are logged at Error level for observability.
//
// TODO(tech-debt, phase-1+): callers cannot distinguish "true zero" from
// "degraded due to error". If a third optional field is added, change the
// signature to (BasePage, error) — or add BasePage.DegradedReasons []string
// surfaced via the layout — before that field lands.
func basePage(r *http.Request, services *service.Services) BasePage {
	allowLogin := services.SiteSetting.AllowLogin(r.Context())
	allowRegistration := services.SiteSetting.AllowRegistration(r.Context())
	claims, ok := middleware.ClaimsFromContext(r.Context())
	if !ok {
		return BasePage{AllowLogin: allowLogin, AllowRegistration: allowRegistration}
	}
	count, err := services.Notification.CountUnread(r.Context(), claims.UserID)
	if err != nil {
		slog.Error("basePage: unread notification count failed; rendering 0",
			"error", err, "user_id", claims.UserID, "path", r.URL.Path)
		count = 0
	}
	page := BasePage{CurrentUser: &claims, UnreadNotifCount: count, AllowLogin: allowLogin, AllowRegistration: allowRegistration}
	orgs, err := services.Org.ListOwnedByUser(r.Context(), claims.UserID)
	if err != nil {
		slog.Error("basePage: workspace switcher org list failed; degrading to personal-only",
			"error", err, "user_id", claims.UserID, "path", r.URL.Path)
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
