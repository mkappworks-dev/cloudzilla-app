package handler

import (
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/go-chi/chi/v5"
	"github.com/mkappworks-dev/cloudzilla-app/internal/listquery"
	"github.com/mkappworks-dev/cloudzilla-app/internal/middleware"
	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/view"
	"github.com/mkappworks-dev/cloudzilla-app/internal/view/components"
	"github.com/mkappworks-dev/cloudzilla-app/internal/view/pages"
)

// PagePulls renders the filtered, paginated pull request list for a repository.
func (h *Handler) PagePulls(w http.ResponseWriter, r *http.Request) {
	if redirectListForm(w, r) {
		return
	}
	owner := chi.URLParam(r, "owner")
	repoName := chi.URLParam(r, "repo")

	repo, ok := h.readableRepoPage(w, r, owner, repoName)
	if !ok {
		return
	}

	var callerID *int64
	canManage := false
	if claims, ok := middleware.ClaimsFromContext(r.Context()); ok {
		callerID = &claims.UserID
		canManage = h.Services.Repo.CanManage(r.Context(), repo, claims.UserID)
	}

	allMilestones, err := h.Services.Milestone.ListByRepo(r.Context(), owner, repoName, callerID)
	if err != nil {
		slog.Warn("pulls: milestone list failed", "owner", owner, "repo", repoName, "error", err)
	}
	allLabels, err := h.Services.Label.ListByRepo(r.Context(), owner, repoName)
	if err != nil {
		slog.Warn("pulls: label list failed", "owner", owner, "repo", repoName, "error", err)
	}
	people, err := h.Services.Issue.Participants(r.Context(), owner, repoName, callerID)
	if err != nil {
		slog.Warn("pulls: participants failed", "owner", owner, "repo", repoName, "error", err)
	}

	q := listMilestoneTitle(listquery.FromValues(r.URL.Query()), allMilestones)
	stateFilter := q.State
	if stateFilter == "" {
		stateFilter = "open"
	}
	if q.State == "open" {
		q = q.Without(listquery.KeyIs, "")
	}

	page := listPage(r)
	serviceRows, total, err := h.Services.Pull.ListWithCIStatus(r.Context(), owner, repoName, listStoreFilter(listViewer(r), q, stateFilter, page))
	if err == nil && len(serviceRows) == 0 && total > 0 && page > 1 {
		page = listTotalPages(total)
		serviceRows, total, err = h.Services.Pull.ListWithCIStatus(r.Context(), owner, repoName, listStoreFilter(listViewer(r), q, stateFilter, page))
	}
	if err != nil {
		slog.Error("pulls: list failed", "owner", owner, "repo", repoName, "error", err)
		http.Error(w, "failed to load pull requests", http.StatusInternalServerError)
		return
	}
	counts, err := h.Services.Pull.CountByState(r.Context(), owner, repoName)
	if err != nil {
		slog.Warn("pulls: tab counts failed; showing 0", "owner", owner, "repo", repoName, "error", err)
	}

	rows := make([]components.PRListRowData, 0, len(serviceRows))
	for _, sr := range serviceRows {
		rowState := string(sr.State)
		if sr.IsDraft && sr.State == model.PRStateOpen {
			rowState = "draft"
		}

		labelChips := make([]components.LabelChipData, 0, len(sr.LabelChips))
		for _, l := range sr.LabelChips {
			labelChips = append(labelChips, components.LabelChipData{Name: l.Name, Color: l.Color})
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

	base := "/" + owner + "/" + repoName + "/pulls"
	totalPages := listTotalPages(total)
	var prevURL, nextURL string
	if page > 1 {
		prevURL = listURL(base, q, page-1)
	}
	if page < totalPages {
		nextURL = listURL(base, q, page+1)
	}
	_, signedIn := middleware.ClaimsFromContext(r.Context())
	tab := func(state string) string {
		if state == "open" {
			return listURL(base, q.Without(listquery.KeyIs, ""), 1)
		}
		return listURL(base, q.With(listquery.KeyIs, state), 1)
	}

	h.render(w, r, pages.Pulls(view.PullsData{
		BasePage:    h.withRepoSubnav(r.Context(), basePage(r, h.Services), repo, "pull_requests", canManage),
		Repo:        *repo,
		Owner:       owner,
		RepoName:    repoName,
		StateFilter: stateFilter,
		OpenURL:     tab("open"),
		DraftURL:    tab("draft"),
		MergedURL:   tab("merged"),
		ClosedURL:   tab("closed"),
		Filter:      listFilterData(base, "pull request", q, total, people, signedIn, allLabels, allMilestones),
		ClearURL:    listURL(base, listquery.Query{State: q.State}, 1),
		Page:        page,
		TotalPages:  totalPages,
		PrevURL:     prevURL,
		NextURL:     nextURL,
		OpenCount:   counts.Open,
		DraftCount:  counts.Draft,
		MergedCount: counts.Merged,
		ClosedCount: counts.Closed,
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
