package handler

import (
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/a-h/templ"
	"github.com/mkappworks-dev/cloudzilla-app/internal/config"
	"github.com/mkappworks-dev/cloudzilla-app/internal/middleware"
	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
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

// Must be called before the response body — sets an HTTP header.
func toast(w http.ResponseWriter, toastType, message string) {
	payload, err := json.Marshal(map[string]any{
		"toast": map[string]string{"type": toastType, "message": message},
	})
	if err != nil {
		return
	}
	w.Header().Set("HX-Trigger", string(payload))
}

func (h *Handler) render(w http.ResponseWriter, r *http.Request, component templ.Component) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := component.Render(r.Context(), w); err != nil {
		slog.Error("render failed", "error", err)
		http.Error(w, "internal server error", http.StatusInternalServerError)
	}
}

// readableRepoJSON is readableRepo for the JSON API, where the caller may be
// anonymous. It must run before anything that answers differently for a repo
// that exists (a 403, a sub-resource 404, body validation), or that answer
// would confirm a private repo exists.
func (h *Handler) readableRepoJSON(w http.ResponseWriter, r *http.Request, owner, repoName string) (*model.Repository, bool) {
	var viewerID *int64
	if claims, ok := middleware.ClaimsFromContext(r.Context()); ok {
		viewerID = &claims.UserID
	}
	repo, err := h.Services.Repo.Get(r.Context(), owner, repoName)
	if err != nil || !h.Services.Repo.CanRead(r.Context(), repo, viewerID) {
		writeError(w, http.StatusNotFound, "repo not found")
		return nil, false
	}
	return repo, true
}
