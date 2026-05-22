package handler

import (
	"errors"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/mkappworks-dev/cloudzilla-app/internal/middleware"
	"github.com/mkappworks-dev/cloudzilla-app/internal/service"
)

func (h *Handler) GetUser(w http.ResponseWriter, r *http.Request) {
	username := chi.URLParam(r, "username")
	user, err := h.Services.User.GetByUsername(r.Context(), username)
	if err != nil {
		writeError(w, http.StatusNotFound, "user not found")
		return
	}
	writeJSON(w, http.StatusOK, user)
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
	repo, err := h.Services.Repo.GetByID(r.Context(), repoID)
	if err != nil {
		writeError(w, http.StatusNotFound, "repository not found")
		return
	}
	if !h.Services.Repo.CanRead(r.Context(), repo, &claims.UserID) {
		writeError(w, http.StatusNotFound, "repository not found")
		return
	}
	if err := h.Services.User.PinRepo(r.Context(), userID, repoID); err != nil {
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
