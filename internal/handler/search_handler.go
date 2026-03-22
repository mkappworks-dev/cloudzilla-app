package handler

import (
	"net/http"

	"github.com/mkappworks/cloudzilla/internal/middleware"
)

func (h *Handler) PageSearch(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query().Get("q")
	searchType := r.URL.Query().Get("type")

	var userID *int64
	if claims, ok := middleware.ClaimsFromContext(r.Context()); ok {
		userID = &claims.UserID
	}

	data := SearchData{
		BasePage: basePage(r, h.Services),
		Query:    q,
		Type:     searchType,
	}

	if q != "" {
		results, _ := h.Services.Search.Search(r.Context(), q, searchType, userID)
		data.Results = results
	}

	h.render(w, "search", data)
}
