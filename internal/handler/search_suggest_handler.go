package handler

import (
	"net/http"
	"regexp"

	"github.com/mkappworks-dev/cloudzilla-app/internal/middleware"
	"github.com/mkappworks-dev/cloudzilla-app/internal/view"
	"github.com/mkappworks-dev/cloudzilla-app/internal/view/fragments"
)

// suggestListID is the id of the element a suggestion list lives in; it prefixes the option ids,
// so two lists on one page (topnav and results page) never share an id.
var suggestListID = regexp.MustCompile(`^[a-z][a-z0-9-]{0,40}$`)

func (h *Handler) SearchSuggest(w http.ResponseWriter, r *http.Request) {
	var userID *int64
	if claims, ok := middleware.ClaimsFromContext(r.Context()); ok {
		userID = &claims.UserID
	}

	listID := r.URL.Query().Get("list")
	if !suggestListID.MatchString(listID) {
		listID = "topnav-suggest"
	}

	s, _ := h.Services.Search.Suggest(r.Context(), r.URL.Query().Get("q"), userID)

	// Users and orgs share a namespace, so one map serves both avatars.
	keys := userAvatarKeys(s.Users)
	for _, o := range s.Orgs {
		keys[o.Name] = o.AvatarKey
	}

	// The same URL answers differently per viewer; a shared cache must not replay it.
	w.Header().Set("Cache-Control", "private, no-store")
	h.render(w, withKnownAvatars(r, keys), fragments.SearchSuggestions(view.SearchSuggestionsData{Suggestions: s, ListID: listID}))
}
