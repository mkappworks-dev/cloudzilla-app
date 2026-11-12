package middleware

import (
	"mime"
	"net/http"
	"regexp"
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
			if isFormBody(r) && !IsStreamedUpload(r) {
				r.Body = http.MaxBytesReader(w, r.Body, maxBytes)
			}
			next.ServeHTTP(w, r)
		})
	}
}

var releaseAssetUploadPath = regexp.MustCompile(`^/api/repos/[^/]+/[^/]+/releases/[0-9]+/assets$`)

// IsStreamedUpload is a request whose handler reads the multipart body as a
// stream under its own cap. The global form cap and CSRF's form parse must
// leave it alone: the first would refuse a large file and the second would
// buffer it, for any caller, before authentication.
func IsStreamedUpload(r *http.Request) bool {
	return r.Method == http.MethodPost && releaseAssetUploadPath.MatchString(r.URL.Path)
}

// isFormBody ignores ParseMediaType's error as net/http does: it reads a form
// body even when the type comes back with ErrInvalidMediaParameter.
func isFormBody(r *http.Request) bool {
	mediaType, _, _ := mime.ParseMediaType(r.Header.Get("Content-Type"))
	return mediaType == "application/x-www-form-urlencoded" || mediaType == "multipart/form-data"
}
