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

// Best-effort: each optional field degrades to zero value on failure rather than 500ing the whole layout.
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

// withRepoSubnav attaches the repo subnav so layout.Base renders it inside
// <header>. Use from any handler serving a repo-scoped route.
func withRepoSubnav(base BasePage, owner, repoName, active string, canManage bool) BasePage {
	base.RepoSubnav = &view.RepoSubnavInfo{
		OwnerName: owner,
		RepoName:  repoName,
		Active:    active,
		CanManage: canManage,
	}
	return base
}

func (h *Handler) PageHome(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	templates, _ := h.Services.Repo.ListTemplates(ctx)
	if templates == nil {
		templates = []model.Repository{}
	}

	repos := []model.Repository{}
	data := view.HomeData{
		BasePage:  basePage(r, h.Services),
		Repos:     repos,
		Templates: templates,
	}

	if claims, ok := middleware.ClaimsFromContext(ctx); ok {
		userID := claims.UserID
		viewerID := userID
		ownRepos, err := h.Services.Repo.ListByOwnerVisibleTo(ctx, claims.Username, &viewerID)
		if err != nil {
			slog.Warn("home: own-repo list failed", "user_id", userID, "error", err)
		} else {
			data.Repos = ownRepos
		}
		commitsLast7, err := h.Services.CommitStats.CommitsForUserSince(ctx, userID, 7)
		if err != nil {
			slog.Warn("home: commits-last-7 stat failed", "user_id", userID, "error", err)
		}
		countRepos, err := h.Services.Repo.CountForUser(ctx, userID)
		if err != nil {
			slog.Warn("home: repo count stat failed", "user_id", userID, "error", err)
		}
		countOpenPulls, err := h.Services.Pull.CountOpenAuthoredByOrAssignedTo(ctx, userID)
		if err != nil {
			slog.Warn("home: open-pulls count stat failed", "user_id", userID, "error", err)
		}
		countOpenIssues, err := h.Services.Issue.CountOpenAuthoredByOrAssignedTo(ctx, userID)
		if err != nil {
			slog.Warn("home: open-issues count stat failed", "user_id", userID, "error", err)
		}
		data.Stats = []components.StatItem{
			{Label: "Repositories", Value: countRepos},
			{Label: "Pull requests", Value: countOpenPulls, Subtitle: "open"},
			{Label: "Issues", Value: countOpenIssues, Subtitle: "open"},
			{Label: "Commits", Value: commitsLast7, Subtitle: "last 7 days"},
		}
		if heat, err := h.Services.CommitStats.LookbackForUser(ctx, userID, 365); err != nil {
			slog.Warn("home: heatmap lookback failed", "user_id", userID, "error", err)
		} else {
			data.Heatmap = heat
		}
		if att, err := h.Services.Attention.ForUser(ctx, userID); err != nil {
			slog.Warn("home: attention list failed", "user_id", userID, "error", err)
		} else {
			data.Attention = att
		}
		if feed, err := h.Services.Event.Feed(ctx, int(userID), 1, 10); err != nil {
			slog.Warn("home: activity feed failed", "user_id", userID, "error", err)
		} else {
			data.Activity = feed
		}
	}

	h.render(w, r, pages.Home(data))
}
