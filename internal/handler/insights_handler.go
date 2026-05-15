package handler

import (
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/mkappworks-dev/cloudzilla-app/internal/middleware"
	"github.com/mkappworks-dev/cloudzilla-app/internal/view"
	"github.com/mkappworks-dev/cloudzilla-app/internal/view/pages"
)

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

	issuesOpened, err := h.Services.Issue.CountCreatedSince(r.Context(), repo.ID, since)
	if err != nil {
		slog.Warn("pulse: count issues created failed", "repo_id", repo.ID, "error", err)
	}
	issuesClosed, err := h.Services.Issue.CountClosedSince(r.Context(), repo.ID, since)
	if err != nil {
		slog.Warn("pulse: count issues closed failed", "repo_id", repo.ID, "error", err)
	}
	pullsOpened, err := h.Services.Pull.CountCreatedSince(r.Context(), repo.ID, since)
	if err != nil {
		slog.Warn("pulse: count pulls created failed", "repo_id", repo.ID, "error", err)
	}
	pullsMerged, err := h.Services.Pull.CountMergedSince(r.Context(), repo.ID, since)
	if err != nil {
		slog.Warn("pulse: count pulls merged failed", "repo_id", repo.ID, "error", err)
	}

	commitsLast30, err := h.Services.CommitStats.WeeklyForRepo(r.Context(), repo.ID, 5)
	if err != nil {
		slog.Warn("pulse: weekly commits failed", "repo_id", repo.ID, "error", err)
	}
	issuesLast30, err := h.Services.Issue.WeeklyCreated(r.Context(), repo.ID, 5)
	if err != nil {
		slog.Warn("pulse: weekly issues failed", "repo_id", repo.ID, "error", err)
	}
	pullsLast30, err := h.Services.Pull.WeeklyCreated(r.Context(), repo.ID, 5)
	if err != nil {
		slog.Warn("pulse: weekly pulls failed", "repo_id", repo.ID, "error", err)
	}

	recentCommits := 0
	for _, n := range commitsLast30 {
		recentCommits += n
	}

	contributors, err := h.Services.ContributorStats.ForRepo(r.Context(), repo.ID)
	if err != nil {
		slog.Warn("pulse: contributor stats failed", "repo_id", repo.ID, "error", err)
	}
	if len(contributors) > 10 {
		contributors = contributors[:10]
	}

	canManage := userID != nil && h.Services.Repo.CanManage(r.Context(), repo, *userID)

	h.render(w, r, pages.Pulse(view.PulseData{
		BasePage:      withRepoSubnav(basePage(r, h.Services), repo, "code", canManage),
		Repo:          *repo,
		Owner:         owner,
		RepoName:      repoName,
		IssuesOpened:  issuesOpened,
		IssuesClosed:  issuesClosed,
		PullsOpened:   pullsOpened,
		PullsMerged:   pullsMerged,
		RecentCommits: recentCommits,
		CommitsLast30: commitsLast30,
		IssuesLast30:  issuesLast30,
		PullsLast30:   pullsLast30,
		Contributors:  contributors,
	}))
}

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

	rows, err := h.Services.ContributorStats.ForRepo(r.Context(), repo.ID)
	if err != nil {
		slog.Error("contributors: load failed", "repo_id", repo.ID, "error", err)
		http.Error(w, "failed to load contributors", http.StatusInternalServerError)
		return
	}

	canManage := userID != nil && h.Services.Repo.CanManage(r.Context(), repo, *userID)

	h.render(w, r, pages.Contributors(view.ContributorsData{
		BasePage: withRepoSubnav(basePage(r, h.Services), repo, "code", canManage),
		Repo:     repo,
		Rows:     rows,
	}))
}
