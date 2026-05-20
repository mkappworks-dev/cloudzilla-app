package handler

import (
	"log/slog"
	"net/http"
	"sort"

	"github.com/mkappworks-dev/cloudzilla-app/internal/middleware"
	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/view"
	"github.com/mkappworks-dev/cloudzilla-app/internal/view/pages"
)

func (h *Handler) PageAccountRepos(w http.ResponseWriter, r *http.Request) {
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

func (h *Handler) PageAccountIssues(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	claims, ok := middleware.ClaimsFromContext(ctx)
	if !ok {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}
	filter := r.URL.Query().Get("filter")
	switch filter {
	case "created", "mentioned":
	default:
		filter = "assigned"
	}
	state := r.URL.Query().Get("state")
	if state != "closed" {
		state = "open"
	}
	issues, err := h.Services.Issue.ListForUser(ctx, claims.UserID, filter, state)
	if err != nil {
		slog.Error("issues: failed to load issues", "filter", filter, "state", state, "error", err)
		http.Error(w, "Failed to load issues", http.StatusInternalServerError)
		return
	}
	data := view.AccountIssuesData{
		BasePage: withAccountSubnav(basePage(r, h.Services), "issues", h.accountCounts(ctx, claims.UserID)),
		Issues:   issues,
		Filter:   filter,
		State:    state,
	}
	h.render(w, r, pages.AccountIssues(data))
}

func (h *Handler) PageAccountStars(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	claims, ok := middleware.ClaimsFromContext(ctx)
	if !ok {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}
	stars, err := h.Services.Star.ListByUser(ctx, claims.Username)
	if err != nil {
		slog.Error("stars: failed to load starred repositories", "username", claims.Username, "error", err)
		http.Error(w, "Failed to load starred repositories", http.StatusInternalServerError)
		return
	}
	if stars == nil {
		stars = []model.Repository{}
	}

	seen := map[string]struct{}{}
	var languages []string
	for _, repo := range stars {
		if repo.PrimaryLanguage != nil && *repo.PrimaryLanguage != "" {
			lang := *repo.PrimaryLanguage
			if _, ok := seen[lang]; !ok {
				seen[lang] = struct{}{}
				languages = append(languages, lang)
			}
		}
	}
	sort.Strings(languages)

	langFilter := r.URL.Query().Get("language")
	if langFilter != "" {
		filtered := make([]model.Repository, 0, len(stars))
		for _, repo := range stars {
			if repo.PrimaryLanguage != nil && *repo.PrimaryLanguage == langFilter {
				filtered = append(filtered, repo)
			}
		}
		stars = filtered
	}

	data := view.AccountStarsData{
		BasePage:  basePage(r, h.Services),
		Username:  claims.Username,
		Stars:     stars,
		Language:  langFilter,
		Languages: languages,
	}
	h.render(w, r, pages.AccountStars(data))
}
