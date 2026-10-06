package handler

import (
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/mkappworks-dev/cloudzilla-app/internal/codeurl"
	"github.com/mkappworks-dev/cloudzilla-app/internal/middleware"
	"github.com/mkappworks-dev/cloudzilla-app/internal/service"
	"github.com/mkappworks-dev/cloudzilla-app/internal/view"
	"github.com/mkappworks-dev/cloudzilla-app/internal/view/pages"
)

const (
	// maxUploadBytes caps a single uploaded file committed through the web UI.
	maxUploadBytes = 25 << 20
	// MaxNewFileBodyBytes caps the New file form's request body: the largest
	// upload plus room for the other fields.
	MaxNewFileBodyBytes = maxUploadBytes + 1<<20
	// maxRawBlobBytes bounds a raw request's memory: go-git inflates a packed
	// blob whole before the first byte can be read.
	maxRawBlobBytes = 25 << 20
)

var fileTooLargeMsg = fmt.Sprintf("files are limited to %d MB", maxUploadBytes>>20)

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
	repo, ok := h.readableRepoJSON(w, r, owner, repoName)
	if !ok {
		return
	}
	if err := r.ParseForm(); err != nil {
		writeError(w, http.StatusBadRequest, "invalid form")
		return
	}
	err := h.Services.Repo.UpdateMeta(r.Context(), repo.ID, claims.UserID,
		r.FormValue("description"), r.FormValue("website"), r.FormValue("license"))
	if err != nil {
		if errors.Is(err, service.ErrForbidden) {
			writeError(w, http.StatusForbidden, "you do not have permission to edit this repository")
		} else {
			slog.Error("repo: update meta failed", "owner", owner, "repo", repoName, "error", err)
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
	ref := strings.TrimSuffix(routeRefPath(r), ".zip")

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

	if _, _, err := h.Services.Code.ResolveRef(owner, repoName, ref); err != nil {
		if errors.Is(err, service.ErrRefNotFound) || errors.Is(err, service.ErrEmptyRepo) {
			h.NotFound(w, r)
			return
		}
		slog.Error("archive ref resolve failed", "owner", owner, "repo", repoName, "ref", ref, "error", err)
		http.Error(w, "failed to build archive", http.StatusInternalServerError)
		return
	}

	label := strings.ReplaceAll(ref, "/", "-")
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", repoName+"-"+label+".zip"))
	if err := h.Services.Code.ArchiveZip(owner, repoName, ref, w); err != nil {
		// Once the stream has started the status is committed, so a mid-stream
		// failure can only be logged and surface client-side as a truncated zip.
		slog.Error("archive zip failed", "owner", owner, "repo", repoName, "ref", ref, "error", err)
	}
}

// RawFile serves a file's bytes at a ref. Text goes out as text/plain and
// everything else as an octet stream, sandboxed, so an uploaded .html or .svg
// can't run script on the forge's origin.
func (h *Handler) RawFile(w http.ResponseWriter, r *http.Request) {
	owner := chi.URLParam(r, "owner")
	repoName := chi.URLParam(r, "repo")
	ref := unescapeRouted(r, chi.URLParam(r, "ref"))
	path := unescapeRouted(r, chi.URLParam(r, "*"))

	repo, err := h.Services.Repo.Get(r.Context(), owner, repoName)
	if err != nil || !h.Services.Repo.CanRead(r.Context(), repo, viewerOf(r)) {
		h.NotFound(w, r)
		return
	}

	blob, err := h.Services.Code.OpenRawBlob(owner, repoName, ref, path, maxRawBlobBytes)
	if errors.Is(err, service.ErrBlobTooLarge) {
		http.Error(w, fmt.Sprintf("files over %d MB can't be viewed raw; clone the repository to get this one", maxRawBlobBytes>>20), http.StatusForbidden)
		return
	}
	if err != nil {
		h.NotFound(w, r)
		return
	}
	defer func() { _ = blob.Close() }()

	contentType := "text/plain; charset=utf-8"
	if blob.IsBinary {
		contentType = "application/octet-stream"
	}
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Content-Length", strconv.FormatInt(blob.Size, 10))
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; sandbox")
	if _, err := io.Copy(w, blob); err != nil {
		slog.Error("raw file stream failed", "owner", owner, "repo", repoName, "ref", ref, "path", path, "error", err)
	}
}

// PageNewFile renders the create-file / upload page.
func (h *Handler) PageNewFile(w http.ResponseWriter, r *http.Request) {
	owner := chi.URLParam(r, "owner")
	repoName := chi.URLParam(r, "repo")

	claims, ok := middleware.ClaimsFromContext(r.Context())
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	repo, ok := h.readableRepo(w, r, owner, repoName, claims.UserID)
	if !ok {
		return
	}
	if !h.Services.Repo.CanWrite(r.Context(), repo, claims.UserID) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	ref, dir := h.Services.Code.SplitRefPath(owner, repoName, routeRefPath(r))
	dir = strings.Trim(dir, "/")
	if ref == "" {
		ref = repo.DefaultBranch
	}
	canManage := h.Services.Repo.CanManage(r.Context(), repo, claims.UserID)
	h.render(w, r, pages.NewFile(view.NewFileData{
		BasePage: h.withRepoSubnav(r.Context(), basePage(r, h.Services), repo, "code", canManage),
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
	ref := routeRefPath(r)

	claims, ok := middleware.ClaimsFromContext(r.Context())
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	repo, ok := h.readableRepo(w, r, owner, repoName, claims.UserID)
	if !ok {
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

	if err := parseNewFileForm(r); err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			http.Error(w, fileTooLargeMsg, http.StatusRequestEntityTooLarge)
			return
		}
		http.Error(w, "invalid form", http.StatusBadRequest)
		return
	}
	path := strings.TrimSpace(r.FormValue("path"))
	dir := strings.Trim(r.FormValue("dir"), "/")
	content := []byte(r.FormValue("content"))
	message := strings.TrimSpace(r.FormValue("message"))

	// An uploaded file, when present, takes precedence over the textarea.
	if file, header, ferr := r.FormFile("file"); ferr == nil {
		defer func() { _ = file.Close() }()
		if header.Size > maxUploadBytes {
			http.Error(w, fileTooLargeMsg, http.StatusRequestEntityTooLarge)
			return
		}
		data, rerr := io.ReadAll(file)
		if rerr != nil {
			http.Error(w, "failed to read uploaded file", http.StatusInternalServerError)
			return
		}
		content = data
		// A blank path means the directory the New file page was opened from.
		if path == "" && dir != "" {
			path = dir + "/"
		}
		if path == "" || strings.HasSuffix(path, "/") {
			path += header.Filename
		}
	}
	if path == "" {
		http.Error(w, "a file path is required", http.StatusBadRequest)
		return
	}
	if strings.HasSuffix(path, "/") {
		http.Error(w, "the path ends in /: add a file name or upload a file", http.StatusBadRequest)
		return
	}
	// CommitFile ignores a leading /, and the redirect must name the committed path.
	path = strings.TrimLeft(path, "/")
	if message == "" {
		message = "Create " + path
	}

	author, err := h.Services.User.CommitAuthor(r.Context(), claims.UserID)
	if err != nil {
		http.Error(w, "failed to load user", http.StatusInternalServerError)
		return
	}

	if err := h.Services.Code.CommitFile(owner, repoName, ref, path, content, author, message); err != nil {
		if errors.Is(err, service.ErrRefMoved) {
			http.Error(w, branchMovedMsg, http.StatusConflict)
			return
		}
		if errors.Is(err, service.ErrPathCollision) {
			http.Error(w, err.Error(), http.StatusConflict)
			return
		}
		if errors.Is(err, service.ErrInvalidFilePath) {
			http.Error(w, err.Error(), http.StatusUnprocessableEntity)
			return
		}
		slog.Error("commit file failed", "owner", owner, "repo", repoName, "ref", ref, "path", path, "error", err)
		http.Error(w, err.Error(), http.StatusUnprocessableEntity)
		return
	}
	http.Redirect(w, r, codeurl.Path(owner, repoName, "blob", ref, path), http.StatusSeeOther)
}

// parseNewFileForm parses either encoding of the New file form, in memory since
// the body limit bounds it. ParseForm goes first because, on a urlencoded body,
// ParseMultipartForm reports ErrNotMultipart in place of ParseForm's error.
func parseNewFileForm(r *http.Request) error {
	if err := r.ParseForm(); err != nil {
		return err
	}
	if err := r.ParseMultipartForm(MaxNewFileBodyBytes); err != nil && !errors.Is(err, http.ErrNotMultipart) {
		return err
	}
	return nil
}
