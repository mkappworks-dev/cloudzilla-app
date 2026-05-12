package handler

import (
	"errors"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/mkappworks-dev/cloudzilla-app/internal/middleware"
	"github.com/mkappworks-dev/cloudzilla-app/internal/service"
	"github.com/mkappworks-dev/cloudzilla-app/internal/view"
	"github.com/mkappworks-dev/cloudzilla-app/internal/view/pages"
)

// PagePulse renders /{owner}/{repo}/pulse — 30-day activity summary.
func (h *Handler) PagePulse(w http.ResponseWriter, r *http.Request) {
	owner := chi.URLParam(r, "owner")
	repoName := chi.URLParam(r, "repo")

	var userID *int64
	if claims, ok := middleware.ClaimsFromContext(r.Context()); ok {
		userID = &claims.UserID
	}

	repo, err := h.Services.Repo.Get(r.Context(), owner, repoName)
	if err != nil {
		http.Error(w, "repo not found", http.StatusNotFound)
		return
	}
	if !h.Services.Repo.CanRead(r.Context(), repo, userID) {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}

	since := time.Now().Add(-30 * 24 * time.Hour)

	newIssues, _ := h.Services.Issue.CountCreatedSince(r.Context(), repo.ID, since)
	closedIssues, _ := h.Services.Issue.CountClosedSince(r.Context(), repo.ID, since)
	newPRs, _ := h.Services.Pull.CountCreatedSince(r.Context(), repo.ID, since)
	mergedPRs, _ := h.Services.Pull.CountMergedSince(r.Context(), repo.ID, since)
	openPRs, _ := h.Services.Pull.CountOpen(r.Context(), repo.ID)

	weeks, _ := h.Services.Code.GetCommitActivity(owner, repoName)
	recentCommits := 0
	// sum last 5 weeks (covers ~30 days plus rounding)
	start := 0
	if len(weeks) > 5 {
		start = len(weeks) - 5
	}
	for _, wa := range weeks[start:] {
		recentCommits += wa.Total
	}

	contributors, _ := h.Services.Code.GetContributors(owner, repoName)
	if len(contributors) > 10 {
		contributors = contributors[:10]
	}

	h.render(w, r, pages.Pulse(view.PulseData{
		BasePage:      basePage(r, h.Services),
		Repo:          *repo,
		Owner:         owner,
		RepoName:      repoName,
		NewIssues:     newIssues,
		ClosedIssues:  closedIssues,
		NewPRs:        newPRs,
		MergedPRs:     mergedPRs,
		OpenPRs:       openPRs,
		RecentCommits: recentCommits,
		Contributors:  contributors,
	}))
}

// PageContributors renders /{owner}/{repo}/graphs/contributors — full contributor table.
func (h *Handler) PageContributors(w http.ResponseWriter, r *http.Request) {
	owner := chi.URLParam(r, "owner")
	repoName := chi.URLParam(r, "repo")

	var userID *int64
	if claims, ok := middleware.ClaimsFromContext(r.Context()); ok {
		userID = &claims.UserID
	}

	repo, err := h.Services.Repo.Get(r.Context(), owner, repoName)
	if err != nil {
		http.Error(w, "repo not found", http.StatusNotFound)
		return
	}
	if !h.Services.Repo.CanRead(r.Context(), repo, userID) {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}

	contributors, err := h.Services.Code.GetContributors(owner, repoName)
	if err != nil {
		if errors.Is(err, service.ErrEmptyRepo) {
			contributors = []service.ContributorStat{}
		} else {
			http.Error(w, "failed to load contributors", http.StatusInternalServerError)
			return
		}
	}

	maxCommits := 0
	for _, c := range contributors {
		if c.Commits > maxCommits {
			maxCommits = c.Commits
		}
	}

	h.render(w, r, pages.Contributors(view.ContributorsData{
		BasePage:     basePage(r, h.Services),
		Repo:         *repo,
		Owner:        owner,
		RepoName:     repoName,
		Contributors: contributors,
		MaxCommits:   maxCommits,
	}))
}
