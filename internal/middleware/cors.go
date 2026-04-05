package middleware

import (
	"net/http"
	"net/url"

	chiCors "github.com/go-chi/cors"
)

// CORS returns middleware that restricts cross-origin requests to the
// configured base URL origin. In development (localhost), the Tailwind
// dev server origin is also permitted.
func CORS(baseURL string) func(http.Handler) http.Handler {
	parsed, _ := url.Parse(baseURL)
	origin := parsed.Scheme + "://" + parsed.Host

	origins := []string{origin}
	if parsed.Hostname() == "localhost" || parsed.Hostname() == "127.0.0.1" {
		origins = append(origins, "http://localhost:3000")
	}

	return chiCors.Handler(chiCors.Options{
		AllowedOrigins:   origins,
		AllowedMethods:   []string{"GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"},
		AllowedHeaders:   []string{"Accept", "Authorization", "Content-Type", "HX-Request", "HX-Target", "X-CSRF-Token"},
		AllowCredentials: true,
		MaxAge:           300,
	})
}
