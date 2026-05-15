package handler

import (
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/go-chi/chi/v5"
	"github.com/mkappworks-dev/cloudzilla-app/internal/markdown"
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

	repo, err := h.Services.Repo.Get(r.Context(), owner, repoName)
	if err != nil {
		h.NotFound(w, r)
		return
	}

	allPulls, err := h.Services.Pull.List(r.Context(), owner, repoName)
	if err != nil || allPulls == nil {
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
	if err != nil || serviceRows == nil {
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
		BasePage:    withRepoSubnav(basePage(r, h.Services), owner, repoName, "pull_requests", canManage),
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

// PageNewPull renders the new pull request form with branch selection and diff preview.
func (h *Handler) PageNewPull(w http.ResponseWriter, r *http.Request) {
	owner := chi.URLParam(r, "owner")
	repoName := chi.URLParam(r, "repo")

	repo, err := h.Services.Repo.Get(r.Context(), owner, repoName)
	if err != nil {
		h.NotFound(w, r)
		return
	}

	templateBody, _ := h.Services.Code.GetPRTemplate(owner, repoName, repo.DefaultBranch)
	refs, _ := h.Services.Code.ListRefs(owner, repoName, repo.DefaultBranch)
	var branches []service.BranchInfo
	if refs != nil {
		branches = refs.Branches
	}

	canManage := false
	if claims, ok := middleware.ClaimsFromContext(r.Context()); ok {
		canManage = h.Services.Repo.CanManage(r.Context(), repo, claims.UserID)
	}
	h.render(w, r, pages.PullNew(view.PullNewData{
		BasePage:     withRepoSubnav(basePage(r, h.Services), owner, repoName, "pull_requests", canManage),
		Repo:         *repo,
		Owner:        owner,
		RepoName:     repoName,
		TemplateBody: templateBody,
		Branches:     branches,
	}))
}

// PageNewPullSubmit handles new PR form submission and redirects to the created PR.
func (h *Handler) PageNewPullSubmit(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.ClaimsFromContext(r.Context())
	if !ok {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}
	owner := chi.URLParam(r, "owner")
	repoName := chi.URLParam(r, "repo")

	repo, err := h.Services.Repo.Get(r.Context(), owner, repoName)
	if err != nil {
		h.NotFound(w, r)
		return
	}

	refs, _ := h.Services.Code.ListRefs(owner, repoName, repo.DefaultBranch)
	var branches []service.BranchInfo
	if refs != nil {
		branches = refs.Branches
	}

	if err := r.ParseForm(); err != nil {
		writeError(w, http.StatusBadRequest, "invalid form")
		return
	}
	title := r.FormValue("title")
	body := r.FormValue("body")
	headBranch := r.FormValue("head_branch")
	baseBranch := r.FormValue("base_branch")

	canManage := h.Services.Repo.CanManage(r.Context(), repo, claims.UserID)
	renderErr := func(msg string) {
		h.render(w, r, pages.PullNew(view.PullNewData{
			BasePage:     withRepoSubnav(basePage(r, h.Services), owner, repoName, "pull_requests", canManage),
			Repo:         *repo,
			Owner:        owner,
			RepoName:     repoName,
			TemplateBody: body,
			Branches:     branches,
			Error:        msg,
		}))
	}

	if title == "" {
		renderErr("Title is required")
		return
	}

	pr, err := h.Services.Pull.Create(r.Context(), owner, repoName, claims.UserID, title, body, headBranch, baseBranch, false)
	if err != nil {
		renderErr("Failed to create pull request: " + err.Error())
		return
	}

	go h.Services.Webhook.Dispatch(repo.ID, "pull_request", h.Services.Webhook.PullPayload("opened", *repo, *pr))

	http.Redirect(w, r, fmt.Sprintf("/%s/%s/pulls/%d", owner, repoName, pr.Number), http.StatusSeeOther)
}

// PagePullDetail renders the pull request detail page with diff, reviews, and merge controls.
func (h *Handler) PagePullDetail(w http.ResponseWriter, r *http.Request) {
	owner := chi.URLParam(r, "owner")
	repoName := chi.URLParam(r, "repo")
	number, err := strconv.Atoi(chi.URLParam(r, "number"))
	if err != nil {
		http.Error(w, "invalid pull number", http.StatusBadRequest)
		return
	}

	repo, err := h.Services.Repo.Get(r.Context(), owner, repoName)
	if err != nil {
		h.NotFound(w, r)
		return
	}

	pull, err := h.Services.Pull.Get(r.Context(), owner, repoName, number)
	if err != nil {
		h.NotFound(w, r)
		return
	}

	var diff *service.PRDiffResult
	if pull.State == model.PRStateOpen {
		if d, err := h.Services.Code.GetPullDiff(owner, repoName, pull.BaseBranch, pull.HeadBranch); err == nil {
			diff = d
		}
	}

	pullLabels2, _ := h.Services.Label.GetForPull(r.Context(), pull.ID)
	if pullLabels2 == nil {
		pullLabels2 = []model.Label{}
	}
	allLabels2, _ := h.Services.Label.ListByRepo(r.Context(), owner, repoName)
	if allLabels2 == nil {
		allLabels2 = []model.Label{}
	}
	pullAssignees, _ := h.Services.Assignee.GetForPull(r.Context(), pull.ID)
	if pullAssignees == nil {
		pullAssignees = []model.User{}
	}

	canWrite2 := false
	canManage2 := false
	if claims, ok := middleware.ClaimsFromContext(r.Context()); ok {
		canWrite2 = h.Services.Repo.CanWrite(r.Context(), repo, claims.UserID)
		canManage2 = h.Services.Repo.CanManage(r.Context(), repo, claims.UserID)
	}

	var headStatuses []model.CommitStatus
	if headCommit, _, err := h.Services.Code.ResolveRef(owner, repoName, pull.HeadBranch); err == nil {
		headStatuses, _ = h.Services.CommitStatus.List(r.Context(), owner, repoName, headCommit.Hash.String())
	}
	if headStatuses == nil {
		headStatuses = []model.CommitStatus{}
	}

	pullMilestone, _ := h.Services.Milestone.GetForPull(r.Context(), pull.ID)
	allPullDetailMilestones, _ := h.Services.Milestone.ListByRepo(r.Context(), owner, repoName)
	if allPullDetailMilestones == nil {
		allPullDetailMilestones = []model.Milestone{}
	}

	reviews, _ := h.Services.PullReview.ListByPull(r.Context(), owner, repoName, number)
	if reviews == nil {
		reviews = []model.PullReview{}
	}
	canMerge, mergeBlockReason, _ := h.Services.PullReview.CanMerge(r.Context(), pull.ID)

	rawLineComments, _ := h.Services.PullLineComment.ListByPull(r.Context(), owner, repoName, number)
	lineComments := map[string][]RenderedLineComment{}
	for _, c := range rawLineComments {
		key := fmt.Sprintf("%s:%d", c.Path, c.Line)
		lineComments[key] = append(lineComments[key], RenderedLineComment{
			PullLineComment: c,
			BodyHTML:        markdown.Render(c.Body),
		})
	}

	mergeabilityBox := components.MergeabilityBoxData{
		PatchURL: fmt.Sprintf("/api/repos/%s/%s/pulls/%d", owner, repoName, pull.Number),
	}
	mg, mgErr := h.Services.Code.Mergeability(r.Context(), owner, repoName, pull.BaseBranch, pull.HeadBranch)
	if mgErr != nil {
		slog.Warn("pull detail: mergeability failed; rendering as unavailable",
			"owner", owner, "repo", repoName, "pull_number", pull.Number, "error", mgErr)
		mergeabilityBox.Unavailable = true
	} else {
		mergeabilityBox.Ahead = mg.Ahead
		mergeabilityBox.Behind = mg.Behind
		mergeabilityBox.HasConflicts = mg.HasConflicts
		mergeabilityBox.UpToDate = !mg.HasConflicts && mg.Ahead == 0
		mergeabilityBox.Mergeable = !mg.HasConflicts && mg.Ahead > 0
		mergeabilityBox.CanFastForward = !mg.HasConflicts && mg.Ahead > 0 && mg.Behind == 0
		mergeabilityBox.CanThreeWayMerge = !mg.HasConflicts && mg.Ahead > 0
		mergeabilityBox.CanSquash = !mg.HasConflicts && mg.Ahead > 0
	}
	if requiredChecks, passingChecks, err := h.Services.CommitStatus.Counts(r.Context(), pull.ID); err != nil {
		slog.Warn("pull detail: commit status counts failed; hiding checks signal",
			"owner", owner, "repo", repoName, "pull_number", pull.Number, "error", err)
	} else {
		mergeabilityBox.RequiredChecks = requiredChecks
		mergeabilityBox.PassingChecks = passingChecks
	}
	if requiredReviews, approvedReviews, err := h.Services.PullReview.Counts(r.Context(), pull.ID); err != nil {
		slog.Warn("pull detail: review counts failed; hiding reviews signal",
			"owner", owner, "repo", repoName, "pull_number", pull.Number, "error", err)
	} else {
		mergeabilityBox.RequiredReviews = requiredReviews
		mergeabilityBox.ApprovedReviews = approvedReviews
	}

	h.render(w, r, pages.PullDetail(view.PullDetailData{
		BasePage:          withRepoSubnav(basePage(r, h.Services), owner, repoName, "pull_requests", canManage2),
		Repo:              *repo,
		Pull:              *pull,
		Owner:             owner,
		RepoName:          repoName,
		Diff:              diff,
		BodyHTML:          markdown.Render(pull.Body),
		Labels:            pullLabels2,
		Assignees:         pullAssignees,
		AllLabels:         allLabels2,
		Milestone:         pullMilestone,
		AllMilestones:     allPullDetailMilestones,
		CanWrite:          canWrite2,
		HeadStatuses:      headStatuses,
		Reviews:           reviews,
		CanMerge:          canMerge,
		MergeBlockReason:  mergeBlockReason,
		AutoMergeEnabled:  pull.AutoMergeEnabled,
		AutoMergeStrategy: pull.AutoMergeStrategy,
		LineComments:      lineComments,
		Mergeability:      mergeabilityBox,
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
