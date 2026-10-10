package handler

import (
	"fmt"
	"log/slog"
	"net/http"
	"slices"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/mkappworks-dev/cloudzilla-app/internal/markdown"
	"github.com/mkappworks-dev/cloudzilla-app/internal/middleware"
	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/view"
	"github.com/mkappworks-dev/cloudzilla-app/internal/view/components"
	"github.com/mkappworks-dev/cloudzilla-app/internal/view/pages"
)

// PagePullDetail renders the pull request detail page with reviews and merge controls.
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

	var viewerID *int64
	if claims, ok := middleware.ClaimsFromContext(r.Context()); ok {
		viewerID = &claims.UserID
	}
	if !h.Services.Repo.CanRead(r.Context(), repo, viewerID) {
		h.NotFound(w, r)
		return
	}

	pull, err := h.Services.Pull.Get(r.Context(), owner, repoName, number)
	if err != nil {
		h.NotFound(w, r)
		return
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
	var callerID *int64
	if claims, ok := middleware.ClaimsFromContext(r.Context()); ok {
		canWrite2 = h.Services.Repo.CanWrite(r.Context(), repo, claims.UserID)
		canManage2 = h.Services.Repo.CanManage(r.Context(), repo, claims.UserID)
		id := claims.UserID
		callerID = &id
	}

	var headStatuses []model.CommitStatus
	if headCommit, _, err := h.Services.Code.ResolveRef(owner, repoName, pull.HeadBranch); err == nil {
		headStatuses, _ = h.Services.CommitStatus.List(r.Context(), owner, repoName, headCommit.Hash.String())
	}
	if headStatuses == nil {
		headStatuses = []model.CommitStatus{}
	}

	pullMilestone, _ := h.Services.Milestone.GetForPull(r.Context(), pull.ID, callerID)
	allPullDetailMilestones, _ := h.Services.Milestone.ListByRepo(r.Context(), owner, repoName, callerID)
	if allPullDetailMilestones == nil {
		allPullDetailMilestones = []model.Milestone{}
	}

	reviews, _ := h.Services.PullReview.ListByPull(r.Context(), owner, repoName, number)
	if reviews == nil {
		reviews = []model.PullReview{}
	}
	canMerge, mergeBlockReason, _ := h.Services.PullReview.CanMerge(r.Context(), pull.ID)

	// The body renders before the comments so it is first in line for the request's highlight budget.
	bodyHTML := markdown.RenderCtx(r.Context(), pull.Body)
	rawComments, err := h.Services.Comment.ListByPull(r.Context(), pull.ID)
	if err != nil {
		slog.Warn("pull detail: comment list failed; rendering without conversation",
			"owner", owner, "repo", repoName, "pull_number", number, "error", err)
	}
	comments := make([]view.RenderedComment, 0, len(rawComments))
	for _, c := range rawComments {
		comments = append(comments, view.RenderedComment{
			Comment:  c,
			BodyHTML: renderMentionsHTML(markdown.RenderCtx(r.Context(), c.Body)),
		})
	}

	mergeabilityBox := components.MergeabilityBoxData{
		PatchURL:   fmt.Sprintf("/api/repos/%s/%s/pulls/%d", owner, repoName, pull.Number),
		BaseBranch: pull.BaseBranch,
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
	if repo.ContentReadOnly() {
		mergeabilityBox.CanFastForward, mergeabilityBox.CanThreeWayMerge, mergeabilityBox.CanSquash = false, false, false
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

	var authorUsername string
	if author, err := h.Services.User.GetByID(r.Context(), pull.AuthorID); err == nil {
		authorUsername = author.Username
	} else {
		slog.Warn("pull detail: author lookup failed; falling back to name",
			"owner", owner, "repo", repoName, "pull_number", pull.Number, "error", err)
	}

	seenParticipant := map[string]bool{}
	participants := make([]string, 0, len(comments)+len(reviews)+1)
	addParticipant := func(name string) {
		if name != "" && !seenParticipant[name] {
			seenParticipant[name] = true
			participants = append(participants, name)
		}
	}
	addParticipant(firstNonEmpty(authorUsername, pull.AuthorName))
	for _, c := range rawComments {
		addParticipant(c.AuthorName)
	}
	for _, rv := range reviews {
		addParticipant(rv.AuthorName)
	}

	linkedIssueModels, err := h.Services.Issue.LinkedForPull(r.Context(), pull.ID, callerID)
	if err != nil {
		slog.Warn("pull detail: linked issues list failed; rendering without them",
			"owner", owner, "repo", repoName, "pull_number", number, "error", err)
	}
	repoIssueModels, err := h.Services.Issue.List(r.Context(), owner, repoName, callerID)
	if err != nil {
		slog.Warn("pull detail: repo issue list failed; link picker will be empty",
			"owner", owner, "repo", repoName, "pull_number", number, "error", err)
	}

	threadSub, err := h.threadSubscriptionData(r, repo, model.ThreadKindPull, "pulls", pull.Number)
	if err != nil {
		slog.Warn("pull detail: subscription lookup failed; rendering as not subscribed", "owner", owner, "repo", repoName, "pull_number", number, "error", err)
	}

	pullEvents, err := h.Services.PullEvent.ListByPull(r.Context(), pull.ID)
	if err != nil {
		slog.Warn("pull detail: timeline event list failed; rendering without them",
			"owner", owner, "repo", repoName, "pull_number", number, "error", err)
	}

	collaborators, err := h.Services.Repo.ListCollaborators(r.Context(), repo.ID)
	if err != nil {
		slog.Warn("pull detail: list collaborators failed", "owner", owner, "repo", repoName, "error", err)
	}

	chromeCounts := h.pullChromeCounts(r.Context(), owner, repoName, pull, convBadge|checksBadge)
	chromeCounts.ConvCount = len(rawComments) + submittedReviewCount(reviews)
	chromeCounts.ChecksTotal, chromeCounts.ChecksPassed = checkCounts(headStatuses)

	r = h.withAvatars(r, slices.Concat(participants, collaboratorUsernames(collaborators), usernames(pullAssignees))...)
	h.render(w, r, pages.PullDetail(view.PullDetailData{
		BasePage:           h.withRepoSubnav(r.Context(), basePage(r, h.Services), repo, "pull_requests", canManage2),
		Repo:               *repo,
		Pull:               *pull,
		Owner:              owner,
		RepoName:           repoName,
		AuthorUsername:     authorUsername,
		BodyHTML:           bodyHTML,
		Labels:             pullLabels2,
		Assignees:          pullAssignees,
		AllLabels:          allLabels2,
		Milestone:          pullMilestone,
		AllMilestones:      allPullDetailMilestones,
		CanWrite:           canWrite2,
		Collaborators:      collaboratorUsernames(collaborators),
		HeadStatuses:       headStatuses,
		Reviews:            reviews,
		Comments:           comments,
		Participants:       participants,
		LinkedIssues:       linkedIssuesToView(owner, repoName, linkedIssueModels),
		LinkableIssues:     linkedIssuesToView(owner, repoName, repoIssueModels),
		ThreadSubscription: threadSub,
		Events:             pullEvents,
		PullChromeCounts:   chromeCounts,
		CanMerge:           canMerge,
		MergeBlockReason:   mergeBlockReason,
		AutoMergeEnabled:   pull.AutoMergeEnabled,
		AutoMergeStrategy:  pull.AutoMergeStrategy,
		Mergeability:       mergeabilityBox,
	}))
}
