package handler

import (
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/mkappworks-dev/cloudzilla-app/internal/middleware"
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

	issuesOpened, _ := h.Services.Issue.CountCreatedSince(r.Context(), repo.ID, since)
	issuesClosed, _ := h.Services.Issue.CountClosedSince(r.Context(), repo.ID, since)
	pullsOpened, _ := h.Services.Pull.CountCreatedSince(r.Context(), repo.ID, since)
	pullsMerged, _ := h.Services.Pull.CountMergedSince(r.Context(), repo.ID, since)

	commitsLast30, _ := h.Services.CommitStats.WeeklyForRepo(r.Context(), repo.ID, 5)
	issuesLast30, _ := h.Services.Issue.WeeklyCreated(r.Context(), repo.ID, 5)
	pullsLast30, _ := h.Services.Pull.WeeklyCreated(r.Context(), repo.ID, 5)

	recentCommits := 0
	for _, n := range commitsLast30 {
		recentCommits += n
	}

	contributors, _ := h.Services.Code.GetContributors(owner, repoName)
	if len(contributors) > 10 {
		contributors = contributors[:10]
	}

	canManage := userID != nil && h.Services.Repo.CanManage(r.Context(), repo, *userID)

	h.render(w, r, pages.Pulse(view.PulseData{
		BasePage:      withRepoSubnav(basePage(r, h.Services), owner, repoName, "code", canManage),
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

	rows, _ := h.Services.ContributorStats.ForRepo(r.Context(), repo.ID)

	canManage := userID != nil && h.Services.Repo.CanManage(r.Context(), repo, *userID)

	h.render(w, r, pages.Contributors(view.ContributorsData{
		BasePage: withRepoSubnav(basePage(r, h.Services), owner, repoName, "code", canManage),
		Repo:     repo,
		Rows:     rows,
	}))
}
