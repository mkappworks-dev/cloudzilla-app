package handler

import (
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/go-chi/chi/v5"
	"github.com/mkappworks-dev/cloudzilla-app/internal/middleware"
	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/service"
	"github.com/mkappworks-dev/cloudzilla-app/internal/view"
	"github.com/mkappworks-dev/cloudzilla-app/internal/view/components"
	"github.com/mkappworks-dev/cloudzilla-app/internal/view/pages"
)

// PagePulls renders the pull request list for a repository.
func (h *Handler) PagePulls(w http.ResponseWriter, r *http.Request) {
	owner := chi.URLParam(r, "owner")
	repoName := chi.URLParam(r, "repo")

	repo, ok := h.readableRepoPage(w, r, owner, repoName)
	if !ok {
		return
	}

	allPulls, err := h.Services.Pull.List(r.Context(), owner, repoName)
	if err != nil {
		slog.Error("pulls: list failed", "owner", owner, "repo", repoName, "error", err)
		http.Error(w, "failed to load pull requests", http.StatusInternalServerError)
		return
	}
	if allPulls == nil {
		allPulls = []model.PullRequest{}
	}

	var openCount, draftCount, mergedCount, closedCount int
	for _, p := range allPulls {
		switch {
		case p.State == model.PRStateOpen && !p.IsDraft:
			openCount++
		case p.State == model.PRStateOpen && p.IsDraft:
			draftCount++
		case p.State == model.PRStateMerged:
			mergedCount++
		case p.State == model.PRStateClosed:
			closedCount++
		}
	}

	stateFilter := r.URL.Query().Get("state")
	if stateFilter == "" {
		stateFilter = "open"
	}

	var prState model.PRState
	switch stateFilter {
	case "draft":
		prState = model.PRStateOpen
	case "merged":
		prState = model.PRStateMerged
	case "closed":
		prState = model.PRStateClosed
	default:
		prState = model.PRStateOpen
	}

	serviceRows, err := h.Services.Pull.ListWithCIStatus(r.Context(), owner, repoName, prState, 0, 0)
	if err != nil {
		slog.Error("pulls: list with CI status failed", "owner", owner, "repo", repoName, "error", err)
	}
	if serviceRows == nil {
		serviceRows = []service.PullListRow{}
	}

	rows := make([]components.PRListRowData, 0, len(serviceRows))
	for _, sr := range serviceRows {
		if stateFilter == "draft" && !sr.IsDraft {
			continue
		}
		if stateFilter == "open" && sr.IsDraft {
			continue
		}

		rowState := string(sr.State)
		if sr.IsDraft && sr.State == model.PRStateOpen {
			rowState = "draft"
		}

		labelChips := make([]components.LabelChip, 0, len(sr.LabelChips))
		for _, l := range sr.LabelChips {
			labelChips = append(labelChips, components.LabelChip{Name: l.Name, Color: l.Color})
		}

		reviewerAvatars := make([]string, 0, len(sr.Reviewers))
		for _, rv := range sr.Reviewers {
			if rv.AuthorName != "" {
				initials := pullInitials(rv.AuthorName)
				reviewerAvatars = append(reviewerAvatars, initials)
			}
		}

		rows = append(rows, components.PRListRowData{
			OwnerName:       owner,
			RepoName:        repoName,
			Number:          sr.Number,
			Title:           sr.Title,
			Author:          sr.AuthorName,
			State:           rowState,
			CIStatus:        sr.CIStatus,
			CIPassing:       sr.CIPassing,
			CITotal:         sr.CITotal,
			CommentCount:    sr.CommentCount,
			LabelChips:      labelChips,
			ReviewerAvatars: reviewerAvatars,
			OpenedAt:        pullFormatRelative(sr.CreatedAt),
		})
	}

	canManage := false
	if claims, ok := middleware.ClaimsFromContext(r.Context()); ok {
		canManage = h.Services.Repo.CanManage(r.Context(), repo, claims.UserID)
	}
	h.render(w, r, pages.Pulls(view.PullsData{
		BasePage:    h.withRepoSubnav(r.Context(), basePage(r, h.Services), repo, "pull_requests", canManage),
		Repo:        *repo,
		Owner:       owner,
		RepoName:    repoName,
		StateFilter: stateFilter,
		OpenCount:   openCount,
		DraftCount:  draftCount,
		MergedCount: mergedCount,
		ClosedCount: closedCount,
		Rows:        rows,
	}))
}

func pullInitials(name string) string {
	parts := strings.Fields(name)
	if len(parts) == 0 {
		return "?"
	}
	if len(parts) == 1 {
		r, size := utf8.DecodeRuneInString(parts[0])
		if size == 0 || r == utf8.RuneError {
			return "?"
		}
		return strings.ToUpper(string(r))
	}
	a, _ := utf8.DecodeRuneInString(parts[0])
	b, _ := utf8.DecodeRuneInString(parts[len(parts)-1])
	return strings.ToUpper(string(a) + string(b))
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}

func collaboratorUsernames(perms []model.Permission) []string {
	out := make([]string, 0, len(perms))
	for _, p := range perms {
		if p.Username != "" {
			out = append(out, p.Username)
		}
	}
	return out
}

func pullFormatRelative(t time.Time) string {
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		m := int(d / time.Minute)
		if m == 1 {
			return "1 minute ago"
		}
		return strconv.Itoa(m) + " minutes ago"
	case d < 24*time.Hour:
		h := int(d / time.Hour)
		if h == 1 {
			return "1 hour ago"
		}
		return strconv.Itoa(h) + " hours ago"
	case d < 7*24*time.Hour:
		days := int(d / (24 * time.Hour))
		if days == 1 {
			return "1 day ago"
		}
		return strconv.Itoa(days) + " days ago"
	default:
		return t.Format("Jan 2, 2006")
	}
}
