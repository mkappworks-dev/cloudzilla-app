package handler

import (
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/mkappworks-dev/cloudzilla-app/internal/middleware"
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
	h.finishPackageChange(w, repo, err)
}

// DeleteGenericVersion handles DELETE .../packages/generic/{name}/{version}.
func (h *Handler) DeleteGenericVersion(w http.ResponseWriter, r *http.Request) {
	repo, _, ok := h.writablePackageRepo(w, r)
	if !ok {
		return
	}
	err := h.Services.Package.DeleteGenericVersion(r.Context(), repo.ID, chi.URLParam(r, "name"), chi.URLParam(r, "version"))
	h.finishPackageChange(w, repo, err)
}

// packageFormRequest is a Packages-page form's repository, package and poster.
type packageFormRequest struct {
	Repo   *model.Repository
	Pkg    *model.PackageSummary
	UserID int64
}

// ownerPackageRequest is an owner Packages tab form's package and poster.
type ownerPackageRequest struct {
	Pkg    *model.PackageSummary
	UserID int64
}

// packageForm resolves the package a Packages-page form names, which must be linked to the repository in the URL
// and readable by the poster; anything else is a 404. What the poster may do with it is the caller's rule.
func (h *Handler) packageForm(w http.ResponseWriter, r *http.Request) (packageFormRequest, bool) {
	claims, ok := middleware.ClaimsFromContext(r.Context())
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return packageFormRequest{}, false
	}
	repo, ok := h.readableRepoPage(w, r, chi.URLParam(r, "owner"), chi.URLParam(r, "repo"))
	if !ok {
		return packageFormRequest{}, false
	}
	id, err := strconv.ParseInt(r.FormValue("package_id"), 10, 64)
	if err != nil {
		h.NotFound(w, r)
		return packageFormRequest{}, false
	}
	p, err := h.Services.Package.InRepoByID(r.Context(), repo, &claims.UserID, id)
	if err != nil {
		if errors.Is(err, service.ErrPackageNotFound) {
			h.NotFound(w, r)
		} else {
			slog.Error("package form: find failed", "repo_id", repo.ID, "error", err)
			h.ServerError(w, r)
		}
		return packageFormRequest{}, false
	}
	return packageFormRequest{Repo: repo, Pkg: p, UserID: claims.UserID}, true
}

// packageFormError answers a refused or failed change with the message the page shows as a toast.
func packageFormError(w http.ResponseWriter, r *http.Request, err error) {
	var regErr *service.RegistryError
	switch {
	case errors.As(err, &regErr):
		settingsError(w, r, regErr.Status, regErr.Message)
	case errors.Is(err, service.ErrPackageQuota):
		settingsError(w, r, http.StatusForbidden, err.Error())
	case errors.Is(err, service.ErrPackageForbidden):
		settingsError(w, r, http.StatusForbidden, "You can't change this package.")
	case errors.Is(err, service.ErrRelinkTarget):
		settingsError(w, r, http.StatusUnprocessableEntity, "A package can only be linked to a repository of its path owner.")
	case errors.Is(err, service.ErrPackageNotFound):
		settingsError(w, r, http.StatusNotFound, "package not found")
	default:
		slog.Error("package form failed", "error", err)
		settingsError(w, r, http.StatusInternalServerError, "internal server error")
	}
}

// DeletePackageTag handles POST /{owner}/{repo}/packages/tags/delete: a tag, or an untagged manifest by digest.
func (h *Handler) DeletePackageTag(w http.ResponseWriter, r *http.Request) {
	form, ok := h.packageForm(w, r)
	if !ok {
		return
	}
	if !h.Services.Repo.CanWrite(r.Context(), form.Repo, form.UserID) {
		settingsError(w, r, http.StatusForbidden, "You can't change this package.")
		return
	}
	if err := h.Services.Registry.DeleteManifest(r.Context(), form.UserID, form.Pkg.OwnerName+"/"+form.Pkg.Name, r.FormValue("ref")); err != nil {
		packageFormError(w, r, err)
		return
	}
	redirectAfterSave(w, r, packageURL(form.Repo, form.Pkg.Type, form.Pkg.Name))
}

// DeletePackageVersion handles POST /{owner}/{repo}/packages/versions/delete. The package goes with its last
// version, so the redirect goes to the list when it is gone.
func (h *Handler) DeletePackageVersion(w http.ResponseWriter, r *http.Request) {
	form, ok := h.packageForm(w, r)
	if !ok {
		return
	}
	if !h.Services.Repo.CanWrite(r.Context(), form.Repo, form.UserID) {
		settingsError(w, r, http.StatusForbidden, "You can't change this package.")
		return
	}
	if err := h.Services.Package.DeleteGenericVersion(r.Context(), form.Repo.ID, form.Pkg.Name, r.FormValue("version")); err != nil {
		packageFormError(w, r, err)
		return
	}
	to := packageURL(form.Repo, form.Pkg.Type, form.Pkg.Name)
	if _, err := h.Services.Package.InRepoByID(r.Context(), form.Repo, &form.UserID, form.Pkg.ID); errors.Is(err, service.ErrPackageNotFound) {
		to = "/" + form.Repo.OwnerName + "/" + form.Repo.Name + "/packages"
	}
	redirectAfterSave(w, r, to)
}

// DeletePackage handles POST /{owner}/{repo}/packages/delete; the whole package needs its name typed.
func (h *Handler) DeletePackage(w http.ResponseWriter, r *http.Request) {
	form, ok := h.packageForm(w, r)
	if !ok {
		return
	}
	if !h.Services.Package.CanDelete(r.Context(), &form.Pkg.Package, form.Repo, form.UserID) {
		settingsError(w, r, http.StatusForbidden, "You can't delete this package.")
		return
	}
	if r.FormValue("confirm_name") != form.Pkg.Name {
		settingsError(w, r, http.StatusUnprocessableEntity, "Type the package name exactly as shown.")
		return
	}
	if err := h.Services.Package.Delete(r.Context(), form.UserID, form.Pkg.ID); err != nil {
		packageFormError(w, r, err)
		return
	}
	redirectAfterSave(w, r, "/"+form.Repo.OwnerName+"/"+form.Repo.Name+"/packages")
}

// RelinkPackageForm handles POST /{owner}/{repo}/packages/relink; repo names a Repository of the path owner.
func (h *Handler) RelinkPackageForm(w http.ResponseWriter, r *http.Request) {
	form, ok := h.packageForm(w, r)
	if !ok {
		return
	}
	if !h.Services.Package.CanDelete(r.Context(), &form.Pkg.Package, form.Repo, form.UserID) {
		settingsError(w, r, http.StatusForbidden, "You can't re-link this package.")
		return
	}
	target, err := h.Services.Repo.Get(r.Context(), form.Pkg.OwnerName, r.FormValue("repo"))
	if err != nil {
		packageFormError(w, r, service.ErrRelinkTarget)
		return
	}
	if err := h.Services.Package.Relink(r.Context(), form.UserID, form.Pkg.ID, target.ID); err != nil {
		packageFormError(w, r, err)
		return
	}
	redirectAfterSave(w, r, packageURL(target, form.Pkg.Type, form.Pkg.Name))
}

// ownerPackageForm resolves the package an owner Packages tab form names. It must sit under the path in the URL and be
// visible to the poster, else 404; only the path owner may change it, else 403.
func (h *Handler) ownerPackageForm(w http.ResponseWriter, r *http.Request) (ownerPackageRequest, bool) {
	claims, ok := middleware.ClaimsFromContext(r.Context())
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return ownerPackageRequest{}, false
	}
	id, err := strconv.ParseInt(r.FormValue("package_id"), 10, 64)
	if err != nil {
		h.NotFound(w, r)
		return ownerPackageRequest{}, false
	}
	p, err := h.Services.Package.InOwner(r.Context(), chi.URLParam(r, "owner"), &claims.UserID, id)
	if errors.Is(err, service.ErrPackageNotFound) {
		h.NotFound(w, r)
		return ownerPackageRequest{}, false
	}
	if err != nil {
		slog.Error("owner package form: find failed", "package_id", id, "error", err)
		h.ServerError(w, r)
		return ownerPackageRequest{}, false
	}
	if !h.Services.Package.IsPathOwner(r.Context(), p.OwnerName, claims.UserID) {
		settingsError(w, r, http.StatusForbidden, "Only the owner of "+p.OwnerName+" can change its packages here.")
		return ownerPackageRequest{}, false
	}
	return ownerPackageRequest{Pkg: p, UserID: claims.UserID}, true
}

// DeleteOwnerPackage handles POST /{owner}/-/packages/delete; the whole package needs its name typed.
func (h *Handler) DeleteOwnerPackage(w http.ResponseWriter, r *http.Request) {
	form, ok := h.ownerPackageForm(w, r)
	if !ok {
		return
	}
	if r.FormValue("confirm_name") != form.Pkg.Name {
		settingsError(w, r, http.StatusUnprocessableEntity, "Type the package name exactly as shown.")
		return
	}
	if err := h.Services.Package.Delete(r.Context(), form.UserID, form.Pkg.ID); err != nil {
		packageFormError(w, r, err)
		return
	}
	redirectAfterSave(w, r, "/"+form.Pkg.OwnerName+"?tab=packages")
}

// RelinkOwnerPackage handles POST /{owner}/-/packages/relink; repo names a Repository of the path owner.
func (h *Handler) RelinkOwnerPackage(w http.ResponseWriter, r *http.Request) {
	form, ok := h.ownerPackageForm(w, r)
	if !ok {
		return
	}
	target, err := h.Services.Repo.Get(r.Context(), form.Pkg.OwnerName, r.FormValue("repo"))
	if err != nil {
		packageFormError(w, r, service.ErrRelinkTarget)
		return
	}
	if err := h.Services.Package.Relink(r.Context(), form.UserID, form.Pkg.ID, target.ID); err != nil {
		packageFormError(w, r, err)
		return
	}
	redirectAfterSave(w, r, "/"+form.Pkg.OwnerName+"?tab=packages")
}
