package handler

import (
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/service"
	"github.com/mkappworks-dev/cloudzilla-app/internal/storage"
)

// GenericUploadBodyOverhead is slack on the file cap for a body that isn't exactly the file.
const GenericUploadBodyOverhead = 1 << 10

type genericFileJSON struct {
	Filename    string    `json:"filename"`
	Size        int64     `json:"size"`
	SHA256      string    `json:"sha256"`
	DownloadURL string    `json:"download_url"`
	CreatedAt   time.Time `json:"created_at"`
}

type genericVersionJSON struct {
	Version   string            `json:"version"`
	CreatedAt time.Time         `json:"created_at"`
	Files     []genericFileJSON `json:"files"`
}

type genericPackageJSON struct {
	Name          string    `json:"name"`
	Type          string    `json:"type"`
	LatestVersion string    `json:"latest_version"`
	VersionCount  int       `json:"version_count"`
	TagCount      int       `json:"tag_count"`
	Size          int64     `json:"size"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
}

func genericFileURL(owner, repo, name, version, filename string) string {
	return "/" + owner + "/" + repo + "/packages/generic/" + name + "/" + version + "/" + filename
}

func genericFileView(repo *model.Repository, name, version string, f model.PackageFile) genericFileJSON {
	return genericFileJSON{
		Filename:    f.Filename,
		Size:        f.Size,
		SHA256:      f.SHA256,
		DownloadURL: genericFileURL(repo.OwnerName, repo.Name, name, version, f.Filename),
		CreatedAt:   f.CreatedAt,
	}
}

// packageCaller resolves who is asking: nil for an anonymous request. A Basic PAT is checked against the route's
// scope here because auth never sees one; a PAT without it gets a plain 403, as for git. ok is false when the
// response is written.
func (h *Handler) packageCaller(w http.ResponseWriter, r *http.Request) (*gitUser, bool) {
	gu, err := h.resolveGitUser(r)
	if err != nil {
		writeError(w, http.StatusForbidden, err.Error())
		return nil, false
	}
	return gu, true
}

func requirePackageCaller(w http.ResponseWriter, gu *gitUser) bool {
	if gu != nil {
		return true
	}
	w.Header().Set("WWW-Authenticate", `Basic realm="packages"`)
	writeError(w, http.StatusUnauthorized, "authentication required")
	return false
}

func (h *Handler) packageRepo(w http.ResponseWriter, r *http.Request, gu *gitUser) (*model.Repository, bool) {
	var viewer *int64
	if gu != nil {
		viewer = &gu.ID
	}
	repo, err := h.Services.Repo.Get(r.Context(), chi.URLParam(r, "owner"), chi.URLParam(r, "repo"))
	if err != nil || !h.Services.Repo.CanRead(r.Context(), repo, viewer) {
		writeError(w, http.StatusNotFound, "repo not found")
		return nil, false
	}
	return repo, true
}

// writablePackageRepo answers who may change a repository's packages: 401 to nobody, 404 to someone who can't read
// the repository, 403 to a reader.
func (h *Handler) writablePackageRepo(w http.ResponseWriter, r *http.Request) (*model.Repository, *gitUser, bool) {
	gu, ok := h.packageCaller(w, r)
	if !ok || !requirePackageCaller(w, gu) {
		return nil, nil, false
	}
	repo, ok := h.packageRepo(w, r, gu)
	if !ok {
		return nil, nil, false
	}
	if !h.Services.Repo.CanWrite(r.Context(), repo, gu.ID) {
		writeError(w, http.StatusForbidden, "forbidden")
		return nil, nil, false
	}
	return repo, gu, true
}

// UploadGenericFile handles PUT /api/repos/{owner}/{repo}/packages/generic/{name}/{version}/{filename}; the body
// is the file.
func (h *Handler) UploadGenericFile(w http.ResponseWriter, r *http.Request) {
	name, version, filename := chi.URLParam(r, "name"), chi.URLParam(r, "version"), chi.URLParam(r, "filename")
	if !service.ValidGenericSegment(name) || !service.ValidGenericSegment(version) || !service.ValidGenericSegment(filename) {
		writeError(w, http.StatusBadRequest, "A name, version or filename may only contain letters, digits, '.', '_' and '-', and can't be '.' or '..'.")
		return
	}
	repo, gu, ok := h.writablePackageRepo(w, r)
	if !ok {
		return
	}
	if max := h.Services.Package.MaxFileBytes(); r.ContentLength > max {
		writeError(w, http.StatusRequestEntityTooLarge, "Files can be at most "+byteSize(max)+".")
		return
	}
	extendDeadlines(w)

	f, err := h.Services.Package.UploadGenericFile(r.Context(), repo, gu.ID, name, version, filename, r.Body)
	if err != nil {
		status, msg := h.packageErrorStatus(err)
		if status >= http.StatusInternalServerError {
			slog.Error("generic package upload failed", "repo_id", repo.ID, "error", err)
		}
		writeError(w, status, msg)
		return
	}
	writeJSON(w, http.StatusCreated, genericFileView(repo, name, version, *f))
}

func (h *Handler) packageErrorStatus(err error) (int, string) {
	var tooBig *http.MaxBytesError
	switch {
	case errors.As(err, &tooBig), errors.Is(err, service.ErrPackageFileTooBig):
		return http.StatusRequestEntityTooLarge, "Files can be at most " + byteSize(h.Services.Package.MaxFileBytes()) + "."
	case errors.Is(err, service.ErrPackageQuota):
		return http.StatusForbidden, err.Error()
	case errors.Is(err, service.ErrPackageName):
		return http.StatusBadRequest, "A name, version or filename may only contain letters, digits, '.', '_' and '-', and can't be '.' or '..'."
	case errors.Is(err, service.ErrPackageNameTaken):
		return http.StatusConflict, "This package name is already used by another repository of the owner."
	case errors.Is(err, service.ErrPackageFileExists):
		return http.StatusConflict, "This file already exists in the version; published files are never replaced."
	case errors.Is(err, service.ErrPackageNotFound):
		return http.StatusNotFound, "package not found"
	case errors.Is(err, service.ErrStorageUnconfigured):
		return http.StatusServiceUnavailable, "File uploads are unavailable."
	default:
		return http.StatusInternalServerError, "Couldn't save the file. Please try again."
	}
}

// DownloadGenericFile handles GET and HEAD /{owner}/{repo}/packages/generic/{name}/{version}/{filename}. Read
// access is checked on every request and every refusal, an unreadable repo included, is the same 404, so a private
// repo's files can't be told from missing ones.
func (h *Handler) DownloadGenericFile(w http.ResponseWriter, r *http.Request) {
	gu, err := h.resolveGitUser(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusForbidden)
		return
	}
	var viewer *int64
	if gu != nil {
		viewer = &gu.ID
	}
	repo, err := h.Services.Repo.Get(r.Context(), chi.URLParam(r, "owner"), chi.URLParam(r, "repo"))
	if err != nil || !h.Services.Repo.CanRead(r.Context(), repo, viewer) {
		http.NotFound(w, r)
		return
	}
	f, err := h.Services.Package.GenericFile(r.Context(), repo.ID, chi.URLParam(r, "name"), chi.URLParam(r, "version"), chi.URLParam(r, "filename"))
	if errors.Is(err, service.ErrPackageNotFound) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		slog.Error("generic package: look up file", "repo_id", repo.ID, "error", err)
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}

	etag := `"` + f.SHA256 + `"`
	hd := w.Header()
	hd.Set("Cache-Control", "private, no-cache")
	hd.Set("Vary", "Authorization, Cookie")
	hd.Set("ETag", etag)
	hd.Set("X-Content-Type-Options", "nosniff")
	hd.Set("Content-Security-Policy", "default-src 'none'; sandbox")
	if etagMatches(r.Header.Get("If-None-Match"), etag) {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	body, err := h.Services.Package.OpenFile(r.Context(), f)
	if errors.Is(err, storage.ErrNotFound) {
		hd.Del("ETag")
		http.NotFound(w, r)
		return
	}
	if err != nil {
		slog.Error("generic package: read object", "file_id", f.ID, "error", err)
		hd.Del("ETag")
		http.Error(w, "storage unavailable", http.StatusBadGateway)
		return
	}
	defer func() { _ = body.Close() }()
	extendDeadlines(w)
	hd.Set("Content-Type", "application/octet-stream")
	hd.Set("Content-Disposition", `attachment; filename="`+f.Filename+`"`)
	hd.Set("Content-Length", strconv.FormatInt(f.Size, 10))
	if r.Method == http.MethodHead {
		return
	}
	if _, err := io.Copy(w, body); err != nil {
		slog.Warn("generic package: stream object", "file_id", f.ID, "error", err)
	}
}

// ListPackages handles GET /api/repos/{owner}/{repo}/packages.
func (h *Handler) ListPackages(w http.ResponseWriter, r *http.Request) {
	gu, ok := h.packageCaller(w, r)
	if !ok {
		return
	}
	repo, ok := h.packageRepo(w, r, gu)
	if !ok {
		return
	}
	var viewer *int64
	if gu != nil {
		viewer = &gu.ID
	}
	list, err := h.Services.Package.ListByRepo(r.Context(), repo, viewer)
	if err != nil {
		slog.Error("list packages failed", "repo_id", repo.ID, "error", err)
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}
	out := make([]genericPackageJSON, len(list))
	for i, p := range list {
		out[i] = genericPackageJSON{Name: p.Name, Type: p.Type, LatestVersion: p.LatestVersion,
			VersionCount: p.VersionCount, TagCount: p.TagCount, Size: p.Size, CreatedAt: p.CreatedAt, UpdatedAt: p.UpdatedAt}
	}
	writeJSON(w, http.StatusOK, out)
}

// GetGenericPackage handles GET /api/repos/{owner}/{repo}/packages/generic/{name}.
func (h *Handler) GetGenericPackage(w http.ResponseWriter, r *http.Request) {
	gu, ok := h.packageCaller(w, r)
	if !ok {
		return
	}
	repo, ok := h.packageRepo(w, r, gu)
	if !ok {
		return
	}
	name := chi.URLParam(r, "name")
	p, versions, err := h.Services.Package.GenericVersions(r.Context(), repo.ID, name)
	if errors.Is(err, service.ErrPackageNotFound) {
		writeError(w, http.StatusNotFound, "package not found")
		return
	}
	if err != nil {
		slog.Error("get generic package failed", "repo_id", repo.ID, "error", err)
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}
	vs := make([]genericVersionJSON, len(versions))
	for i, v := range versions {
		vs[i] = genericVersionJSON{Version: v.Version, CreatedAt: v.CreatedAt, Files: make([]genericFileJSON, len(v.Files))}
		for j, f := range v.Files {
			vs[i].Files[j] = genericFileView(repo, name, v.Version, f)
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"name": p.Name, "type": p.Type, "created_at": p.CreatedAt, "updated_at": p.UpdatedAt, "versions": vs,
	})
}

// DeleteGenericFile handles DELETE .../packages/generic/{name}/{version}/{filename}.
func (h *Handler) DeleteGenericFile(w http.ResponseWriter, r *http.Request) {
	repo, _, ok := h.writablePackageRepo(w, r)
	if !ok {
		return
	}
	err := h.Services.Package.DeleteGenericFile(r.Context(), repo.ID, chi.URLParam(r, "name"), chi.URLParam(r, "version"), chi.URLParam(r, "filename"))
	h.finishPackageDelete(w, repo, err)
}

// DeleteGenericVersion handles DELETE .../packages/generic/{name}/{version}.
func (h *Handler) DeleteGenericVersion(w http.ResponseWriter, r *http.Request) {
	repo, _, ok := h.writablePackageRepo(w, r)
	if !ok {
		return
	}
	err := h.Services.Package.DeleteGenericVersion(r.Context(), repo.ID, chi.URLParam(r, "name"), chi.URLParam(r, "version"))
	h.finishPackageDelete(w, repo, err)
}

func (h *Handler) finishPackageDelete(w http.ResponseWriter, repo *model.Repository, err error) {
	switch {
	case errors.Is(err, service.ErrPackageNotFound):
		writeError(w, http.StatusNotFound, "package not found")
	case err != nil:
		slog.Error("delete generic package failed", "repo_id", repo.ID, "error", err)
		writeError(w, http.StatusInternalServerError, "internal server error")
	default:
		w.WriteHeader(http.StatusNoContent)
	}
}
