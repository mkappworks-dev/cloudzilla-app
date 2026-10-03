package middleware

import (
	"mime"
	"net/http"
)

// MaxBodySize limits the request body to the given number of bytes.
// Returns 413 Request Entity Too Large if the body exceeds the limit.
// MaxBodySize returns middleware that limits request bodies to maxBytes, returning 413 on excess.
func MaxBodySize(maxBytes int64) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			r.Body = http.MaxBytesReader(w, r.Body, maxBytes)
			next.ServeHTTP(w, r)
		})
	}
}

// MaxFormBodySize caps urlencoded and multipart bodies, the ones net/http's
// form parsing reads, at maxBytes. Other bodies, such as git pushes and JSON,
// pass through whole. net/http drops its own 10 MB cap on a urlencoded body it
// finds wrapped, so maxBytes is also how much of one a parse holds in memory.
func MaxFormBodySize(maxBytes int64) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if isFormBody(r) {
				r.Body = http.MaxBytesReader(w, r.Body, maxBytes)
			}
			next.ServeHTTP(w, r)
		})
	}
}

// isFormBody ignores ParseMediaType's error as net/http does: it reads a form
// body even when the type comes back with ErrInvalidMediaParameter.
func isFormBody(r *http.Request) bool {
	mediaType, _, _ := mime.ParseMediaType(r.Header.Get("Content-Type"))
	return mediaType == "application/x-www-form-urlencoded" || mediaType == "multipart/form-data"
}
