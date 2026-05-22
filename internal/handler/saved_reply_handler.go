package handler

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/mkappworks-dev/cloudzilla-app/internal/middleware"
	"github.com/mkappworks-dev/cloudzilla-app/internal/service"
	"github.com/mkappworks-dev/cloudzilla-app/internal/view"
	"github.com/mkappworks-dev/cloudzilla-app/internal/view/fragments"
)

// CreateSavedReply handles POST /api/user/replies
func (h *Handler) CreateSavedReply(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.ClaimsFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	var title, body string
	if r.Header.Get("HX-Request") == "true" {
		if err := r.ParseForm(); err != nil {
			writeError(w, http.StatusBadRequest, "invalid form data")
			return
		}
		title = r.FormValue("title")
		body = r.FormValue("body")
	} else {
		var req struct {
			Title string `json:"title"`
			Body  string `json:"body"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid request body")
			return
		}
		title = req.Title
		body = req.Body
	}

	_, err := h.Services.SavedReply.Create(r.Context(), claims.UserID, title, body)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	if r.Header.Get("HX-Request") == "true" {
		replies, _ := h.Services.SavedReply.List(r.Context(), claims.UserID)
		h.render(w, r, fragments.SavedRepliesList(view.SavedRepliesFragData{Replies: replies}))
		return
	}
	w.WriteHeader(http.StatusCreated)
}

// UpdateSavedReply handles PATCH /api/user/replies/{id}
func (h *Handler) UpdateSavedReply(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.ClaimsFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}

	var title, body string
	if r.Header.Get("HX-Request") == "true" {
		if err := r.ParseForm(); err != nil {
			writeError(w, http.StatusBadRequest, "invalid form data")
			return
		}
		title = r.FormValue("title")
		body = r.FormValue("body")
	} else {
		var req struct {
			Title string `json:"title"`
			Body  string `json:"body"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid request body")
			return
		}
		title = req.Title
		body = req.Body
	}

	_, err = h.Services.SavedReply.Update(r.Context(), id, claims.UserID, title, body)
	if err != nil {
		if errors.Is(err, service.ErrForbidden) {
			writeError(w, http.StatusForbidden, "forbidden")
			return
		}
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	if r.Header.Get("HX-Request") == "true" {
		replies, _ := h.Services.SavedReply.List(r.Context(), claims.UserID)
		h.render(w, r, fragments.SavedRepliesList(view.SavedRepliesFragData{Replies: replies}))
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// DeleteSavedReply handles DELETE /api/user/replies/{id}
func (h *Handler) DeleteSavedReply(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.ClaimsFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}

	if err := h.Services.SavedReply.Delete(r.Context(), id, claims.UserID); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to delete")
		return
	}

	if r.Header.Get("HX-Request") == "true" {
		replies, _ := h.Services.SavedReply.List(r.Context(), claims.UserID)
		h.render(w, r, fragments.SavedRepliesList(view.SavedRepliesFragData{Replies: replies}))
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ListSavedRepliesFragment handles GET /api/user/replies — returns picker fragment for HTMX
func (h *Handler) ListSavedRepliesFragment(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.ClaimsFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	replies, _ := h.Services.SavedReply.List(r.Context(), claims.UserID)
	if r.Header.Get("HX-Request") == "true" {
		h.render(w, r, fragments.SavedRepliesPicker(view.SavedRepliesPickerFragData{Replies: replies}))
		return
	}
	writeJSON(w, http.StatusOK, replies)
}
