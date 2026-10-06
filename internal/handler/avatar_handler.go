package handler

import (
	"errors"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"path"
	"regexp"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/mkappworks-dev/cloudzilla-app/internal/avatar"
	"github.com/mkappworks-dev/cloudzilla-app/internal/middleware"
	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/service"
	"github.com/mkappworks-dev/cloudzilla-app/internal/storage"
	"github.com/mkappworks-dev/cloudzilla-app/internal/view"
	"github.com/mkappworks-dev/cloudzilla-app/internal/view/components"
)

// AvatarBodyBytes caps an avatar upload: the image plus multipart overhead.
const AvatarBodyBytes = avatar.MaxUploadBytes + 64<<10

const (
	avatarFormError    = "#avatar-form-error"
	orgAvatarFormError = "#org-avatar-form-error"
)

var avatarKeyRe = regexp.MustCompile(`^avatars/(user|org)/([0-9]{1,19})/([0-9a-f]{64})\.(png|jpg)$`)

// UploadUserAvatar handles POST /settings/avatar.
func (h *Handler) UploadUserAvatar(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.ClaimsFromContext(r.Context())
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	file, err := avatarFile(r)
	if err == nil {
		defer func() { _ = file.Close() }()
		_, err = h.Services.Avatar.SetUserAvatar(r.Context(), claims.UserID, file)
	}
	if err != nil {
		avatarError(w, r, avatarFormError, err)
		return
	}
	redirectAfterSave(w, r, "/settings")
}

// RemoveUserAvatar handles POST /settings/avatar/delete.
func (h *Handler) RemoveUserAvatar(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.ClaimsFromContext(r.Context())
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	if err := h.Services.Avatar.RemoveUserAvatar(r.Context(), claims.UserID); err != nil {
		avatarError(w, r, avatarFormError, err)
		return
	}
	redirectAfterSave(w, r, "/settings")
}

// UploadOrgAvatar handles POST /orgs/{org}/settings/avatar.
func (h *Handler) UploadOrgAvatar(w http.ResponseWriter, r *http.Request) {
	claims, org, ok := h.orgAvatarTarget(w, r)
	if !ok {
		return
	}
	file, err := avatarFile(r)
	if err == nil {
		defer func() { _ = file.Close() }()
		_, err = h.Services.Avatar.SetOrgAvatar(r.Context(), org.ID, claims.UserID, file)
	}
	if err != nil {
		avatarError(w, r, orgAvatarFormError, err)
		return
	}
	h.Services.AuditLog.Record(r.Context(), r, claims.UserID, claims.Username,
		model.AuditActionOrgAvatarUpdate, model.AuditTargetOrg, org.ID, org.Name, nil)
	redirectAfterSave(w, r, "/orgs/"+org.Name+"/settings")
}

// RemoveOrgAvatar handles POST /orgs/{org}/settings/avatar/delete.
func (h *Handler) RemoveOrgAvatar(w http.ResponseWriter, r *http.Request) {
	claims, org, ok := h.orgAvatarTarget(w, r)
	if !ok {
		return
	}
	if err := h.Services.Avatar.RemoveOrgAvatar(r.Context(), org.ID, claims.UserID); err != nil {
		avatarError(w, r, orgAvatarFormError, err)
		return
	}
	h.Services.AuditLog.Record(r.Context(), r, claims.UserID, claims.Username,
		model.AuditActionOrgAvatarRemove, model.AuditTargetOrg, org.ID, org.Name, nil)
	redirectAfterSave(w, r, "/orgs/"+org.Name+"/settings")
}

func (h *Handler) orgAvatarTarget(w http.ResponseWriter, r *http.Request) (middleware.Claims, *model.Organization, bool) {
	claims, ok := middleware.ClaimsFromContext(r.Context())
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return middleware.Claims{}, nil, false
	}
	org, err := h.Services.Org.Get(r.Context(), chi.URLParam(r, "org"))
	if err != nil {
		h.NotFound(w, r)
		return middleware.Claims{}, nil, false
	}
	return claims, org, true
}

var errNoAvatarFile = errors.New("no avatar file")

// avatarFile reads the "avatar" part. CSRF has already parsed the form when
// the request carried no X-CSRF-Token, in which case FormFile reuses it.
func avatarFile(r *http.Request) (multipart.File, error) {
	if r.MultipartForm == nil {
		if err := r.ParseMultipartForm(AvatarBodyBytes); err != nil {
			return nil, err
		}
	}
	file, _, err := r.FormFile("avatar")
	if errors.Is(err, http.ErrMissingFile) {
		return nil, errNoAvatarFile
	}
	return file, err
}

// avatarError answers htmx with the message in the form's error slot, as the
// other settings forms do; other clients get the status.
func avatarError(w http.ResponseWriter, r *http.Request, slot string, err error) {
	status, msg := avatarErrorStatus(err)
	if status >= http.StatusInternalServerError {
		slog.Error("avatar upload failed", "error", err)
	}
	if r.Header.Get("HX-Request") == "true" {
		renderFormError(w, slot, msg)
		return
	}
	http.Error(w, msg, status)
}

func avatarErrorStatus(err error) (int, string) {
	var tooBig *http.MaxBytesError
	switch {
	case errors.As(err, &tooBig), errors.Is(err, avatar.ErrTooLarge), errors.Is(err, multipart.ErrMessageTooLarge):
		return http.StatusRequestEntityTooLarge, "Images can be at most 2 MB."
	case errors.Is(err, avatar.ErrUnsupportedType), errors.Is(err, avatar.ErrInvalidImage):
		return http.StatusUnprocessableEntity, "Use a PNG, JPEG, GIF or WebP image."
	case errors.Is(err, avatar.ErrDimensions):
		return http.StatusUnprocessableEntity, "Images can be at most 4096 × 4096 pixels."
	case errors.Is(err, errNoAvatarFile), errors.Is(err, http.ErrNotMultipart), errors.Is(err, http.ErrMissingBoundary):
		return http.StatusUnprocessableEntity, "Choose an image to upload."
	case errors.Is(err, service.ErrNotOrgOwner):
		return http.StatusForbidden, "Only organization owners can change its picture."
	default:
		return http.StatusInternalServerError, "Couldn't save the picture. Please try again."
	}
}

// ServeAvatar handles GET /avatars/*. A key names its content by hash, so the
// response never changes and can be cached for a year.
func (h *Handler) ServeAvatar(w http.ResponseWriter, r *http.Request) {
	key := "avatars/" + chi.URLParam(r, "*")
	m := avatarKeyRe.FindStringSubmatch(key)
	if m == nil {
		http.NotFound(w, r)
		return
	}
	etag := `"` + m[3] + `"`
	setAvatarHeaders(w, etag)
	if etagMatches(r.Header.Get("If-None-Match"), etag) {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	backend := h.Services.Avatar.Backend()
	if backend == nil {
		clearAvatarCache(w)
		http.Error(w, "storage unavailable", http.StatusServiceUnavailable)
		return
	}
	// The row check keeps made-up keys, which this route doesn't rate-limit,
	// from each costing a backend read.
	id, _ := strconv.ParseInt(m[2], 10, 64)
	inUse, err := h.Services.Avatar.KeyInUse(r.Context(), m[1], id, key)
	if err != nil {
		slog.Error("avatar: check key", "key", key, "error", err)
		clearAvatarCache(w)
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}
	if !inUse {
		clearAvatarCache(w)
		http.NotFound(w, r)
		return
	}
	body, err := backend.Get(r.Context(), key)
	if errors.Is(err, storage.ErrNotFound) {
		clearAvatarCache(w)
		http.NotFound(w, r)
		return
	}
	if err != nil {
		slog.Error("avatar: read object", "key", key, "error", err)
		clearAvatarCache(w)
		http.Error(w, "storage unavailable", http.StatusBadGateway)
		return
	}
	defer func() { _ = body.Close() }()
	if path.Ext(key) == ".png" {
		w.Header().Set("Content-Type", "image/png")
	} else {
		w.Header().Set("Content-Type", "image/jpeg")
	}
	if _, err := io.Copy(w, body); err != nil {
		slog.Warn("avatar: stream object", "key", key, "error", err)
	}
}

func setAvatarHeaders(w http.ResponseWriter, etag string) {
	h := w.Header()
	h.Set("Cache-Control", "public, max-age=31536000, immutable")
	h.Set("ETag", etag)
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("Content-Security-Policy", "default-src 'none'; sandbox")
}

// A missing object may be restored later under the same key, so its 404 must
// not be cached for a year.
func clearAvatarCache(w http.ResponseWriter) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Del("ETag")
}

func etagMatches(header, etag string) bool {
	for candidate := range strings.SplitSeq(header, ",") {
		candidate = strings.TrimPrefix(strings.TrimSpace(candidate), "W/")
		if candidate == etag || candidate == "*" {
			return true
		}
	}
	return false
}

// withAvatars looks up the avatars of the named users and orgs, and of the
// viewer, in one query and puts them on the request context, where
// components.Avatar finds them.
func (h *Handler) withAvatars(r *http.Request, names ...string) *http.Request {
	if claims, ok := middleware.ClaimsFromContext(r.Context()); ok {
		names = append(names, claims.Username)
	}
	if len(names) == 0 {
		return r
	}
	keys := h.Services.Avatar.KeysByOwnerName(r.Context(), names...)
	return r.WithContext(components.WithAvatarKeys(r.Context(), keys))
}

// withKnownAvatars is withAvatars for keys the handler already loaded.
func withKnownAvatars(r *http.Request, keys map[string]string) *http.Request {
	return r.WithContext(components.WithAvatarKeys(r.Context(), keys))
}

func memberNames(members []model.OrgMember) []string {
	names := make([]string, len(members))
	for i, m := range members {
		names[i] = m.Username
	}
	return names
}

func userAvatarKeys(users []model.User) map[string]string {
	keys := make(map[string]string, len(users))
	for _, u := range users {
		keys[u.Username] = u.AvatarKey
	}
	return keys
}

func commentAuthors(comments []view.RenderedComment) []string {
	names := make([]string, len(comments))
	for i, c := range comments {
		names[i] = c.AuthorName
	}
	return names
}

func usernames(users []model.User) []string {
	names := make([]string, len(users))
	for i, u := range users {
		names[i] = u.Username
	}
	return names
}

func contributorNames(rows []service.ContributorWithTimeline) []string {
	names := make([]string, len(rows))
	for i, c := range rows {
		names[i] = c.Username
	}
	return names
}
