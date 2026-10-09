package handler

import (
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/mkappworks-dev/cloudzilla-app/internal/middleware"
	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/service"
	"github.com/mkappworks-dev/cloudzilla-app/internal/view/components"
)

// model.User carries the email; this is all anyone else may see.
type publicUser struct {
	ID        int64     `json:"id"`
	Username  string    `json:"username"`
	Bio       string    `json:"bio"`
	AvatarURL string    `json:"avatar_url"`
	CreatedAt time.Time `json:"created_at"`
}

func newPublicUser(u model.User, baseURL string) publicUser {
	return publicUser{
		ID:        u.ID,
		Username:  u.Username,
		Bio:       u.Bio,
		AvatarURL: apiAvatarURL(baseURL, u.AvatarKey, u.AvatarURL),
		CreatedAt: u.CreatedAt,
	}
}

// apiAvatarURL is the absolute URL of an uploaded avatar, or the stored
// avatar_url, such as a Google picture, when none was uploaded.
func apiAvatarURL(baseURL, key, stored string) string {
	if key == "" {
		return stored
	}
	return strings.TrimRight(baseURL, "/") + components.AvatarURL(key)
}

func (h *Handler) GetUser(w http.ResponseWriter, r *http.Request) {
	username := chi.URLParam(r, "username")
	user, err := h.Services.User.GetByUsername(r.Context(), username)
	if err != nil {
		writeError(w, http.StatusNotFound, "user not found")
		return
	}
	writeJSON(w, http.StatusOK, newPublicUser(*user, h.Cfg.Server.BaseURL))
}

func (h *Handler) GetCurrentUser(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.ClaimsFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	// Claims can carry a username from before a rename, so read it fresh.
	user, err := h.Services.User.GetByID(r.Context(), claims.UserID)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	writeJSON(w, http.StatusOK, struct {
		ID       int64  `json:"id"`
		Username string `json:"username"`
	}{user.ID, user.Username})
}

func (h *Handler) PinRepo(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.ClaimsFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	userID, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid user id")
		return
	}
	if claims.UserID != userID {
		writeError(w, http.StatusForbidden, "forbidden")
		return
	}
	repoID, err := strconv.ParseInt(chi.URLParam(r, "repoID"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid repo id")
		return
	}
	if err := h.Services.User.PinRepo(r.Context(), userID, repoID); err != nil {
		if errors.Is(err, service.ErrRepoNotFound) {
			writeError(w, http.StatusNotFound, "repository not found")
			return
		}
		if errors.Is(err, service.ErrPinLimit) {
			writeError(w, http.StatusUnprocessableEntity, "pin limit reached")
			return
		}
		slog.Error("pin repo failed", "error", err)
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (h *Handler) UnpinRepo(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.ClaimsFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	userID, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid user id")
		return
	}
	if claims.UserID != userID {
		writeError(w, http.StatusForbidden, "forbidden")
		return
	}
	repoID, err := strconv.ParseInt(chi.URLParam(r, "repoID"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid repo id")
		return
	}
	if err := h.Services.User.UnpinRepo(r.Context(), userID, repoID); err != nil {
		slog.Error("unpin repo failed", "error", err)
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}
