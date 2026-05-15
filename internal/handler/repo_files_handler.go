package handler

import (
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/mkappworks-dev/cloudzilla-app/internal/middleware"
	"github.com/mkappworks-dev/cloudzilla-app/internal/view"
	"github.com/mkappworks-dev/cloudzilla-app/internal/view/pages"
)

// maxUploadBytes caps a single uploaded file committed through the web UI.
const maxUploadBytes = 25 << 20

// UpdateRepo handles PATCH /api/repos/{owner}/{repo} — updates the editable
// repository metadata (description, website, license) from the About panel.
func (h *Handler) UpdateRepo(w http.ResponseWriter, r *http.Request) {
	owner := chi.URLParam(r, "owner")
	repoName := chi.URLParam(r, "repo")
	claims, ok := middleware.ClaimsFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	repo, err := h.Services.Repo.Get(r.Context(), owner, repoName)
	if err != nil {
		writeError(w, http.StatusNotFound, "repo not found")
		return
	}
	if err := r.ParseForm(); err != nil {
		writeError(w, http.StatusBadRequest, "invalid form")
		return
	}
	err = h.Services.Repo.UpdateMeta(r.Context(), repo.ID, claims.UserID,
		r.FormValue("description"), r.FormValue("website"), r.FormValue("license"))
	if err != nil {
		if strings.Contains(err.Error(), "permission") {
			writeError(w, http.StatusForbidden, "you do not have permission to edit this repository")
		} else {
			writeError(w, http.StatusInternalServerError, "failed to update repository")
		}
		return
	}
	w.Header().Set("HX-Refresh", "true")
	w.WriteHeader(http.StatusNoContent)
}

// DownloadArchive handles GET /{owner}/{repo}/archive/{ref} — streams a zip of
// the repository tree at the given ref. A trailing ".zip" on the ref is ignored.
func (h *Handler) DownloadArchive(w http.ResponseWriter, r *http.Request) {
	owner := chi.URLParam(r, "owner")
	repoName := chi.URLParam(r, "repo")
	ref := strings.TrimSuffix(chi.URLParam(r, "ref"), ".zip")

	repo, err := h.Services.Repo.Get(r.Context(), owner, repoName)
	if err != nil {
		h.NotFound(w, r)
		return
	}
	var uid *int64
	if claims, ok := middleware.ClaimsFromContext(r.Context()); ok {
		uid = &claims.UserID
	}
	if !h.Services.Repo.CanRead(r.Context(), repo, uid) {
		h.NotFound(w, r)
		return
	}

	label := strings.ReplaceAll(ref, "/", "-")
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", repoName+"-"+label+".zip"))
	if err := h.Services.Code.ArchiveZip(owner, repoName, ref, w); err != nil {
		// Headers are already committed, so the response can't switch to an
		// error status — log it and let the truncated stream surface client-side.
		slog.Error("archive zip failed", "owner", owner, "repo", repoName, "ref", ref, "error", err)
	}
}

// PageNewFile renders the create-file / upload page.
func (h *Handler) PageNewFile(w http.ResponseWriter, r *http.Request) {
	owner := chi.URLParam(r, "owner")
	repoName := chi.URLParam(r, "repo")
	ref := chi.URLParam(r, "ref")
	dir := strings.Trim(chi.URLParam(r, "*"), "/")

	claims, ok := middleware.ClaimsFromContext(r.Context())
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	repo, err := h.Services.Repo.Get(r.Context(), owner, repoName)
	if err != nil {
		h.NotFound(w, r)
		return
	}
	if !h.Services.Repo.CanWrite(r.Context(), repo, claims.UserID) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	if ref == "" {
		ref = repo.DefaultBranch
	}
	canManage := h.Services.Repo.CanManage(r.Context(), repo, claims.UserID)
	h.render(w, r, pages.NewFile(view.NewFileData{
		BasePage: withRepoSubnav(basePage(r, h.Services), owner, repoName, "code", canManage),
		Owner:    owner,
		RepoName: repoName,
		Ref:      ref,
		Dir:      dir,
	}))
}

// SubmitNewFile handles POST /{owner}/{repo}/new/{ref} — commits a typed or
// uploaded file to the branch and redirects to the new file's blob view.
func (h *Handler) SubmitNewFile(w http.ResponseWriter, r *http.Request) {
	owner := chi.URLParam(r, "owner")
	repoName := chi.URLParam(r, "repo")
	ref := chi.URLParam(r, "ref")

	claims, ok := middleware.ClaimsFromContext(r.Context())
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	repo, err := h.Services.Repo.Get(r.Context(), owner, repoName)
	if err != nil {
		h.NotFound(w, r)
		return
	}
	if !h.Services.Repo.CanWrite(r.Context(), repo, claims.UserID) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	if repo.IsArchived {
		http.Error(w, "repository is archived", http.StatusForbidden)
		return
	}
	if ref == "" {
		ref = repo.DefaultBranch
	}

	path := strings.TrimSpace(r.FormValue("path"))
	content := []byte(r.FormValue("content"))
	message := strings.TrimSpace(r.FormValue("message"))

	// An uploaded file, when present, takes precedence over the textarea.
	if file, header, ferr := r.FormFile("file"); ferr == nil {
		defer file.Close()
		data, rerr := io.ReadAll(io.LimitReader(file, maxUploadBytes))
		if rerr != nil {
			http.Error(w, "failed to read uploaded file", http.StatusInternalServerError)
			return
		}
		content = data
		if path == "" {
			path = header.Filename
		}
	}
	if path == "" {
		http.Error(w, "a file path is required", http.StatusBadRequest)
		return
	}
	if message == "" {
		message = "Create " + path
	}

	user, err := h.Services.User.GetByID(r.Context(), claims.UserID)
	if err != nil {
		http.Error(w, "failed to load user", http.StatusInternalServerError)
		return
	}
	email := user.Email
	if email == "" {
		email = user.Username + "@localhost"
	}

	if err := h.Services.Code.CommitFile(owner, repoName, ref, path, content, user.Username, email, message); err != nil {
		slog.Error("commit file failed", "owner", owner, "repo", repoName, "ref", ref, "path", path, "error", err)
		http.Error(w, err.Error(), http.StatusUnprocessableEntity)
		return
	}
	http.Redirect(w, r, "/"+owner+"/"+repoName+"/blob/"+ref+"/"+path, http.StatusSeeOther)
}
