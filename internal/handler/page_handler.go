package handler

import (
	"context"
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

// withRepoSubnav attaches the repo subnav and the topbar repo-switcher list.
// The switcher is best-effort: a failed lookup leaves it empty.
func (h *Handler) withRepoSubnav(ctx context.Context, base BasePage, repo *model.Repository, active string, canManage bool) BasePage {
	base.RepoSubnav = &view.RepoSubnavInfo{
		OwnerName:        repo.OwnerName,
		RepoName:         repo.Name,
		Active:           active,
		CanManage:        canManage,
		AllowIssues:      repo.AllowIssues,
		AllowDiscussions: repo.AllowDiscussions,
		AllowProjects:    repo.AllowProjects,
		AllowWiki:        repo.AllowWiki,
	}
	var viewerID *int64
	if base.CurrentUser != nil {
		viewerID = &base.CurrentUser.UserID
	}
	if siblings, err := h.Services.Repo.ListByOwnerVisibleTo(ctx, repo.OwnerName, viewerID); err == nil {
		refs := make([]view.RepoRef, 0, len(siblings))
		for _, s := range siblings {
			refs = append(refs, view.RepoRef{Name: s.Name, Path: "/" + s.OwnerName + "/" + s.Name})
		}
		base.RepoSwitcher = refs
	} else {
		slog.Error("withRepoSubnav: repo switcher list failed", "owner", repo.OwnerName, "error", err)
	}
	return base
}

// withAccountSubnav attaches the account-level Primary nav. counts is keyed by
// tab ("repositories"/"gists"/"pulls"/"issues"); pass nil to omit all badges.
func withAccountSubnav(base BasePage, active string, counts map[string]int) BasePage {
	base.AccountSubnav = &view.AccountSubnavInfo{Active: active, Counts: counts}
	return base
}

// accountCounts loads the nav badge counts for a logged-in user. Best-effort:
// any failed query degrades that badge to 0 rather than failing the page.
func (h *Handler) accountCounts(ctx context.Context, userID int64) map[string]int {
	counts := map[string]int{}
	logFail := func(badge string, err error) {
		slog.Warn("account counts: badge query failed; showing 0",
			"badge", badge, "user_id", userID, "error", err)
	}
	if n, err := h.Services.Repo.CountForUser(ctx, userID); err == nil {
		counts["repositories"] = n
	} else {
		logFail("repositories", err)
	}
	if n, err := h.Services.Gist.CountByUser(ctx, userID); err == nil {
		counts["gists"] = n
	} else {
		logFail("gists", err)
	}
	if n, err := h.Services.Pull.CountOpenAssignedTo(ctx, userID); err == nil {
		counts["pulls"] = n
	} else {
		logFail("pulls", err)
	}
	if n, err := h.Services.Issue.CountOpenAssignedTo(ctx, userID); err == nil {
		counts["issues"] = n
	} else {
		logFail("issues", err)
	}
	return counts
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
			slog.Error("home: own-repo list failed", "user_id", userID, "error", err)
			data.LoadWarnings = append(data.LoadWarnings,
				"We couldn't load your repositories right now. Refresh to try again.")
		} else {
			data.Repos = ownRepos
		}
	} else {
		publicRepos, err := h.Services.Repo.List(ctx)
		if err != nil {
			slog.Warn("home: anonymous public-repo list failed", "error", err)
		} else {
			data.Repos = publicRepos
		}
	}
	if claims, ok := middleware.ClaimsFromContext(ctx); ok {
		userID := claims.UserID
		commitsLast7, err := h.Services.CommitStats.CommitsForUserSince(ctx, userID, 7)
		if err != nil {
			slog.Warn("home: commits-last-7 stat failed", "user_id", userID, "error", err)
		}
		countRepos, err := h.Services.Repo.CountForUser(ctx, userID)
		if err != nil {
			slog.Warn("home: repo count stat failed", "user_id", userID, "error", err)
		}
		countOpenPulls, err := h.Services.Pull.CountOpenAssignedTo(ctx, userID)
		if err != nil {
			slog.Warn("home: open-pulls count stat failed", "user_id", userID, "error", err)
		}
		countOpenIssues, err := h.Services.Issue.CountOpenAssignedTo(ctx, userID)
		if err != nil {
			slog.Warn("home: open-issues count stat failed", "user_id", userID, "error", err)
		}
		data.Stats = []components.StatItem{
			{Label: "Repositories", Value: countRepos},
			{Label: "Pull requests", Value: countOpenPulls, Subtitle: "open"},
			{Label: "Issues", Value: countOpenIssues, Subtitle: "open"},
			{Label: "Commits", Value: commitsLast7, Subtitle: "last 7 days"},
		}
		data.BasePage = withAccountSubnav(data.BasePage, "overview", h.accountCounts(ctx, userID))
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
