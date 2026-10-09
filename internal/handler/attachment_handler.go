package handler

import (
	"errors"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"path"
	"regexp"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/mkappworks-dev/cloudzilla-app/internal/attachment"
	"github.com/mkappworks-dev/cloudzilla-app/internal/middleware"
	"github.com/mkappworks-dev/cloudzilla-app/internal/service"
	"github.com/mkappworks-dev/cloudzilla-app/internal/storage"
)

// AttachmentBodyBytes caps an attachment upload: the image plus multipart overhead.
const AttachmentBodyBytes = attachment.MaxBytes + 64<<10

var attachmentNameRe = regexp.MustCompile(`^([0-9a-f]{32})\.(png|jpg|gif|webp)$`)

// UploadAttachment handles POST /{owner}/{repo}/attachments with multipart
// field "file" and answers with the markdown to insert. Anyone who can read
// the repo may attach, which is also who may comment on it.
func (h *Handler) UploadAttachment(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.ClaimsFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	repo, ok := h.readableRepoJSON(w, r, chi.URLParam(r, "owner"), chi.URLParam(r, "repo"))
	if !ok {
		return
	}
	file, header, err := attachmentFile(r)
	if err == nil {
		defer func() { _ = file.Close() }()
		var a *attachmentUpload
		a, err = h.storeAttachment(r, repo.ID, claims.UserID, file, header.Filename)
		if err == nil {
			writeJSON(w, http.StatusOK, a)
			return
		}
	}
	status, msg := attachmentErrorStatus(err)
	if status >= http.StatusInternalServerError {
		slog.Error("attachment upload failed", "error", err)
	}
	writeError(w, status, msg)
}

type attachmentUpload struct {
	URL      string `json:"url"`
	Markdown string `json:"markdown"`
}

func (h *Handler) storeAttachment(r *http.Request, repoID, userID int64, file multipart.File, filename string) (*attachmentUpload, error) {
	a, err := h.Services.Attachment.Upload(r.Context(), repoID, userID, file)
	if err != nil {
		return nil, err
	}
	return &attachmentUpload{URL: a.URL(), Markdown: "![" + attachmentAlt(filename) + "](" + a.URL() + ")"}, nil
}

var errNoAttachmentFile = errors.New("no attachment file")

// attachmentFile reads the "file" part. CSRF has already parsed the form when
// the request carried no X-CSRF-Token, in which case FormFile reuses it.
func attachmentFile(r *http.Request) (multipart.File, *multipart.FileHeader, error) {
	if r.MultipartForm == nil {
		if err := r.ParseMultipartForm(AttachmentBodyBytes); err != nil {
			return nil, nil, err
		}
	}
	file, header, err := r.FormFile("file")
	if errors.Is(err, http.ErrMissingFile) {
		return nil, nil, errNoAttachmentFile
	}
	return file, header, err
}

// attachmentAlt turns a filename into alt text that can't break the markdown
// around it: no extension, and only letters, digits, spaces and . _ -.
func attachmentAlt(filename string) string {
	name := strings.TrimSuffix(path.Base(strings.ReplaceAll(filename, `\`, "/")), path.Ext(filename))
	var b strings.Builder
	for _, c := range name {
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9', c == ' ', c == '.', c == '_', c == '-':
			b.WriteRune(c)
		}
	}
	if alt := strings.TrimSpace(b.String()); alt != "" {
		return alt
	}
	return "image"
}

func attachmentErrorStatus(err error) (int, string) {
	var tooBig *http.MaxBytesError
	switch {
	case errors.As(err, &tooBig), errors.Is(err, attachment.ErrTooLarge), errors.Is(err, multipart.ErrMessageTooLarge):
		return http.StatusRequestEntityTooLarge, "Images can be at most 10 MB."
	case errors.Is(err, attachment.ErrUnsupportedType), errors.Is(err, attachment.ErrInvalidImage):
		return http.StatusUnprocessableEntity, "Use a PNG, JPEG, GIF or WebP image."
	case errors.Is(err, attachment.ErrDimensions):
		return http.StatusUnprocessableEntity, "Images can be at most 16384 × 16384 pixels."
	case errors.Is(err, errNoAttachmentFile), errors.Is(err, http.ErrNotMultipart), errors.Is(err, http.ErrMissingBoundary):
		return http.StatusUnprocessableEntity, "Choose an image to upload."
	case errors.Is(err, service.ErrStorageUnconfigured):
		return http.StatusServiceUnavailable, "Image uploads are unavailable."
	default:
		return http.StatusInternalServerError, "Couldn't save the image. Please try again."
	}
}

// ServeAttachment handles GET and HEAD /attachments/{name}. The name is
// unguessable but only names the image: the repo's CanRead decides, on every
// request, so a repo made private stops serving at once. Nothing is cached
// without revalidation for that reason, and every miss, including an
// unreadable repo, is the same 404.
func (h *Handler) ServeAttachment(w http.ResponseWriter, r *http.Request) {
	m := attachmentNameRe.FindStringSubmatch(chi.URLParam(r, "name"))
	if m == nil {
		http.NotFound(w, r)
		return
	}
	a, err := h.Services.Attachment.Get(r.Context(), m[1])
	if errors.Is(err, service.ErrAttachmentNotFound) || (err == nil && path.Ext(a.StorageKey) != "."+m[2]) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		slog.Error("attachment: look up", "token", m[1], "error", err)
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}
	repo, err := h.Services.Repo.GetByID(r.Context(), a.RepoID)
	if err != nil || !h.Services.Repo.CanRead(r.Context(), repo, viewerOf(r)) {
		http.NotFound(w, r)
		return
	}
	hd := w.Header()
	hd.Set("Cache-Control", "private, no-cache")
	hd.Set("Vary", "Authorization, Cookie")
	hd.Set("ETag", `"`+a.Token+`"`)
	hd.Set("X-Content-Type-Options", "nosniff")
	hd.Set("Content-Security-Policy", "default-src 'none'; sandbox")
	if etagMatches(r.Header.Get("If-None-Match"), `"`+a.Token+`"`) {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	body, err := h.Services.Attachment.Open(r.Context(), a)
	if errors.Is(err, storage.ErrNotFound) {
		hd.Del("ETag")
		http.NotFound(w, r)
		return
	}
	if err != nil {
		slog.Error("attachment: read object", "token", a.Token, "error", err)
		hd.Del("ETag")
		http.Error(w, "storage unavailable", http.StatusBadGateway)
		return
	}
	defer func() { _ = body.Close() }()
	hd.Set("Content-Type", a.ContentType)
	if r.Method == http.MethodHead {
		return
	}
	if _, err := io.Copy(w, body); err != nil {
		slog.Warn("attachment: stream object", "token", a.Token, "error", err)
	}
}
