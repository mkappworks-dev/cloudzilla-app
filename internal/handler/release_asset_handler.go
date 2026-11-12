package handler

import (
	"errors"
	"fmt"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/mkappworks-dev/cloudzilla-app/internal/middleware"
	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/service"
	"github.com/mkappworks-dev/cloudzilla-app/internal/storage"
)

// ReleaseAssetBodyOverhead is what the multipart framing may add to the file.
const ReleaseAssetBodyOverhead = 1 << 20

var errNoReleaseAssetFile = errors.New("no release asset file")

// releaseAssetURL is the path a release asset downloads from. url.PathEscape
// keeps a tag containing "/" in one segment.
func releaseAssetURL(owner, repoName, tag, name string) string {
	return "/" + owner + "/" + repoName + "/releases/download/" + url.PathEscape(tag) + "/" + url.PathEscape(name)
}

func releaseAssetsWithURLs(owner, repoName, tag string, list []model.ReleaseAsset) []model.ReleaseAsset {
	out := make([]model.ReleaseAsset, len(list))
	for i, a := range list {
		a.DownloadURL = releaseAssetURL(owner, repoName, tag, a.Name)
		out[i] = a
	}
	return out
}

// UploadReleaseAsset handles POST /api/repos/{owner}/{repo}/releases/{id}/assets
// with one multipart field "file". The body is read as a stream, not parsed
// into a form, so a large file never sits in memory.
func (h *Handler) UploadReleaseAsset(w http.ResponseWriter, r *http.Request) {
	_, _, release, _, ok := h.releaseWriteContext(w, r)
	if !ok {
		return
	}
	claims, _ := middleware.ClaimsFromContext(r.Context())
	extendDeadlines(w)

	filename, file, err := releaseAssetPart(r)
	if err == nil {
		var a *model.ReleaseAsset
		a, err = h.Services.ReleaseAsset.Upload(r.Context(), release.RepoID, release.ID, claims.UserID, filename, file)
		if err == nil {
			a.DownloadURL = releaseAssetURL(chi.URLParam(r, "owner"), chi.URLParam(r, "repo"), release.TagName, a.Name)
			writeJSON(w, http.StatusCreated, a)
			return
		}
	}
	status, msg := h.releaseAssetErrorStatus(err, filename)
	if status >= http.StatusInternalServerError {
		slog.Error("release asset upload failed", "release_id", release.ID, "error", err)
	}
	writeError(w, status, msg)
}

func releaseAssetPart(r *http.Request) (string, io.Reader, error) {
	mr, err := r.MultipartReader()
	if err != nil {
		return "", nil, err
	}
	for {
		part, err := mr.NextPart()
		if errors.Is(err, io.EOF) {
			return "", nil, errNoReleaseAssetFile
		}
		if err != nil {
			return "", nil, err
		}
		if part.FormName() == "file" && part.FileName() != "" {
			return part.FileName(), part, nil
		}
	}
}

func (h *Handler) releaseAssetErrorStatus(err error, filename string) (int, string) {
	var tooBig *http.MaxBytesError
	switch {
	case errors.As(err, &tooBig), errors.Is(err, service.ErrReleaseAssetTooLarge), errors.Is(err, multipart.ErrMessageTooLarge):
		return http.StatusRequestEntityTooLarge, "Assets can be at most " + byteSize(h.Services.ReleaseAsset.MaxBytes()) + "."
	case errors.Is(err, service.ErrReleaseAssetLimit):
		return http.StatusUnprocessableEntity, "A release can have at most " + strconv.Itoa(h.Cfg.Release.MaxAssets) + " assets."
	case errors.Is(err, service.ErrReleaseAssetNameTaken):
		name, _ := service.CleanReleaseAssetName(filename)
		return http.StatusConflict, "This release already has an asset named " + name + "."
	case errors.Is(err, service.ErrReleaseAssetName), errors.Is(err, errNoReleaseAssetFile),
		errors.Is(err, http.ErrNotMultipart), errors.Is(err, http.ErrMissingBoundary):
		return http.StatusUnprocessableEntity, "Choose a file to upload."
	case errors.Is(err, service.ErrStorageUnconfigured):
		return http.StatusServiceUnavailable, "File uploads are unavailable."
	default:
		return http.StatusInternalServerError, "Couldn't save the file. Please try again."
	}
}

func byteSize(n int64) string {
	switch {
	case n >= 1<<30 && n%(1<<30) == 0:
		return fmt.Sprintf("%d GiB", n>>30)
	case n >= 1<<20:
		return fmt.Sprintf("%d MiB", n>>20)
	case n >= 1<<10:
		return fmt.Sprintf("%d KiB", n>>10)
	}
	return fmt.Sprintf("%d bytes", n)
}

// extendDeadlines lifts the server's read and write timeouts, which are sized
// for pages and would cut off a large transfer. Servers without deadline
// support (test recorders) ignore the error.
func extendDeadlines(w http.ResponseWriter) {
	rc := http.NewResponseController(w)
	_ = rc.SetReadDeadline(time.Time{})
	_ = rc.SetWriteDeadline(time.Time{})
}

func (h *Handler) DeleteReleaseAsset(w http.ResponseWriter, r *http.Request) {
	_, _, release, _, ok := h.releaseWriteContext(w, r)
	if !ok {
		return
	}
	assetID, err := strconv.ParseInt(chi.URLParam(r, "assetID"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid asset id")
		return
	}
	a, err := h.Services.ReleaseAsset.Get(r.Context(), release.ID, assetID)
	if errors.Is(err, service.ErrReleaseAssetNotFound) {
		writeError(w, http.StatusNotFound, "asset not found")
		return
	}
	if err == nil {
		err = h.Services.ReleaseAsset.Delete(r.Context(), a)
	}
	if err != nil {
		slog.Error("delete release asset failed", "release_id", release.ID, "asset_id", assetID, "error", err)
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}
	if r.Header.Get("HX-Request") == "true" {
		w.Header().Set("HX-Refresh", "true")
		w.WriteHeader(http.StatusOK)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// DownloadReleaseAsset handles GET and HEAD /{owner}/{repo}/releases/download/{tag}/{name}.
// Read access is checked on every request, and a draft's assets are for
// writers only. Every refusal, including an unreadable repo, is the same 404 so
// a private repo's assets can't be told from missing ones.
func (h *Handler) DownloadReleaseAsset(w http.ResponseWriter, r *http.Request) {
	repo, err := h.Services.Repo.Get(r.Context(), chi.URLParam(r, "owner"), chi.URLParam(r, "repo"))
	if err != nil || !h.Services.Repo.CanRead(r.Context(), repo, viewerOf(r)) {
		http.NotFound(w, r)
		return
	}
	release, err := h.Services.Release.GetByTag(r.Context(), repo.OwnerName, repo.Name, unescapeRouted(r, chi.URLParam(r, "tag")))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if release.IsDraft && !h.viewerCanWrite(r, repo) {
		http.NotFound(w, r)
		return
	}
	a, err := h.Services.ReleaseAsset.GetByName(r.Context(), release.ID, unescapeRouted(r, chi.URLParam(r, "name")))
	if errors.Is(err, service.ErrReleaseAssetNotFound) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		slog.Error("release asset: look up", "release_id", release.ID, "error", err)
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}

	etag := `"` + a.SHA256 + `"`
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
	body, err := h.Services.ReleaseAsset.Open(r.Context(), a)
	if errors.Is(err, storage.ErrNotFound) {
		hd.Del("ETag")
		http.NotFound(w, r)
		return
	}
	if err != nil {
		slog.Error("release asset: read object", "asset_id", a.ID, "error", err)
		hd.Del("ETag")
		http.Error(w, "storage unavailable", http.StatusBadGateway)
		return
	}
	defer func() { _ = body.Close() }()
	extendDeadlines(w)
	hd.Set("Content-Type", "application/octet-stream")
	hd.Set("Content-Disposition", `attachment; filename="`+a.Name+`"`)
	hd.Set("Content-Length", strconv.FormatInt(a.Size, 10))
	if r.Method == http.MethodHead {
		return
	}
	if _, err := io.Copy(w, body); err != nil {
		slog.Warn("release asset: stream object", "asset_id", a.ID, "error", err)
	}
}

func (h *Handler) viewerCanWrite(r *http.Request, repo *model.Repository) bool {
	claims, ok := middleware.ClaimsFromContext(r.Context())
	return ok && h.Services.Repo.CanWrite(r.Context(), repo, claims.UserID)
}
