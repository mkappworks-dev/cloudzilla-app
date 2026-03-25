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
)

type createPRRequest struct {
	Title      string `json:"title"`
	Body       string `json:"body"`
	HeadBranch string `json:"head_branch"`
	BaseBranch string `json:"base_branch"`
	IsDraft    bool   `json:"is_draft"`
}

type updatePRRequest struct {
	State         string `json:"state"`
	MergeStrategy string `json:"merge_strategy"` // "ff" | "merge" | "squash"
	IsDraft       *bool  `json:"is_draft"`
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
	owner := chi.URLParam(r, "owner")
	repoName := chi.URLParam(r, "repo")
	number, _ := strconv.Atoi(chi.URLParam(r, "number"))

	var state, mergeStrategy, isDraftStr string
	var req *updatePRRequest
	if r.Header.Get("HX-Request") == "true" {
		if err := r.ParseForm(); err != nil {
			writeError(w, http.StatusBadRequest, "invalid request body")
			return
		}
		state = r.FormValue("state")
		mergeStrategy = r.FormValue("merge_strategy")
		isDraftStr = r.FormValue("is_draft")
	} else {
		var decoded updatePRRequest
		if err := json.NewDecoder(r.Body).Decode(&decoded); err != nil {
			writeError(w, http.StatusBadRequest, "invalid request body")
			return
		}
		req = &decoded
		state = req.State
		mergeStrategy = req.MergeStrategy
	}
	if mergeStrategy == "" {
		mergeStrategy = "ff"
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
			h.renderFragment(w, "fragment-pull-detail", PullDetailFragData{
				Pull: *pr, Owner: owner, Repo: repoName,
				BodyHTML: markdown.Render(pr.Body),
			})
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
		repo, err := h.Services.Repo.Get(r.Context(), owner, repoName)
		if err != nil {
			writeError(w, http.StatusNotFound, "repo not found")
			return
		}
		if err := h.Services.BranchProtection.CheckMerge(r.Context(), repo.ID, existingPR, headSHA); err != nil {
			writeError(w, http.StatusUnprocessableEntity, "merge blocked: "+err.Error())
			return
		}
		claims, _ := middleware.ClaimsFromContext(r.Context())
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

	repo, _ := h.Services.Repo.Get(r.Context(), owner, repoName)
	if repo != nil {
		go h.Services.Webhook.Dispatch(repo.ID, "pull_request", h.Services.Webhook.PullPayload(state, *repo, *pr))
		if claims, ok := middleware.ClaimsFromContext(r.Context()); ok {
			go func() {
				h.Services.Notification.NotifyPRStateChange(r.Context(), *repo, *pr, claims.UserID, claims.Username)
			}()
		}
	}

	if r.Header.Get("HX-Request") == "true" {
		h.renderFragment(w, "fragment-pull-detail", PullDetailFragData{
			Pull: *pr, Owner: owner, Repo: repoName,
			BodyHTML: markdown.Render(pr.Body),
		})
		return
	}
	writeJSON(w, http.StatusOK, pr)
}
