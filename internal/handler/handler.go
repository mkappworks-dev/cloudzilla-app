package handler

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"time"

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

func (h *Handler) setAuthCookie(w http.ResponseWriter, token string) {
	http.SetCookie(w, &http.Cookie{
		Name:     h.Cfg.Auth.CookieName,
		Value:    token,
		HttpOnly: true,
		Secure:   h.Cfg.Auth.CookieSecure,
		Path:     "/",
		Expires:  time.Now().Add(h.Cfg.Auth.JWTExpiry),
		SameSite: http.SameSiteLaxMode,
	})
}

// linkLookupOK reports whether a token link's lookup succeeded, writing the
// response itself when it didn't: the invalid-link page for an unusable link,
// a logged 500 for anything else.
func (h *Handler) linkLookupOK(w http.ResponseWriter, r *http.Request, err, unusable error, renderInvalid func(http.ResponseWriter, *http.Request), logPrefix string) bool {
	switch {
	case err == nil:
		return true
	case errors.Is(err, unusable):
		renderInvalid(w, r)
	default:
		slog.Error(logPrefix+": link lookup failed", "error", err)
		http.Error(w, "internal server error", http.StatusInternalServerError)
	}
	return false
}

// readableRepoJSON is readableRepo with a JSON 404, for API and fragment routes.
// It must run before anything that would answer an existing repo differently.
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

func (h *Handler) writableRepoJSON(w http.ResponseWriter, r *http.Request, owner, repoName string, userID int64) (*model.Repository, bool) {
	return h.permittedRepoJSON(w, r, owner, repoName, userID, h.Services.Repo.CanWrite)
}

func (h *Handler) manageableRepoJSON(w http.ResponseWriter, r *http.Request, owner, repoName string, userID int64) (*model.Repository, bool) {
	return h.permittedRepoJSON(w, r, owner, repoName, userID, h.Services.Repo.CanManage)
}

func (h *Handler) permittedRepoJSON(w http.ResponseWriter, r *http.Request, owner, repoName string, userID int64, permitted func(context.Context, *model.Repository, int64) bool) (*model.Repository, bool) {
	repo, ok := h.readableRepoJSON(w, r, owner, repoName)
	if !ok {
		return nil, false
	}
	if !permitted(r.Context(), repo, userID) {
		writeError(w, http.StatusForbidden, "forbidden")
		return nil, false
	}
	return repo, true
}
