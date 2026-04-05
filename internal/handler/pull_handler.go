package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/mkappworks/cloudzilla/internal/markdown"
	"github.com/mkappworks/cloudzilla/internal/middleware"
	"github.com/mkappworks/cloudzilla/internal/model"
	"github.com/mkappworks/cloudzilla/internal/view"
	"github.com/mkappworks/cloudzilla/internal/view/fragments"
)

type createPRRequest struct {
	Title      string `json:"title"`
	Body       string `json:"body"`
	HeadBranch string `json:"head_branch"`
	BaseBranch string `json:"base_branch"`
	IsDraft    bool   `json:"is_draft"`
}

type updatePRRequest struct {
	State             string `json:"state"`
	MergeStrategy     string `json:"merge_strategy"`     // "ff" | "merge" | "squash"
	IsDraft           *bool  `json:"is_draft"`
	AutoMerge         string `json:"auto_merge"`          // "enable" | "disable"
	AutoMergeStrategy string `json:"auto_merge_strategy"` // "ff" | "merge" | "squash"
}

func (h *Handler) ListPulls(w http.ResponseWriter, r *http.Request) {
	owner := chi.URLParam(r, "owner")
	repo := chi.URLParam(r, "repo")
	prs, err := h.Services.Pull.List(r.Context(), owner, repo)
	if err != nil {
		writeError(w, http.StatusNotFound, "repo not found")
		return
	}
	writeJSON(w, http.StatusOK, prs)
}

func (h *Handler) GetPull(w http.ResponseWriter, r *http.Request) {
	owner := chi.URLParam(r, "owner")
	repo := chi.URLParam(r, "repo")
	number, _ := strconv.Atoi(chi.URLParam(r, "number"))
	pr, err := h.Services.Pull.Get(r.Context(), owner, repo, number)
	if err != nil {
		writeError(w, http.StatusNotFound, "pull request not found")
		return
	}
	writeJSON(w, http.StatusOK, pr)
}

func (h *Handler) CreatePull(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.ClaimsFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	owner := chi.URLParam(r, "owner")
	repoName := chi.URLParam(r, "repo")

	var req createPRRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	pr, err := h.Services.Pull.Create(r.Context(), owner, repoName, claims.UserID, req.Title, req.Body, req.HeadBranch, req.BaseBranch, req.IsDraft)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	repo, _ := h.Services.Repo.Get(r.Context(), owner, repoName)
	if repo != nil {
		go h.Services.Webhook.Dispatch(repo.ID, "pull_request", h.Services.Webhook.PullPayload("opened", *repo, *pr))
		repoID := repo.ID
		go h.Services.Event.Record(context.Background(), claims.UserID, claims.Username, &repoID, repoName, owner, model.EventPROpened, map[string]any{"number": pr.Number, "title": pr.Title})

		// Auto-assign code owners based on CODEOWNERS file.
		go func(owner, repoName string, pr *model.PullRequest, defaultBranch string) {
			diff, err := h.Services.Code.GetPullDiff(owner, repoName, pr.BaseBranch, pr.HeadBranch)
			if err != nil {
				return
			}
			changedFiles := make([]string, 0, len(diff.Files))
			for _, f := range diff.Files {
				changedFiles = append(changedFiles, f.NewPath)
			}
			rules, err := h.Services.Code.GetCodeOwners(owner, repoName, defaultBranch)
			if err != nil || len(rules) == 0 {
				return
			}
			owners := h.Services.Code.MatchCodeOwners(rules, changedFiles)
			for _, u := range owners {
				_ = h.Services.Assignee.AddToPull(context.Background(), owner, repoName, pr.Number, u)
			}
		}(owner, repoName, pr, repo.DefaultBranch)
	}

	writeJSON(w, http.StatusCreated, pr)
}

func (h *Handler) UpdatePull(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.ClaimsFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	owner := chi.URLParam(r, "owner")
	repoName := chi.URLParam(r, "repo")
	number, err := strconv.Atoi(chi.URLParam(r, "number"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid pull request number")
		return
	}

	repo, err := h.Services.Repo.Get(r.Context(), owner, repoName)
	if err != nil {
		writeError(w, http.StatusNotFound, "repo not found")
		return
	}
	if !h.Services.Repo.CanWrite(r.Context(), repo, claims.UserID) {
		writeError(w, http.StatusForbidden, "forbidden")
		return
	}

	var state, mergeStrategy, isDraftStr, autoMergeAction, autoMergeStrategy string
	var req *updatePRRequest
	if r.Header.Get("HX-Request") == "true" {
		if err := r.ParseForm(); err != nil {
			writeError(w, http.StatusBadRequest, "invalid request body")
			return
		}
		state = r.FormValue("state")
		mergeStrategy = r.FormValue("merge_strategy")
		isDraftStr = r.FormValue("is_draft")
		autoMergeAction = r.FormValue("auto_merge")
		autoMergeStrategy = r.FormValue("auto_merge_strategy")
	} else {
		var decoded updatePRRequest
		if err := json.NewDecoder(r.Body).Decode(&decoded); err != nil {
			writeError(w, http.StatusBadRequest, "invalid request body")
			return
		}
		req = &decoded
		state = req.State
		mergeStrategy = req.MergeStrategy
		autoMergeAction = req.AutoMerge
		autoMergeStrategy = req.AutoMergeStrategy
	}
	if mergeStrategy == "" {
		mergeStrategy = "ff"
	}

	// Validate state if provided
	if state != "" && state != "open" && state != "closed" && state != "merged" {
		writeError(w, http.StatusBadRequest, "state must be 'open', 'closed', or 'merged'")
		return
	}

	// Handle is_draft toggle
	if isDraftStr != "" || (req != nil && req.IsDraft != nil) {
		newDraft := isDraftStr == "true" || (req != nil && req.IsDraft != nil && *req.IsDraft)
		if err := h.Services.Pull.SetDraft(r.Context(), owner, repoName, number, newDraft); err != nil {
			writeError(w, http.StatusUnprocessableEntity, err.Error())
			return
		}
		pr, err := h.Services.Pull.Get(r.Context(), owner, repoName, number)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		if r.Header.Get("HX-Request") == "true" {
			h.render(w, r, fragments.PullDetail(view.PullDetailFragData{
				Pull:              *pr,
				Owner:             owner,
				Repo:              repoName,
				BodyHTML:          markdown.Render(pr.Body),
				CanWrite:          true, // already verified above
				AutoMergeEnabled:  pr.AutoMergeEnabled,
				AutoMergeStrategy: pr.AutoMergeStrategy,
			}))
			return
		}
		writeJSON(w, http.StatusOK, pr)
		return
	}

	// Handle auto-merge enable/disable
	if autoMergeAction != "" {
		var svcErr error
		if autoMergeAction == "enable" {
			if autoMergeStrategy == "" {
				autoMergeStrategy = "ff"
			}
			svcErr = h.Services.Pull.EnableAutoMerge(r.Context(), owner, repoName, number, claims.UserID, autoMergeStrategy)
		} else {
			svcErr = h.Services.Pull.DisableAutoMerge(r.Context(), owner, repoName, number, claims.UserID)
		}
		if svcErr != nil {
			writeError(w, http.StatusUnprocessableEntity, svcErr.Error())
			return
		}
		pr, err := h.Services.Pull.Get(r.Context(), owner, repoName, number)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		if r.Header.Get("HX-Request") == "true" {
			h.render(w, r, fragments.PullDetail(view.PullDetailFragData{
				Pull:              *pr,
				Owner:             owner,
				Repo:              repoName,
				BodyHTML:          markdown.Render(pr.Body),
				CanWrite:          true, // already verified above
				AutoMergeEnabled:  pr.AutoMergeEnabled,
				AutoMergeStrategy: pr.AutoMergeStrategy,
			}))
			return
		}
		writeJSON(w, http.StatusOK, pr)
		return
	}

	if state == "merged" {
		existingPR, err := h.Services.Pull.Get(r.Context(), owner, repoName, number)
		if err != nil {
			writeError(w, http.StatusNotFound, "pull request not found")
			return
		}
		if existingPR.IsDraft {
			writeError(w, http.StatusUnprocessableEntity, "cannot merge a draft pull request")
			return
		}
		if ok, reason, _ := h.Services.PullReview.CanMerge(r.Context(), existingPR.ID); !ok {
			writeError(w, http.StatusUnprocessableEntity, "merge blocked: "+reason)
			return
		}
		// Resolve the HEAD SHA of the head branch for status check enforcement.
		var headSHA string
		if _, sha, err := h.Services.Code.ResolveRef(owner, repoName, existingPR.HeadBranch); err == nil {
			headSHA = sha
		}
		if err := h.Services.BranchProtection.CheckMerge(r.Context(), repo.ID, existingPR, headSHA); err != nil {
			writeError(w, http.StatusUnprocessableEntity, "merge blocked: "+err.Error())
			return
		}
		authorName := claims.Username
		authorEmail := claims.Username + "@localhost"
		base := existingPR.BaseBranch
		head := existingPR.HeadBranch
		switch mergeStrategy {
		case "merge":
			err = h.Services.Code.ThreeWayMergePullRequest(owner, repoName, base, head, authorName, authorEmail)
		case "squash":
			err = h.Services.Code.SquashMergePullRequest(owner, repoName, base, head, authorName, authorEmail)
		default:
			err = h.Services.Code.MergePullRequest(owner, repoName, base, head)
		}
		if err != nil {
			writeError(w, http.StatusUnprocessableEntity, err.Error())
			return
		}
	}

	pr, err := h.Services.Pull.SetState(r.Context(), owner, repoName, number, model.PRState(state))
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	go h.Services.Webhook.Dispatch(repo.ID, "pull_request", h.Services.Webhook.PullPayload(state, *repo, *pr))
	go func() {
		h.Services.Notification.NotifyPRStateChange(r.Context(), *repo, *pr, claims.UserID, claims.Username)
	}()
	evType := model.EventPRClosed
	if pr.State == model.PRStateMerged {
		evType = model.EventPRMerged
	}
	repoID := repo.ID
	go h.Services.Event.Record(context.Background(), claims.UserID, claims.Username, &repoID, repoName, owner, evType, map[string]any{"number": pr.Number})

	if r.Header.Get("HX-Request") == "true" {
		h.render(w, r, fragments.PullDetail(view.PullDetailFragData{
			Pull: *pr, Owner: owner, Repo: repoName,
			BodyHTML: markdown.Render(pr.Body),
		}))
		return
	}
	writeJSON(w, http.StatusOK, pr)
}

// tryAutoMerge checks if auto-merge conditions are satisfied for the given PR and,
// if so, executes the merge. Safe to call as a goroutine — idempotent and silently
// no-ops when conditions are not met.
func (h *Handler) tryAutoMerge(owner, repoName string, pullID int64) {
	ctx := context.Background()

	pr, err := h.Services.Pull.GetByID(ctx, pullID)
	if err != nil {
		return
	}
	// Guard: only act when auto-merge is armed and PR is eligible.
	if !pr.AutoMergeEnabled || pr.State != model.PRStateOpen || pr.IsDraft {
		return
	}

	canMerge, _, _ := h.Services.PullReview.CanMerge(ctx, pr.ID)
	if !canMerge {
		return
	}

	repo, err := h.Services.Repo.Get(ctx, owner, repoName)
	if err != nil {
		return
	}

	_, headSHA, err := h.Services.Code.ResolveRef(owner, repoName, pr.HeadBranch)
	if err != nil {
		return
	}
	if err := h.Services.BranchProtection.CheckMerge(ctx, repo.ID, pr, headSHA); err != nil {
		return
	}

	// All checks passed — execute merge.
	var mergeErr error
	switch pr.AutoMergeStrategy {
	case "merge":
		mergeErr = h.Services.Code.ThreeWayMergePullRequest(owner, repoName, pr.BaseBranch, pr.HeadBranch, "auto-merge", "auto-merge@localhost")
	case "squash":
		mergeErr = h.Services.Code.SquashMergePullRequest(owner, repoName, pr.BaseBranch, pr.HeadBranch, "auto-merge", "auto-merge@localhost")
	default: // "ff"
		mergeErr = h.Services.Code.MergePullRequest(owner, repoName, pr.BaseBranch, pr.HeadBranch)
	}
	if mergeErr != nil {
		return
	}
	_, _ = h.Services.Pull.SetState(ctx, owner, repoName, pr.Number, model.PRStateMerged)
}
