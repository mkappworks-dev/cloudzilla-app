package handler

import (
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/a-h/templ"
	"github.com/mkappworks-dev/cloudzilla-app/internal/config"
	"github.com/mkappworks-dev/cloudzilla-app/internal/middleware"
	"github.com/mkappworks-dev/cloudzilla-app/internal/service"
)

// Handler holds all application services and configuration needed by HTTP handlers.
type Handler struct {
	Services *service.Services
	Cfg      *config.Config
}

// New creates a Handler wiring the given services and configuration.
func New(services *service.Services, cfg *config.Config) *Handler {
	return &Handler{Services: services, Cfg: cfg}
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

func (h *Handler) render(w http.ResponseWriter, r *http.Request, component templ.Component) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := component.Render(r.Context(), w); err != nil {
		slog.Error("render failed", "error", err)
		http.Error(w, "internal server error", http.StatusInternalServerError)
	}
}

// viewerCanReadRepo looks up the repo by owner/name and returns true if the
// viewer (resolved from the request context, anonymous if no claims) has read
// access. Returns false when the repo does not exist or is not visible.
func (h *Handler) viewerCanReadRepo(r *http.Request, owner, repoName string) bool {
	repo, err := h.Services.Repo.Get(r.Context(), owner, repoName)
	if err != nil {
		return false
	}
	var viewerID *int64
	if claims, ok := middleware.ClaimsFromContext(r.Context()); ok {
		viewerID = &claims.UserID
	}
	return h.Services.Repo.CanRead(r.Context(), repo, viewerID)
}
