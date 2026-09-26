package handler

import (
	"net/http"
	"net/url"

	"github.com/go-chi/chi/v5"
)

// MovedPermanently 301s to path#fragment, carrying the request's query along.
func MovedPermanently(path, fragment string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		target := url.URL{Path: path, RawQuery: r.URL.Query().Encode(), Fragment: fragment}
		http.Redirect(w, r, target.String(), http.StatusMovedPermanently)
	}
}

// MovedToProfileTab 301s /{owner}/<tab> to /{owner}?tab=<tab>, carrying the request's query along.
func MovedToProfileTab(tab string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		q.Set("tab", tab)
		target := url.URL{Path: "/" + chi.URLParam(r, "owner"), RawQuery: q.Encode()}
		http.Redirect(w, r, target.String(), http.StatusMovedPermanently)
	}
}
