package handler

import (
	"fmt"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/mkappworks-dev/cloudzilla-app/internal/middleware"
	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/service"
	"github.com/mkappworks-dev/cloudzilla-app/internal/view"
	"github.com/mkappworks-dev/cloudzilla-app/internal/view/components"
	"github.com/mkappworks-dev/cloudzilla-app/internal/view/pages"
)

// PageNewPull renders the new pull request form with branch selection and diff preview.
func (h *Handler) PageNewPull(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.ClaimsFromContext(r.Context())
	if !ok {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}
	owner := chi.URLParam(r, "owner")
	repoName := chi.URLParam(r, "repo")

	repo, ok := h.readableRepo(w, r, owner, repoName, claims.UserID)
	if !ok {
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

	canManage := h.Services.Repo.CanManage(r.Context(), repo, claims.UserID)
	canWrite := h.Services.Repo.CanWrite(r.Context(), repo, claims.UserID)

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

	r = h.withAvatars(r, append(usernames(suggested), collaboratorUsernames(collaborators)...)...)
	h.render(w, r, pages.PullNew(view.PullNewData{
		BasePage:     h.withRepoSubnav(r.Context(), basePage(r, h.Services), repo, "pull_requests", canManage),
		Repo:         *repo,
		Owner:        owner,
		RepoName:     repoName,
		TemplateBody: templateBody,
		Branches:     branches,
		Base:         base,
		Head:         head,
		CanWrite:     canWrite,
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

	repo, ok := h.readableRepo(w, r, owner, repoName, claims.UserID)
	if !ok {
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
	isDraft := r.FormValue("draft") == "1"

	canManage := h.Services.Repo.CanManage(r.Context(), repo, claims.UserID)
	// Opening a PR needs only read access; labels and reviewers are write-only metadata.
	canWrite := h.Services.Repo.CanWrite(r.Context(), repo, claims.UserID)
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
		r := h.withAvatars(r, append(usernames(errSuggested), collaboratorUsernames(errCollaborators)...)...)
		h.render(w, r, pages.PullNew(view.PullNewData{
			BasePage:     h.withRepoSubnav(r.Context(), basePage(r, h.Services), repo, "pull_requests", canManage),
			Repo:         *repo,
			Owner:        owner,
			RepoName:     repoName,
			TemplateBody: body,
			Branches:     branches,
			Base:         baseBranch,
			Head:         headBranch,
			CanWrite:     canWrite,
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

	pr, err := h.Services.Pull.Create(r.Context(), owner, repoName, claims.UserID, title, body, headBranch, baseBranch, isDraft, claims.Targets)
	if err != nil {
		renderErr(createFailedMessage(err, "pull request", "owner", owner, "repo", repoName))
		return
	}

	if canWrite {
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
	}

	go h.Services.Webhook.Dispatch(repo.ID, "pull_request", h.Services.Webhook.PullPayload("opened", *repo, *pr))

	http.Redirect(w, r, fmt.Sprintf("/%s/%s/pulls/%d", owner, repoName, pr.Number), http.StatusSeeOther)
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
