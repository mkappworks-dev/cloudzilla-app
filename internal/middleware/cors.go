package middleware

import (
	"net/http"

	chiCors "github.com/go-chi/cors"
)

func CORS(devMode bool) func(http.Handler) http.Handler {
	origins := []string{"http://localhost:3000"}
	if !devMode {
		origins = []string{"*"}
	}
	return chiCors.Handler(chiCors.Options{
		AllowedOrigins:   origins,
		AllowedMethods:   []string{"GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"},
		AllowedHeaders:   []string{"Accept", "Authorization", "Content-Type", "HX-Request", "HX-Target"},
		AllowCredentials: true,
		MaxAge:           300,
	})
}
