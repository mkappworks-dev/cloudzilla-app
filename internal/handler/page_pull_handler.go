package handler

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"regexp"
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
		BasePage:    withRepoSubnav(basePage(r, h.Services), repo, "pull_requests", canManage),
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

	base := firstNonEmpty(r.URL.Query().Get("base"), repo.DefaultBranch)
	head := r.URL.Query().Get("head")

	canManage := false
	if claims, ok := middleware.ClaimsFromContext(r.Context()); ok {
		canManage = h.Services.Repo.CanManage(r.Context(), repo, claims.UserID)
	}

	allLabels, err := h.Services.Label.ListByRepo(r.Context(), owner, repoName)
	if err != nil {
		slog.Warn("new PR: label list failed", "owner", owner, "repo", repoName, "error", err)
	}

	var suggested []model.User
	if head != "" {
		if suggested, err = h.Services.Pull.SuggestReviewers(r.Context(), owner, repoName, base, head, 5); err != nil {
			slog.Warn("new PR: suggest reviewers failed", "owner", owner, "repo", repoName, "error", err)
		}
	}
	collaborators, err := h.Services.Repo.ListCollaborators(r.Context(), repo.ID)
	if err != nil {
		slog.Warn("new PR: list collaborators failed", "owner", owner, "repo", repoName, "error", err)
	}

	h.render(w, r, pages.PullNew(view.PullNewData{
		BasePage:     withRepoSubnav(basePage(r, h.Services), repo, "pull_requests", canManage),
		Repo:         *repo,
		Owner:        owner,
		RepoName:     repoName,
		TemplateBody: templateBody,
		Branches:     branches,
		Base:         base,
		Head:         head,
		AllLabels:    allLabels,
		Reviewer: components.ReviewerPickerData{
			Suggested: toReviewerOptions(suggested, nil),
			All:       collaboratorsToReviewerOptions(collaborators, nil),
		},
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
	allLabels, err := h.Services.Label.ListByRepo(r.Context(), owner, repoName)
	if err != nil {
		slog.Warn("new PR: label list failed", "owner", owner, "repo", repoName, "error", err)
	}

	errCollaborators, err := h.Services.Repo.ListCollaborators(r.Context(), repo.ID)
	if err != nil {
		slog.Warn("new PR: list collaborators failed", "owner", owner, "repo", repoName, "error", err)
	}
	var errSuggested []model.User
	if headBranch != "" {
		if errSuggested, err = h.Services.Pull.SuggestReviewers(r.Context(), owner, repoName, baseBranch, headBranch, 5); err != nil {
			slog.Warn("new PR: suggest reviewers failed", "owner", owner, "repo", repoName, "error", err)
		}
	}
	selectedReviewers := make(map[string]bool, len(r.Form["reviewers"]))
	for _, u := range r.Form["reviewers"] {
		selectedReviewers[u] = true
	}

	renderErr := func(msg string) {
		h.render(w, r, pages.PullNew(view.PullNewData{
			BasePage:     withRepoSubnav(basePage(r, h.Services), repo, "pull_requests", canManage),
			Repo:         *repo,
			Owner:        owner,
			RepoName:     repoName,
			TemplateBody: body,
			Branches:     branches,
			Base:         baseBranch,
			Head:         headBranch,
			AllLabels:    allLabels,
			Error:        msg,
			Reviewer: components.ReviewerPickerData{
				Suggested: toReviewerOptions(errSuggested, selectedReviewers),
				All:       collaboratorsToReviewerOptions(errCollaborators, selectedReviewers),
			},
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

	for _, raw := range r.Form["labels"] {
		if id, err := strconv.ParseInt(raw, 10, 64); err == nil {
			if aerr := h.Services.Label.AddToPull(r.Context(), owner, repoName, pr.Number, id); aerr != nil {
				slog.Warn("new PR: attach label failed", "label_id", id, "error", aerr)
			}
		}
	}

	if usernames := r.Form["reviewers"]; len(usernames) > 0 {
		if reviewers, rerr := h.Services.User.GetManyByUsernames(r.Context(), usernames); rerr != nil {
			slog.Warn("new PR: resolve reviewer usernames failed", "error", rerr)
		} else {
			// Only request reviews from users who can actually read the repo,
			// so a crafted POST cannot pull arbitrary accounts into the PR.
			eligible := reviewers[:0]
			for _, u := range reviewers {
				if h.Services.Repo.CanRead(r.Context(), repo, &u.ID) {
					eligible = append(eligible, u)
				}
			}
			if len(eligible) > 0 {
				if rerr := h.Services.PullReview.RequestReviewers(r.Context(), owner, repoName, pr.Number, eligible); rerr != nil {
					slog.Warn("new PR: request reviewers failed", "error", rerr)
				}
			}
		}
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

	rawComments, err := h.Services.Comment.ListByPull(r.Context(), pull.ID)
	if err != nil {
		slog.Warn("pull detail: comment list failed; rendering without conversation",
			"owner", owner, "repo", repoName, "pull_number", number, "error", err)
	}
	comments := make([]view.RenderedComment, 0, len(rawComments))
	for _, c := range rawComments {
		comments = append(comments, view.RenderedComment{
			Comment:  c,
			BodyHTML: renderMentionsHTML(markdown.Render(c.Body)),
		})
	}

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

	linkedIssues := h.resolvePullLinkedIssues(r.Context(), owner, repoName, pull.Body, callerID)

	pullEvents, err := h.Services.PullEvent.ListByPull(r.Context(), pull.ID)
	if err != nil {
		slog.Warn("pull detail: timeline event list failed; rendering without them",
			"owner", owner, "repo", repoName, "pull_number", number, "error", err)
	}

	h.render(w, r, pages.PullDetail(view.PullDetailData{
		BasePage:          withRepoSubnav(basePage(r, h.Services), repo, "pull_requests", canManage2),
		Repo:              *repo,
		Pull:              *pull,
		Owner:             owner,
		RepoName:          repoName,
		AuthorUsername:    authorUsername,
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
		Comments:          comments,
		Participants:      participants,
		LinkedIssues:      linkedIssues,
		Events:            pullEvents,
		CommitsCount:      mergeabilityBox.Ahead,
		CanMerge:          canMerge,
		MergeBlockReason:  mergeBlockReason,
		AutoMergeEnabled:  pull.AutoMergeEnabled,
		AutoMergeStrategy: pull.AutoMergeStrategy,
		LineComments:      lineComments,
		Mergeability:      mergeabilityBox,
	}))
}

func (h *Handler) PagePullCommits(w http.ResponseWriter, r *http.Request) {
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

	var loadErrCommits bool
	commits, err := h.Services.Code.PullCommits(owner, repoName, pull.BaseBranch, pull.HeadBranch)
	if err != nil {
		slog.Warn("pull commits: git walk failed", "owner", owner, "repo", repoName, "pull_number", number, "error", err)
		commits = nil
		loadErrCommits = true
	}

	var authorUsername string
	if author, err := h.Services.User.GetByID(r.Context(), pull.AuthorID); err == nil {
		authorUsername = author.Username
	} else {
		slog.Warn("pull commits: author lookup failed; falling back to name",
			"owner", owner, "repo", repoName, "pull_number", number, "author_id", pull.AuthorID, "error", err)
	}

	canManage := false
	canWrite := false
	if claims, ok := middleware.ClaimsFromContext(r.Context()); ok {
		canManage = h.Services.Repo.CanManage(r.Context(), repo, claims.UserID)
		canWrite = h.Services.Repo.CanWrite(r.Context(), repo, claims.UserID)
	}

	h.render(w, r, pages.PullCommits(view.PullCommitsData{
		BasePage:       withRepoSubnav(basePage(r, h.Services), repo, "pull_requests", canManage),
		OwnerName:      owner,
		Repo:           repo,
		Pull:           pull,
		AuthorUsername: authorUsername,
		Commits:        commits,
		CanWrite:       canWrite,
		LoadError:      loadErrCommits,
	}))
}

func (h *Handler) PagePullChecks(w http.ResponseWriter, r *http.Request) {
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

	var rows []components.CheckRow
	var loadErrChecks bool
	var headSHA string
	headCommit, _, rerr := h.Services.Code.ResolveRef(owner, repoName, pull.HeadBranch)
	if rerr != nil {
		slog.Warn("pull checks: ref resolution failed", "owner", owner, "repo", repoName, "pull_number", number, "error", rerr)
		loadErrChecks = true
	} else {
		headSHA = headCommit.Hash.String()
		statuses, serr := h.Services.CommitStatus.List(r.Context(), owner, repoName, headSHA)
		if serr != nil {
			slog.Warn("pull checks: status list failed", "owner", owner, "repo", repoName, "pull_number", number, "error", serr)
			loadErrChecks = true
		} else {
			rows = make([]components.CheckRow, 0, len(statuses))
			for _, s := range statuses {
				rows = append(rows, components.CheckRow{
					Context:     s.Context,
					State:       string(s.State),
					Description: s.Description,
					URL:         s.TargetURL,
				})
			}
		}
	}

	var authorUsername string
	if author, err := h.Services.User.GetByID(r.Context(), pull.AuthorID); err == nil {
		authorUsername = author.Username
	} else {
		slog.Warn("pull checks: author lookup failed; falling back to name",
			"owner", owner, "repo", repoName, "pull_number", number, "author_id", pull.AuthorID, "error", err)
	}

	canManage := false
	canWrite := false
	if claims, ok := middleware.ClaimsFromContext(r.Context()); ok {
		canManage = h.Services.Repo.CanManage(r.Context(), repo, claims.UserID)
		canWrite = h.Services.Repo.CanWrite(r.Context(), repo, claims.UserID)
	}

	h.render(w, r, pages.PullChecks(view.PullChecksData{
		BasePage:       withRepoSubnav(basePage(r, h.Services), repo, "pull_requests", canManage),
		OwnerName:      owner,
		Repo:           repo,
		Pull:           pull,
		AuthorUsername: authorUsername,
		Rows:           rows,
		HeadSHA:        headSHA,
		CanWrite:       canWrite,
		LoadError:      loadErrChecks,
	}))
}

func (h *Handler) PagePullFiles(w http.ResponseWriter, r *http.Request) {
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

	var loadErrFiles bool
	diff, err := h.Services.Code.GetPullDiff(owner, repoName, pull.BaseBranch, pull.HeadBranch)
	if err != nil {
		slog.Warn("pull files: get pull diff failed", "owner", owner, "repo", repoName, "pull_number", number, "error", err)
		diff = &service.PRDiffResult{}
		loadErrFiles = true
	}

	// Anchor index must match the pr_files template's section id="diff-N" — both
	// iterate Diff.Files in order, so sidebar links resolve to the right section.
	tree := make([]components.DiffFileTreeItem, 0, len(diff.Files))
	for i, f := range diff.Files {
		tree = append(tree, components.DiffFileTreeItem{
			Path:    f.DisplayPath(),
			Anchor:  fmt.Sprintf("diff-%d", i),
			Added:   f.Added,
			Deleted: f.Deleted,
		})
	}

	rawLineComments, _ := h.Services.PullLineComment.ListByPull(r.Context(), owner, repoName, number)
	lineComments := map[string][]RenderedLineComment{}
	for _, c := range rawLineComments {
		key := fmt.Sprintf("%s:%d", c.Path, c.Line)
		lineComments[key] = append(lineComments[key], RenderedLineComment{
			PullLineComment: c,
			BodyHTML:        markdown.Render(c.Body),
		})
	}

	var authorUsername string
	if author, err := h.Services.User.GetByID(r.Context(), pull.AuthorID); err == nil {
		authorUsername = author.Username
	} else {
		slog.Warn("pull files: author lookup failed; falling back to name",
			"owner", owner, "repo", repoName, "pull_number", number, "author_id", pull.AuthorID, "error", err)
	}

	canManage := false
	canWrite := false
	if claims, ok := middleware.ClaimsFromContext(r.Context()); ok {
		canManage = h.Services.Repo.CanManage(r.Context(), repo, claims.UserID)
		canWrite = h.Services.Repo.CanWrite(r.Context(), repo, claims.UserID)
	}

	h.render(w, r, pages.PullFiles(view.PullFilesData{
		BasePage:       withRepoSubnav(basePage(r, h.Services), repo, "pull_requests", canManage),
		OwnerName:      owner,
		Repo:           repo,
		Pull:           pull,
		AuthorUsername: authorUsername,
		Tree:           tree,
		Diff:           diff,
		CanWrite:       canWrite,
		LineComments:   lineComments,
		LoadError:      loadErrFiles,
	}))
}

var pullIssueRefRe = regexp.MustCompile(`#(\d+)`)

// resolvePullLinkedIssues extracts #N issue references from a PR body and
// returns the ones that resolve to real issues — deduped, capped at 10.
func (h *Handler) resolvePullLinkedIssues(ctx context.Context, owner, repoName, body string, callerID *int64) []view.LinkedIssue {
	matches := pullIssueRefRe.FindAllStringSubmatch(body, -1)
	if len(matches) == 0 {
		return nil
	}
	seen := map[int]bool{}
	var out []view.LinkedIssue
	for _, m := range matches {
		n, err := strconv.Atoi(m[1])
		if err != nil || seen[n] {
			continue
		}
		seen[n] = true
		issue, err := h.Services.Issue.Get(ctx, owner, repoName, n, callerID)
		if err != nil {
			continue
		}
		out = append(out, view.LinkedIssue{Number: issue.Number, Title: issue.Title, State: string(issue.State)})
		if len(out) >= 10 {
			break
		}
	}
	return out
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

func toReviewerOptions(users []model.User, selected map[string]bool) []components.ReviewerOption {
	opts := make([]components.ReviewerOption, 0, len(users))
	for _, u := range users {
		opts = append(opts, components.ReviewerOption{Username: u.Username, Selected: selected[u.Username]})
	}
	return opts
}

func collaboratorsToReviewerOptions(perms []model.Permission, selected map[string]bool) []components.ReviewerOption {
	opts := make([]components.ReviewerOption, 0, len(perms))
	for _, p := range perms {
		opts = append(opts, components.ReviewerOption{Username: p.Username, Selected: selected[p.Username]})
	}
	return opts
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
