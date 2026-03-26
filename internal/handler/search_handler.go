package handler

import (
	"net/http"

	"github.com/mkappworks/cloudzilla/internal/middleware"
	"github.com/mkappworks/cloudzilla/internal/view"
	"github.com/mkappworks/cloudzilla/internal/view/pages"
)

func (h *Handler) PageSearch(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query().Get("q")
	searchType := r.URL.Query().Get("type")

	var userID *int64
	if claims, ok := middleware.ClaimsFromContext(r.Context()); ok {
		userID = &claims.UserID
	}

	data := view.SearchData{
		BasePage: basePage(r, h.Services),
		Query:    q,
		Type:     searchType,
	}

	if q != "" {
		results, _ := h.Services.Search.Search(r.Context(), q, searchType, userID)
		data.Results = results
	}

	h.render(w, r, pages.Search(data))
}
