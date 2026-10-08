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
	"github.com/go-git/go-git/v5/plumbing/filemode"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/mkappworks-dev/cloudzilla-app/internal/codeurl"
	"github.com/mkappworks-dev/cloudzilla-app/internal/middleware"
	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
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
	// maxEditFileBytes caps a file the browser editor opens or saves.
	maxEditFileBytes = 1 << 20
	// MaxEditFileBodyBytes caps the edit form's request body: a textarea sends
	// every line break as CRLF, which can double a file's bytes. CSRF parses a
	// browser's form under the global cap first, so SubmitEditFile also checks
	// Content-Length.
	MaxEditFileBodyBytes = 2*maxEditFileBytes + 1<<20
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
	ref, path := h.Services.Code.SplitRefPath(owner, repoName, routeRefPath(r))

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
	if err := service.CheckContentWritable(repo); err != nil {
		http.Error(w, err.Error(), http.StatusForbidden)
		return
	}
	if status, msg := h.storageRefusal(r, repo); status != 0 {
		http.Error(w, msg, status)
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
	content := []byte(textareaText(r.FormValue("content"), false))
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
		if errors.Is(err, service.ErrInvalidFilePath) || errors.Is(err, service.ErrFileUnchanged) {
			http.Error(w, err.Error(), http.StatusUnprocessableEntity)
			return
		}
		slog.Error("commit file failed", "owner", owner, "repo", repoName, "ref", ref, "path", path, "error", err)
		http.Error(w, err.Error(), http.StatusUnprocessableEntity)
		return
	}
	h.Services.Quota.Recompute(repo)
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

var editFileTooLargeMsg = fmt.Sprintf("files over %d MB can't be edited in the browser", maxEditFileBytes>>20)

// editableRepo loads a repo the signed-in viewer may commit to from the
// browser: 404 when they can't read it, 403 when they can't write to it or it
// is archived.
func (h *Handler) editableRepo(w http.ResponseWriter, r *http.Request, owner, repoName string, viewerID int64) (*model.Repository, bool) {
	repo, ok := h.readableRepo(w, r, owner, repoName, viewerID)
	if !ok {
		return nil, false
	}
	if !h.Services.Repo.CanWrite(r.Context(), repo, viewerID) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return nil, false
	}
	if err := service.CheckContentWritable(repo); err != nil {
		http.Error(w, err.Error(), http.StatusForbidden)
		return nil, false
	}
	return repo, true
}

// storageRefusal is the status and message for a web write to repo that its
// owner's storage quota refuses, or 0 when it may go ahead.
func (h *Handler) storageRefusal(r *http.Request, repo *model.Repository) (int, string) {
	err := h.Services.Quota.CheckStorage(r.Context(), repo)
	switch {
	case err == nil:
		return 0, ""
	case errors.Is(err, service.ErrQuotaReached):
		return http.StatusForbidden, err.Error()
	default:
		slog.Error("check storage quota", "repo_id", repo.ID, "error", err)
		return http.StatusInternalServerError, "failed to check the storage quota"
	}
}

// editRefusal says why the browser editor can't open f, or "" when it can.
func editRefusal(f *service.BranchFile) string {
	switch {
	case f.Mode == filemode.Symlink:
		return "symlinks can't be edited in the browser"
	case f.IsBinary:
		return "binary files can't be edited in the browser"
	case f.Size > maxEditFileBytes:
		return editFileTooLargeMsg
	case !service.IsEditableText(string(f.Content)):
		return "files that aren't UTF-8, or hold a carriage return outside a CRLF, can't be edited in the browser"
	}
	return ""
}

// branchFile reads the file an edit starts from, answering 404 for a ref that
// isn't a branch or a path that isn't a file. A nil file with ok set means the
// path holds no file now.
func (h *Handler) branchFile(w http.ResponseWriter, r *http.Request, owner, repoName, ref, path string) (file *service.BranchFile, ok bool) {
	if path == "" {
		h.NotFound(w, r)
		return nil, false
	}
	f, err := h.Services.Code.GetBranchFile(owner, repoName, ref, path, maxEditFileBytes)
	switch {
	case err == nil:
		return f, true
	case errors.Is(err, object.ErrFileNotFound):
		return nil, true
	case errors.Is(err, service.ErrRefNotFound) || errors.Is(err, service.ErrEmptyRepo):
		h.NotFound(w, r)
	default:
		slog.Error("edit file: read failed", "owner", owner, "repo", repoName, "ref", ref, "path", path, "error", err)
		http.Error(w, "failed to read file", http.StatusInternalServerError)
	}
	return nil, false
}

// PageEditFile renders the editor for a text file on a branch.
func (h *Handler) PageEditFile(w http.ResponseWriter, r *http.Request) {
	owner := chi.URLParam(r, "owner")
	repoName := chi.URLParam(r, "repo")
	claims, ok := middleware.ClaimsFromContext(r.Context())
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	repo, ok := h.editableRepo(w, r, owner, repoName, claims.UserID)
	if !ok {
		return
	}
	ref, path := h.Services.Code.SplitRefPath(owner, repoName, routeRefPath(r))
	file, ok := h.branchFile(w, r, owner, repoName, ref, path)
	if !ok {
		return
	}
	if file == nil {
		h.NotFound(w, r)
		return
	}
	if reason := editRefusal(file); reason != "" {
		http.Error(w, reason, http.StatusUnprocessableEntity)
		return
	}
	canManage := h.Services.Repo.CanManage(r.Context(), repo, claims.UserID)
	h.render(w, r, pages.EditFile(view.EditFileData{
		BasePage: h.withRepoSubnav(r.Context(), basePage(r, h.Services), repo, "code", canManage),
		Owner:    owner,
		RepoName: repoName,
		Ref:      ref,
		Path:     path,
		BlobSHA:  file.SHA,
		NewPath:  path,
		Content:  string(file.Content),
	}))
}

// SubmitEditFile commits an edit, and with a new path a rename, of a file on a
// branch, refusing it when the file changed after the editor loaded it.
func (h *Handler) SubmitEditFile(w http.ResponseWriter, r *http.Request) {
	owner := chi.URLParam(r, "owner")
	repoName := chi.URLParam(r, "repo")
	claims, ok := middleware.ClaimsFromContext(r.Context())
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	repo, ok := h.editableRepo(w, r, owner, repoName, claims.UserID)
	if !ok {
		return
	}
	ref, path := h.Services.Code.SplitRefPath(owner, repoName, routeRefPath(r))
	if r.ContentLength > MaxEditFileBodyBytes {
		http.Error(w, editFileTooLargeMsg, http.StatusRequestEntityTooLarge)
		return
	}
	if err := parseNewFileForm(r); err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			http.Error(w, editFileTooLargeMsg, http.StatusRequestEntityTooLarge)
			return
		}
		http.Error(w, "invalid form", http.StatusBadRequest)
		return
	}
	file, ok := h.branchFile(w, r, owner, repoName, ref, path)
	if !ok {
		return
	}

	canManage := h.Services.Repo.CanManage(r.Context(), repo, claims.UserID)
	data := view.EditFileData{
		BasePage: h.withRepoSubnav(r.Context(), basePage(r, h.Services), repo, "code", canManage),
		Owner:    owner,
		RepoName: repoName,
		Ref:      ref,
		Path:     path,
		BlobSHA:  r.FormValue("blob_sha"),
		NewPath:  r.FormValue("path"),
		Content:  textareaText(r.FormValue("content"), false),
		Message:  r.FormValue("message"),
	}
	refuse := func(status int, msg string) {
		data.Error = msg
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(status)
		if err := pages.EditFile(data).Render(r.Context(), w); err != nil {
			slog.Error("render failed", "error", err)
		}
	}
	if status, msg := h.storageRefusal(r, repo); status != 0 {
		refuse(status, msg)
		return
	}
	conflict := func() {
		data.ConflictURL = codeurl.Path(owner, repoName, "blob", ref, path)
		if file == nil {
			data.ConflictURL = codeurl.Path(owner, repoName, "tree", ref, parentDir(path))
		}
		refuse(http.StatusConflict, "This file changed on "+ref+" after you opened it, so your changes weren't committed.")
	}

	if file == nil || file.SHA != data.BlobSHA {
		conflict()
		return
	}
	if reason := editRefusal(file); reason != "" {
		http.Error(w, reason, http.StatusUnprocessableEntity)
		return
	}
	content := textareaText(r.FormValue("content"), allCRLF(string(file.Content)))
	if len(content) > maxEditFileBytes {
		refuse(http.StatusRequestEntityTooLarge, editFileTooLargeMsg)
		return
	}
	newPath := path
	if data.NewPath != path {
		var err error
		if newPath, err = service.CleanFilePath(strings.TrimSpace(data.NewPath)); err != nil {
			refuse(http.StatusUnprocessableEntity, err.Error())
			return
		}
	}
	message := strings.TrimSpace(data.Message)
	if message == "" {
		switch {
		case newPath == path:
			message = "Update " + path
		case content == string(file.Content):
			message = "Rename " + path + " to " + newPath
		default:
			message = "Update and rename " + path + " to " + newPath
		}
	}
	author, err := h.Services.User.CommitAuthor(r.Context(), claims.UserID)
	if err != nil {
		http.Error(w, "failed to load user", http.StatusInternalServerError)
		return
	}

	err = h.Services.Code.EditFile(owner, repoName, ref, path, newPath, data.BlobSHA, []byte(content), author, message)
	switch {
	case err == nil:
		h.Services.Quota.Recompute(repo)
		http.Redirect(w, r, codeurl.Path(owner, repoName, "blob", ref, newPath), http.StatusSeeOther)
	case errors.Is(err, service.ErrRefNotFound):
		h.NotFound(w, r)
	case errors.Is(err, service.ErrFileChanged):
		conflict()
	case errors.Is(err, service.ErrRefMoved):
		refuse(http.StatusConflict, branchMovedMsg)
	case errors.Is(err, service.ErrPathCollision):
		refuse(http.StatusConflict, err.Error())
	case errors.Is(err, service.ErrInvalidFilePath):
		refuse(http.StatusUnprocessableEntity, err.Error())
	case errors.Is(err, service.ErrFileUnchanged):
		refuse(http.StatusUnprocessableEntity, "Nothing to commit: the path and contents are unchanged.")
	default:
		slog.Error("edit file failed", "owner", owner, "repo", repoName, "ref", ref, "path", path, "error", err)
		http.Error(w, "failed to commit the file", http.StatusInternalServerError)
	}
}

// DeleteFile handles POST /{owner}/{repo}/delete/{ref}/* from the blob page's
// delete dialog: JSON errors become toasts, and success redirects to the
// nearest folder left on the branch.
func (h *Handler) DeleteFile(w http.ResponseWriter, r *http.Request) {
	owner := chi.URLParam(r, "owner")
	repoName := chi.URLParam(r, "repo")
	claims, ok := middleware.ClaimsFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	repo, ok := h.contentWritableRepoJSON(w, r, owner, repoName, claims.UserID)
	if !ok {
		return
	}
	ref, path := h.Services.Code.SplitRefPath(owner, repoName, routeRefPath(r))
	if path == "" {
		writeError(w, http.StatusNotFound, "file not found")
		return
	}
	if err := parseNewFileForm(r); err != nil {
		writeError(w, http.StatusBadRequest, "invalid form")
		return
	}
	message := strings.TrimSpace(r.FormValue("message"))
	if message == "" {
		message = "Delete " + path
	}
	author, err := h.Services.User.CommitAuthor(r.Context(), claims.UserID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load user")
		return
	}

	dir, err := h.Services.Code.DeleteFile(owner, repoName, ref, path, r.FormValue("blob_sha"), author, message)
	switch {
	case err == nil:
		h.Services.Quota.Recompute(repo)
		redirectAfterSave(w, r, codeurl.Path(owner, repoName, "tree", ref, dir))
	case errors.Is(err, service.ErrRefNotFound) || errors.Is(err, service.ErrEmptyRepo):
		writeError(w, http.StatusNotFound, "branch not found")
	case errors.Is(err, object.ErrFileNotFound):
		writeError(w, http.StatusNotFound, "file not found")
	case errors.Is(err, service.ErrFileChanged):
		writeError(w, http.StatusConflict, "This file changed on "+ref+" after the page loaded; reload to see the current version.")
	case errors.Is(err, service.ErrRefMoved):
		writeError(w, http.StatusConflict, branchMovedMsg)
	default:
		slog.Error("delete file failed", "owner", owner, "repo", repoName, "ref", ref, "path", path, "error", err)
		writeError(w, http.StatusInternalServerError, "failed to delete the file")
	}
}

// parentDir is path's folder, "" at the root.
func parentDir(path string) string {
	if i := strings.LastIndex(path, "/"); i >= 0 {
		return path[:i]
	}
	return ""
}
