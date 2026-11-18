package handler

import (
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/mkappworks-dev/cloudzilla-app/internal/middleware"
	"github.com/mkappworks-dev/cloudzilla-app/internal/service"
)

// registryBlobOp serves the blob and upload operations and reports false for any other.
func (h *Handler) registryBlobOp(w http.ResponseWriter, r *http.Request, route service.RegistryRoute) bool {
	switch route.Op {
	case service.RegistryOpBlob:
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			return false
		}
		h.registryGetBlob(w, r, route)
	case service.RegistryOpUploadStart:
		if r.Method != http.MethodPost {
			return false
		}
		h.registryStartUpload(w, r, route)
	case service.RegistryOpUpload:
		switch r.Method {
		case http.MethodPatch:
			h.registryPatchUpload(w, r, route)
		case http.MethodPut:
			h.registryCompleteUpload(w, r, route)
		case http.MethodGet:
			h.registryUploadStatus(w, r, route)
		case http.MethodDelete:
			h.registryCancelUpload(w, r, route)
		default:
			return false
		}
	default:
		return false
	}
	return true
}

func registryUserID(r *http.Request) int64 {
	if c, ok := middleware.RegistryClaimsFromContext(r.Context()); ok {
		return c.UserID
	}
	return 0
}

// liftDeadlines lets a transfer outlast the server's read and write timeouts, which suit a page, not a 5 GiB layer.
// A writer that has no deadlines to lift, such as a test recorder, is fine.
func liftDeadlines(w http.ResponseWriter) {
	rc := http.NewResponseController(w)
	_ = rc.SetReadDeadline(time.Time{})
	_ = rc.SetWriteDeadline(time.Time{})
}

func writeRegistryFailure(w http.ResponseWriter, err error) {
	var re *service.RegistryError
	if errors.As(err, &re) {
		for k, v := range re.Headers {
			w.Header().Set(k, v)
		}
		middleware.WriteRegistryError(w, re.Status, re.Code, re.Message, re.Detail)
		return
	}
	middleware.WriteRegistryError(w, http.StatusInternalServerError, "UNSUPPORTED", "registry error", nil)
}

func blobLocation(name, digest string) string { return "/v2/" + name + "/blobs/" + digest }

func uploadHeaders(w http.ResponseWriter, name string, st *service.UploadState) {
	w.Header().Set("Location", "/v2/"+name+"/blobs/uploads/"+st.ID)
	w.Header().Set("Docker-Upload-UUID", st.ID)
	w.Header().Set("Range", service.UploadRange(st.Bytes))
}

func (h *Handler) registryStartUpload(w http.ResponseWriter, r *http.Request, route service.RegistryRoute) {
	liftDeadlines(w)
	ctx, userID := r.Context(), registryUserID(r)
	q := r.URL.Query()
	if digest, from := q.Get("mount"), q.Get("from"); digest != "" && from != "" {
		mounted, err := h.Services.Registry.MountBlob(ctx, userID, route.Name, from, digest)
		if err != nil {
			writeRegistryFailure(w, err)
			return
		}
		if mounted {
			w.Header().Set("Location", blobLocation(route.Name, digest))
			w.Header().Set("Docker-Content-Digest", digest)
			w.WriteHeader(http.StatusCreated)
			return
		}
	}
	st, err := h.Services.Registry.StartUpload(ctx, userID, route.Name)
	if err != nil {
		writeRegistryFailure(w, err)
		return
	}
	if digest := q.Get("digest"); digest != "" {
		h.finishUpload(w, r, route, st.ID, digest)
		return
	}
	uploadHeaders(w, route.Name, st)
	w.WriteHeader(http.StatusAccepted)
}

func (h *Handler) registryPatchUpload(w http.ResponseWriter, r *http.Request, route service.RegistryRoute) {
	liftDeadlines(w)
	rng, ok := contentRange(w, r)
	if !ok {
		return
	}
	st, err := h.Services.Registry.AppendUpload(r.Context(), registryUserID(r), route.Name, route.Ref, r.Body, rng)
	if err != nil {
		writeRegistryFailure(w, err)
		return
	}
	uploadHeaders(w, route.Name, st)
	w.WriteHeader(http.StatusAccepted)
}

func (h *Handler) registryCompleteUpload(w http.ResponseWriter, r *http.Request, route service.RegistryRoute) {
	liftDeadlines(w)
	h.finishUpload(w, r, route, route.Ref, r.URL.Query().Get("digest"))
}

func (h *Handler) finishUpload(w http.ResponseWriter, r *http.Request, route service.RegistryRoute, id, digest string) {
	rng, ok := contentRange(w, r)
	if !ok {
		return
	}
	got, err := h.Services.Registry.CompleteUpload(r.Context(), registryUserID(r), route.Name, id, digest, r.Body, rng)
	if err != nil {
		writeRegistryFailure(w, err)
		return
	}
	w.Header().Set("Location", blobLocation(route.Name, got))
	w.Header().Set("Docker-Content-Digest", got)
	w.WriteHeader(http.StatusCreated)
}

func (h *Handler) registryUploadStatus(w http.ResponseWriter, r *http.Request, route service.RegistryRoute) {
	st, err := h.Services.Registry.UploadStatus(r.Context(), registryUserID(r), route.Name, route.Ref)
	if err != nil {
		writeRegistryFailure(w, err)
		return
	}
	uploadHeaders(w, route.Name, st)
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) registryCancelUpload(w http.ResponseWriter, r *http.Request, route service.RegistryRoute) {
	if err := h.Services.Registry.CancelUpload(r.Context(), registryUserID(r), route.Name, route.Ref); err != nil {
		writeRegistryFailure(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// contentRange reads the optional Content-Range of a chunk, "<start>-<end>" or "bytes <start>-<end>". It writes the
// 400 itself and reports false when the header is present but unreadable.
func contentRange(w http.ResponseWriter, r *http.Request) (*service.ContentRange, bool) {
	raw := strings.TrimSpace(strings.TrimPrefix(r.Header.Get("Content-Range"), "bytes "))
	if raw == "" {
		return nil, true
	}
	a, b, ok := strings.Cut(raw, "-")
	start, err1 := strconv.ParseInt(a, 10, 64)
	end, err2 := strconv.ParseInt(b, 10, 64)
	if !ok || err1 != nil || err2 != nil || start < 0 || end < start {
		middleware.WriteRegistryError(w, http.StatusBadRequest, "BLOB_UPLOAD_INVALID", "unreadable Content-Range", nil)
		return nil, false
	}
	return &service.ContentRange{Start: start, End: end}, true
}

func (h *Handler) registryGetBlob(w http.ResponseWriter, r *http.Request, route service.RegistryRoute) {
	liftDeadlines(w)
	ctx, userID := r.Context(), registryUserID(r)
	w.Header().Set("Docker-Content-Digest", route.Ref)
	// Read access is rechecked on every request, so no shared cache may keep a blob after a Repository goes private.
	w.Header().Set("Cache-Control", "private, no-cache")
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Accept-Ranges", "bytes")

	if r.Method == http.MethodHead {
		size, err := h.Services.Registry.StatBlob(ctx, userID, route.Name, route.Ref)
		if err != nil {
			writeRegistryFailure(w, err)
			return
		}
		w.Header().Set("Content-Length", strconv.FormatInt(size, 10))
		return
	}
	rc, size, err := h.Services.Registry.OpenBlob(ctx, userID, route.Name, route.Ref)
	if err != nil {
		writeRegistryFailure(w, err)
		return
	}
	defer func() { _ = rc.Close() }()

	start, end, partial := int64(0), size-1, false
	if spec := r.Header.Get("Range"); spec != "" {
		var ok bool
		if start, end, ok = parseByteRange(spec, size); !ok {
			w.Header().Set("Content-Range", "bytes */"+strconv.FormatInt(size, 10))
			middleware.WriteRegistryError(w, http.StatusRequestedRangeNotSatisfiable, "BLOB_UPLOAD_INVALID", "range not satisfiable", nil)
			return
		}
		partial = true
	}
	// Backend has no ranged read, so the skipped bytes are read and dropped.
	if _, err := io.CopyN(io.Discard, rc, start); err != nil {
		middleware.WriteRegistryError(w, http.StatusInternalServerError, "UNSUPPORTED", "could not read blob", nil)
		return
	}
	w.Header().Set("Content-Length", strconv.FormatInt(end-start+1, 10))
	if partial {
		w.Header().Set("Content-Range", "bytes "+strconv.FormatInt(start, 10)+"-"+strconv.FormatInt(end, 10)+"/"+strconv.FormatInt(size, 10))
		w.WriteHeader(http.StatusPartialContent)
	}
	_, _ = io.CopyN(w, rc, end-start+1)
}

// parseByteRange reads a single "bytes=a-b", "bytes=a-" or "bytes=-n" range against size.
func parseByteRange(spec string, size int64) (start, end int64, ok bool) {
	rest, found := strings.CutPrefix(spec, "bytes=")
	if !found || strings.Contains(rest, ",") {
		return 0, 0, false
	}
	a, b, found := strings.Cut(rest, "-")
	if !found {
		return 0, 0, false
	}
	if a == "" {
		n, err := strconv.ParseInt(b, 10, 64)
		if err != nil || n <= 0 || size == 0 {
			return 0, 0, false
		}
		return max(size-n, 0), size - 1, true
	}
	s, err := strconv.ParseInt(a, 10, 64)
	if err != nil || s < 0 || s >= size {
		return 0, 0, false
	}
	e := size - 1
	if b != "" {
		if e, err = strconv.ParseInt(b, 10, 64); err != nil || e < s {
			return 0, 0, false
		}
		e = min(e, size-1)
	}
	return s, e, true
}
