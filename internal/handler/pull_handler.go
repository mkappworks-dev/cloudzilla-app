package handler

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/mkappworks/cloudzilla/internal/middleware"
	"github.com/mkappworks/cloudzilla/internal/model"
)

type createPRRequest struct {
	Title      string `json:"title"`
	Body       string `json:"body"`
	HeadBranch string `json:"head_branch"`
	BaseBranch string `json:"base_branch"`
}

type updatePRRequest struct {
	State         string `json:"state"`
	MergeStrategy string `json:"merge_strategy"` // "ff" | "merge" | "squash"
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
	repo := chi.URLParam(r, "repo")

	var req createPRRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	pr, err := h.Services.Pull.Create(r.Context(), owner, repo, claims.UserID, req.Title, req.Body, req.HeadBranch, req.BaseBranch)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, pr)
}

func (h *Handler) UpdatePull(w http.ResponseWriter, r *http.Request) {
	owner := chi.URLParam(r, "owner")
	repo := chi.URLParam(r, "repo")
	number, _ := strconv.Atoi(chi.URLParam(r, "number"))

	var state, mergeStrategy string
	if r.Header.Get("HX-Request") == "true" {
		if err := r.ParseForm(); err != nil {
			writeError(w, http.StatusBadRequest, "invalid request body")
			return
		}
		state = r.FormValue("state")
		mergeStrategy = r.FormValue("merge_strategy")
	} else {
		var req updatePRRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid request body")
			return
		}
		state = req.State
		mergeStrategy = req.MergeStrategy
	}
	if mergeStrategy == "" {
		mergeStrategy = "ff"
	}

	if state == "merged" {
		existingPR, err := h.Services.Pull.Get(r.Context(), owner, repo, number)
		if err != nil {
			writeError(w, http.StatusNotFound, "pull request not found")
			return
		}
		claims, _ := middleware.ClaimsFromContext(r.Context())
		authorName := claims.Username
		authorEmail := claims.Username + "@localhost"
		base := existingPR.BaseBranch
		head := existingPR.HeadBranch
		switch mergeStrategy {
		case "merge":
			err = h.Services.Code.ThreeWayMergePullRequest(owner, repo, base, head, authorName, authorEmail)
		case "squash":
			err = h.Services.Code.SquashMergePullRequest(owner, repo, base, head, authorName, authorEmail)
		default:
			err = h.Services.Code.MergePullRequest(owner, repo, base, head)
		}
		if err != nil {
			writeError(w, http.StatusUnprocessableEntity, err.Error())
			return
		}
	}

	pr, err := h.Services.Pull.SetState(r.Context(), owner, repo, number, model.PRState(state))
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	if r.Header.Get("HX-Request") == "true" {
		h.renderFragment(w, "fragment-pull-detail", PullDetailFragData{
			Pull: *pr, Owner: owner, Repo: repo,
		})
		return
	}
	writeJSON(w, http.StatusOK, pr)
}
