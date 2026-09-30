package handler

import (
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/mkappworks-dev/cloudzilla-app/internal/middleware"
	"github.com/mkappworks-dev/cloudzilla-app/internal/view"
	"github.com/mkappworks-dev/cloudzilla-app/internal/view/fragments"
)

const deployKeyFormError = "#deploy-key-form-error"

// deployKeyErrorMessage maps known service-level errors to user-safe copy.
// The unmatched fallback logs the raw error and returns a generic message so
// driver-level details (pq error structure, schema names) never reach clients.
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
	slog.Error("deploy key: unexpected error", "error", err)
	return "Couldn't add deploy key. Please try again."
}

func (h *Handler) ListDeployKeys(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.ClaimsFromContext(r.Context())
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	owner := chi.URLParam(r, "owner")
	repoName := chi.URLParam(r, "repo")

	repo, ok := h.manageableRepoJSON(w, r, owner, repoName, claims.UserID)
	if !ok {
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

	repo, ok := h.manageableRepoJSON(w, r, owner, repoName, claims.UserID)
	if !ok {
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
			renderFormError(w, deployKeyFormError, "Title and public key are required.")
			return
		}
		http.Error(w, "title and public_key are required", http.StatusBadRequest)
		return
	}
	if !h.confirmGrant(w, r, claims.UserID, confirmationFrom(r), deployKeyFormError) {
		return
	}

	dk, err := h.Services.DeployKey.Add(r.Context(), repo.ID, title, publicKey, readOnly)
	if err != nil {
		msg := deployKeyErrorMessage(err)
		if r.Header.Get("HX-Request") == "true" {
			renderFormError(w, deployKeyFormError, msg)
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

	repo, ok := h.manageableRepoJSON(w, r, owner, repoName, claims.UserID)
	if !ok {
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
