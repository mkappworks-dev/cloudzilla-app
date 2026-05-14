package handler

import (
	"fmt"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/mkappworks-dev/cloudzilla-app/internal/markdown"
	"github.com/mkappworks-dev/cloudzilla-app/internal/middleware"
	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/service"
	"github.com/mkappworks-dev/cloudzilla-app/internal/view"
	"github.com/mkappworks-dev/cloudzilla-app/internal/view/components"
	"github.com/mkappworks-dev/cloudzilla-app/internal/view/pages"
)

// PagePulls renders the paginated pull request list for a repository.
func (h *Handler) PagePulls(w http.ResponseWriter, r *http.Request) {
	owner := chi.URLParam(r, "owner")
	repoName := chi.URLParam(r, "repo")

	repo, err := h.Services.Repo.Get(r.Context(), owner, repoName)
	if err != nil {
		h.NotFound(w, r)
		return
	}

	allPulls, err := h.Services.Pull.List(r.Context(), owner, repoName)
	if err != nil {
		allPulls = []model.PullRequest{}
	}
	if allPulls == nil {
		allPulls = []model.PullRequest{}
	}

	stateFilter := r.URL.Query().Get("state")
	if stateFilter == "" {
		stateFilter = "open"
	}
	var pulls []model.PullRequest
	for _, p := range allPulls {
		switch stateFilter {
		case "draft":
			if p.IsDraft && p.State == model.PRStateOpen {
				pulls = append(pulls, p)
			}
		case "closed":
			if p.State == model.PRStateClosed {
				pulls = append(pulls, p)
			}
		case "merged":
			if p.State == model.PRStateMerged {
				pulls = append(pulls, p)
			}
		default: // "open"
			if p.State == model.PRStateOpen && !p.IsDraft {
				pulls = append(pulls, p)
			}
		}
	}
	if pulls == nil {
		pulls = []model.PullRequest{}
	}

	pullLabels, _ := h.Services.Label.BatchForPulls(r.Context(), pulls)
	if pullLabels == nil {
		pullLabels = map[int64][]model.Label{}
	}

	allPullMilestones, _ := h.Services.Milestone.ListByRepo(r.Context(), owner, repoName)
	if allPullMilestones == nil {
		allPullMilestones = []model.Milestone{}
	}

	h.render(w, r, pages.Pulls(view.PullsData{
		BasePage:      basePage(r, h.Services),
		Repo:          *repo,
		Pulls:         pulls,
		Owner:         owner,
		RepoName:      repoName,
		PullLabels:    pullLabels,
		AllMilestones: allPullMilestones,
		StateFilter:   stateFilter,
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

	h.render(w, r, pages.PullNew(view.PullNewData{
		BasePage:     basePage(r, h.Services),
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

	renderErr := func(msg string) {
		h.render(w, r, pages.PullNew(view.PullNewData{
			BasePage:     basePage(r, h.Services),
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
	if claims, ok := middleware.ClaimsFromContext(r.Context()); ok {
		canWrite2 = h.Services.Repo.CanWrite(r.Context(), repo, claims.UserID)
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
		BasePage:          basePage(r, h.Services),
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
