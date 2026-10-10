package handler

import (
	"context"
	"log/slog"
	"net/http"

	"github.com/mkappworks-dev/cloudzilla-app/internal/middleware"
	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/service"
	"github.com/mkappworks-dev/cloudzilla-app/internal/view"
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
	if prefs, err := services.User.LayoutPrefs(r.Context(), claims.UserID); err != nil {
		slog.Error("basePage: layout prefs lookup failed; rendering defaults",
			"error", err, "user_id", claims.UserID, "path", r.URL.Path)
	} else {
		page.CodeLight, page.CodeDark, page.AvatarKey = prefs.CodeLight, prefs.CodeDark, prefs.AvatarKey
	}
	memberships, err := services.Org.ListMembershipsForUser(r.Context(), claims.UserID)
	if err != nil {
		slog.Error("basePage: workspace switcher org list failed; degrading to personal-only",
			"error", err, "user_id", claims.UserID, "path", r.URL.Path)
	} else {
		entries := make([]view.OrgEntry, 0, len(memberships))
		for _, m := range memberships {
			entries = append(entries, view.OrgEntry{Org: m.Org, Role: m.Role})
		}
		page.UserOrgs = entries
	}
	return page
}

// readableRepo loads a repo for a signed-in viewer. A repo the viewer can't read
// gets the same 404 as a missing one, so callers must run it before any
// CanWrite/CanManage 403, or that 403 would confirm a private repo exists.
func (h *Handler) readableRepo(w http.ResponseWriter, r *http.Request, owner, repoName string, viewerID int64) (*model.Repository, bool) {
	repo, err := h.Services.Repo.Get(r.Context(), owner, repoName)
	if err != nil || !h.Services.Repo.CanRead(r.Context(), repo, &viewerID) {
		h.NotFound(w, r)
		return nil, false
	}
	return repo, true
}

// readableRepoPage is readableRepo for pages an anonymous viewer can open.
func (h *Handler) readableRepoPage(w http.ResponseWriter, r *http.Request, owner, repoName string) (*model.Repository, bool) {
	repo, err := h.Services.Repo.Get(r.Context(), owner, repoName)
	if err != nil || !h.Services.Repo.CanRead(r.Context(), repo, viewerOf(r)) {
		h.NotFound(w, r)
		return nil, false
	}
	return repo, true
}

// The repo-switcher list is best-effort: a failed lookup leaves it empty.
func (h *Handler) withRepoSubnav(ctx context.Context, base BasePage, repo *model.Repository, active string, canManage bool) BasePage {
	var viewerID *int64
	if base.CurrentUser != nil {
		viewerID = &base.CurrentUser.UserID
	}
	base.OwnerContext = repo.OwnerName
	base.RepoSubnav = &view.RepoSubnavInfo{
		OwnerName:        repo.OwnerName,
		RepoName:         repo.Name,
		Active:           active,
		CanManage:        canManage,
		Private:          repo.Private,
		AllowIssues:      repo.AllowIssues,
		AllowDiscussions: repo.AllowDiscussions,
		AllowProjects:    repo.AllowProjects,
		AllowWiki:        repo.AllowWiki,
		Counts:           h.repoSubnavCounts(ctx, repo, viewerID),
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

// Best-effort: a failed query drops that tab's count rather than failing the page.
func (h *Handler) repoSubnavCounts(ctx context.Context, repo *model.Repository, viewerID *int64) map[string]int {
	counts := map[string]int{}
	logFail := func(tab string, err error) {
		slog.Warn("repo subnav counts: tab query failed; hiding count",
			"tab", tab, "repo_id", repo.ID, "error", err)
	}
	if n, err := h.Services.Pull.CountOpen(ctx, repo.ID); err == nil {
		counts["pull_requests"] = n
	} else {
		logFail("pull_requests", err)
	}
	if repo.AllowIssues {
		if n, err := h.Services.Issue.CountOpen(ctx, repo.ID, viewerID); err == nil {
			counts["issues"] = n
		} else {
			logFail("issues", err)
		}
	}
	if repo.AllowDiscussions {
		if n, err := h.Services.Discussion.CountByRepo(ctx, repo.ID); err == nil {
			counts["discussions"] = n
		} else {
			logFail("discussions", err)
		}
	}
	if n, err := h.Services.Release.CountPublished(ctx, repo.ID); err == nil {
		counts["releases"] = n
	} else {
		logFail("releases", err)
	}
	return counts
}

func withAccountSubnav(base BasePage, active string, counts map[string]int) BasePage {
	base.AccountSubnav = &view.AccountSubnavInfo{Active: active, Counts: counts}
	return base
}

// Best-effort: any failed query degrades that badge to 0 rather than failing the page.
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
	if n, err := h.Services.Attention.CountForUser(ctx, userID); err == nil {
		counts["attention"] = n
	} else {
		logFail("attention", err)
	}
	return counts
}
