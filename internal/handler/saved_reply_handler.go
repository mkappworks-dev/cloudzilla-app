package handler

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/mkappworks/cloudzilla/internal/middleware"
	"github.com/mkappworks/cloudzilla/internal/view"
	"github.com/mkappworks/cloudzilla/internal/view/fragments"
	"github.com/mkappworks/cloudzilla/internal/view/pages"
)

// PageSavedReplies renders GET /settings/replies
func (h *Handler) PageSavedReplies(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.ClaimsFromContext(r.Context())
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	replies, _ := h.Services.SavedReply.List(r.Context(), claims.UserID)
	h.render(w, r, pages.SavedReplies(view.SavedRepliesData{
		BasePage: basePage(r, h.Services),
		Replies:  replies,
	}))
}

// CreateSavedReply handles POST /api/user/replies
func (h *Handler) CreateSavedReply(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.ClaimsFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	var title, body string
	if r.Header.Get("HX-Request") == "true" {
		r.ParseForm()
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
		r.ParseForm()
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
		if err.Error() == "forbidden" {
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
