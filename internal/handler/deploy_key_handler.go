package handler

import (
	"html"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/mkappworks-dev/cloudzilla-app/internal/middleware"
	"github.com/mkappworks-dev/cloudzilla-app/internal/view"
	"github.com/mkappworks-dev/cloudzilla-app/internal/view/fragments"
)

// renderDeployKeyFormError redirects htmx's swap to the in-dialog error slot
// so the dialog stays open and shows the validation error inline. Uses status
// 200 because htmx skips swaps on 4xx by default; the form distinguishes
// success from error by inspecting the swapped target's id.
func renderDeployKeyFormError(w http.ResponseWriter, msg string) {
	w.Header().Set("HX-Retarget", "#deploy-key-form-error")
	w.Header().Set("HX-Reswap", "innerHTML")
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write([]byte(`<div class="rounded-md border border-destructive/30 bg-destructive/10 text-destructive text-xs p-3" role="alert">` + html.EscapeString(msg) + `</div>`))
}

// deployKeyErrorMessage turns a service-level error into copy fit for a toast.
// Unique-constraint violations come back from Postgres as a wrapped pq error
// containing "duplicate key value"; the SSH parser returns "invalid public key".
func deployKeyErrorMessage(err error) string {
	if err == nil {
		return ""
	}
	msg := err.Error()
	switch {
	case strings.Contains(msg, "invalid public key"):
		return "Invalid public key. Paste the full contents of a .pub file (e.g. starts with ssh-ed25519 or ssh-rsa)."
	case strings.Contains(msg, "already registered as a user SSH key"):
		return "This key is already registered to your account. Deploy keys must be distinct from user SSH keys."
	case strings.Contains(msg, "duplicate key") || strings.Contains(msg, "unique constraint"):
		return "This key is already a deploy key on this repository."
	}
	return "Couldn't add deploy key: " + msg
}

func (h *Handler) ListDeployKeys(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.ClaimsFromContext(r.Context())
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	owner := chi.URLParam(r, "owner")
	repoName := chi.URLParam(r, "repo")

	repo, err := h.Services.Repo.Get(r.Context(), owner, repoName)
	if err != nil {
		http.Error(w, "repo not found", http.StatusNotFound)
		return
	}

	if !h.Services.Repo.CanManage(r.Context(), repo, claims.UserID) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}

	keys, err := h.Services.DeployKey.List(r.Context(), repo.ID)
	if err != nil {
		http.Error(w, "failed to list deploy keys", http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, keys)
}

func (h *Handler) AddDeployKey(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.ClaimsFromContext(r.Context())
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	owner := chi.URLParam(r, "owner")
	repoName := chi.URLParam(r, "repo")

	repo, err := h.Services.Repo.Get(r.Context(), owner, repoName)
	if err != nil {
		http.Error(w, "repo not found", http.StatusNotFound)
		return
	}

	if !h.Services.Repo.CanManage(r.Context(), repo, claims.UserID) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}

	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}

	title := r.FormValue("title")
	publicKey := r.FormValue("public_key")
	readOnly := r.FormValue("read_only") == "true" // checkbox sends "true" when checked; unchecked = read-write

	if title == "" || publicKey == "" {
		if r.Header.Get("HX-Request") == "true" {
			renderDeployKeyFormError(w, "Title and public key are required.")
			return
		}
		http.Error(w, "title and public_key are required", http.StatusBadRequest)
		return
	}

	dk, err := h.Services.DeployKey.Add(r.Context(), repo.ID, title, publicKey, readOnly)
	if err != nil {
		msg := deployKeyErrorMessage(err)
		if r.Header.Get("HX-Request") == "true" {
			renderDeployKeyFormError(w, msg)
			return
		}
		writeError(w, http.StatusUnprocessableEntity, msg)
		return
	}

	if r.Header.Get("HX-Request") == "true" {
		keys, _ := h.Services.DeployKey.List(r.Context(), repo.ID)
		h.render(w, r, fragments.DeployKeys(view.DeployKeysFragData{
			Owner:      owner,
			RepoName:   repoName,
			RepoID:     repo.ID,
			DeployKeys: keys,
			CanManage:  true,
		}))
		return
	}
	writeJSON(w, http.StatusCreated, dk)
}

func (h *Handler) DeleteDeployKey(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.ClaimsFromContext(r.Context())
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	owner := chi.URLParam(r, "owner")
	repoName := chi.URLParam(r, "repo")

	repo, err := h.Services.Repo.Get(r.Context(), owner, repoName)
	if err != nil {
		http.Error(w, "repo not found", http.StatusNotFound)
		return
	}

	if !h.Services.Repo.CanManage(r.Context(), repo, claims.UserID) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}

	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return
	}

	if err := h.Services.DeployKey.Delete(r.Context(), id, repo.ID); err != nil {
		http.Error(w, "failed to delete deploy key", http.StatusInternalServerError)
		return
	}

	if r.Header.Get("HX-Request") == "true" {
		keys, _ := h.Services.DeployKey.List(r.Context(), repo.ID)
		h.render(w, r, fragments.DeployKeys(view.DeployKeysFragData{
			Owner:      owner,
			RepoName:   repoName,
			RepoID:     repo.ID,
			DeployKeys: keys,
			CanManage:  true,
		}))
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
