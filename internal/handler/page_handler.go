package handler

import (
	"log/slog"
	"net/http"

	"github.com/mkappworks-dev/cloudzilla-app/internal/middleware"
	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/service"
	"github.com/mkappworks-dev/cloudzilla-app/internal/view"
	"github.com/mkappworks-dev/cloudzilla-app/internal/view/components"
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

// PageHome renders the home dashboard page.
//
// Phase 1 UI overhaul: the dashboard sections (stat strip, contribution
// heatmap, attention list, activity feed) are populated for signed-in
// users. Each optional fetch is best-effort — a single sub-service
// outage degrades that section's data to its zero value rather than
// failing the whole page. Signed-out viewers see the static repo list
// only.
func (h *Handler) PageHome(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	repos, err := h.Services.Repo.List(ctx)
	if err != nil {
		http.Error(w, "failed to list repos", http.StatusInternalServerError)
		return
	}
	if repos == nil {
		repos = []model.Repository{}
	}
	templates, _ := h.Services.Repo.ListTemplates(ctx)
	if templates == nil {
		templates = []model.Repository{}
	}

	data := view.HomeData{
		BasePage:  basePage(r, h.Services),
		Repos:     repos,
		Templates: templates,
	}

	if claims, ok := middleware.ClaimsFromContext(ctx); ok {
		userID := claims.UserID
		commitsLast7, _ := h.Services.CommitStats.CommitsForUserSince(ctx, userID, 7)
		countRepos, _ := h.Services.Repo.CountForUser(ctx, userID)
		countOpenPulls, _ := h.Services.Pull.CountOpenAuthoredByOrAssignedTo(ctx, userID)
		countOpenIssues, _ := h.Services.Issue.CountOpenAuthoredByOrAssignedTo(ctx, userID)
		data.Stats = []components.StatItem{
			{Label: "Repositories", Value: countRepos},
			{Label: "Pull requests", Value: countOpenPulls, Subtitle: "open"},
			{Label: "Issues", Value: countOpenIssues, Subtitle: "open"},
			{Label: "Commits", Value: commitsLast7, Subtitle: "last 7 days"},
		}
		if heat, err := h.Services.CommitStats.LookbackForUser(ctx, userID, 365); err == nil {
			data.Heatmap = heat
		}
		if att, err := h.Services.Attention.ForUser(ctx, userID); err == nil {
			data.Attention = att
		}
		if feed, err := h.Services.Event.Feed(ctx, int(userID), 1, 10); err == nil {
			data.Activity = feed
		}
	}

	h.render(w, r, pages.Home(data))
}
