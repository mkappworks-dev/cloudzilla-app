package handler

import (
	"log/slog"
	"net/http"

	"github.com/mkappworks-dev/cloudzilla-app/internal/middleware"
	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/view"
	"github.com/mkappworks-dev/cloudzilla-app/internal/view/pages"
)

// PageRepos renders the logged-in user's cross-repo repository list at /repos.
func (h *Handler) PageRepos(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	claims, ok := middleware.ClaimsFromContext(ctx)
	if !ok {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}
	filter := r.URL.Query().Get("filter")
	if filter != "owned" && filter != "collaborator" {
		filter = "all"
	}
	repos, err := h.Services.Repo.ListForUser(ctx, claims.UserID, filter)
	if err != nil {
		slog.Error("repos: failed to load repositories", "filter", filter, "error", err)
		http.Error(w, "Failed to load repositories", http.StatusInternalServerError)
		return
	}
	if repos == nil {
		repos = []model.Repository{}
	}
	data := view.AccountReposData{
		BasePage: withAccountSubnav(basePage(r, h.Services), "repositories", h.accountCounts(ctx, claims.UserID)),
		Repos:    repos,
		Filter:   filter,
	}
	h.render(w, r, pages.AccountRepos(data))
}

// PageAccountPulls renders the logged-in user's cross-repo pull-request list at /pulls.
func (h *Handler) PageAccountPulls(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	claims, ok := middleware.ClaimsFromContext(ctx)
	if !ok {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}
	filter := r.URL.Query().Get("filter")
	switch filter {
	case "assigned", "review_requested", "mentioned":
	default:
		filter = "created"
	}
	state := r.URL.Query().Get("state")
	if state != "closed" {
		state = "open"
	}
	pulls, err := h.Services.Pull.ListForUser(ctx, claims.UserID, filter, state)
	if err != nil {
		slog.Error("pulls: failed to load pull requests", "filter", filter, "state", state, "error", err)
		http.Error(w, "Failed to load pull requests", http.StatusInternalServerError)
		return
	}
	data := view.AccountPullsData{
		BasePage: withAccountSubnav(basePage(r, h.Services), "pulls", h.accountCounts(ctx, claims.UserID)),
		Pulls:    pulls,
		Filter:   filter,
		State:    state,
	}
	h.render(w, r, pages.AccountPulls(data))
}
