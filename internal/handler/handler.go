package handler

import (
	"context"
	"encoding/json"
	"errors"
	"html"
	"log/slog"
	"mime"
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

// isFormEncoded reports whether r carries a URL-encoded form. fetch() sends the
// media type with ";charset=UTF-8" appended, so only the media type is compared.
func isFormEncoded(r *http.Request) bool {
	mediaType, _, _ := mime.ParseMediaType(r.Header.Get("Content-Type"))
	return mediaType == "application/x-www-form-urlencoded"
}

// isJSON compares only the media type: clients send it in any case and with parameters.
func isJSON(r *http.Request) bool {
	mediaType, _, _ := mime.ParseMediaType(r.Header.Get("Content-Type"))
	return mediaType == "application/json"
}

// renderFormError puts msg in slot, the error area of an HTMX form. It answers
// 200 because the layout's htmx config skips swaps on 4xx; the form and the
// data-toast listener tell success from error by HX-Retarget.
func renderFormError(w http.ResponseWriter, slot, msg string) {
	w.Header().Set("HX-Retarget", slot)
	w.Header().Set("HX-Reswap", "innerHTML")
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write([]byte(`<div class="rounded-md border border-destructive/30 bg-destructive/10 text-destructive text-xs p-3" role="alert">` + html.EscapeString(msg) + `</div>`))
}

// settingsError answers an HTMX settings form with JSON, which the layout shows
// as an error toast; a plain text body would only show the HTTP status.
func settingsError(w http.ResponseWriter, r *http.Request, status int, msg string) {
	if r.Header.Get("HX-Request") == "true" {
		writeError(w, status, msg)
		return
	}
	http.Error(w, msg, status)
}

// redirectAfterSave answers a saved settings form. Its data-toast stashes the
// message for the next page only when the response carries HX-Redirect.
func redirectAfterSave(w http.ResponseWriter, r *http.Request, to string) {
	if r.Header.Get("HX-Request") == "true" {
		w.Header().Set("HX-Redirect", to)
		w.WriteHeader(http.StatusNoContent)
		return
	}
	http.Redirect(w, r, to, http.StatusSeeOther)
}

const branchMovedMsg = "branch was updated while saving; reload and try again"

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
// a 500 logged as logMsg for anything else.
func (h *Handler) linkLookupOK(w http.ResponseWriter, r *http.Request, err, unusable error, renderInvalid func(http.ResponseWriter, *http.Request), logMsg string) bool {
	switch {
	case err == nil:
		return true
	case errors.Is(err, unusable):
		renderInvalid(w, r)
	default:
		slog.Error(logMsg, "error", err)
		http.Error(w, "internal server error", http.StatusInternalServerError)
	}
	return false
}

// viewerOf returns the signed-in user's ID, or nil for an anonymous request.
func viewerOf(r *http.Request) *int64 {
	if claims, ok := middleware.ClaimsFromContext(r.Context()); ok {
		return &claims.UserID
	}
	return nil
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

// contentWritableRepoJSON also refuses a repo whose git content is read-only.
func (h *Handler) contentWritableRepoJSON(w http.ResponseWriter, r *http.Request, owner, repoName string, userID int64) (*model.Repository, bool) {
	repo, ok := h.writableRepoJSON(w, r, owner, repoName, userID)
	if !ok {
		return nil, false
	}
	if err := service.CheckContentWritable(repo); err != nil {
		writeError(w, http.StatusForbidden, err.Error())
		return nil, false
	}
	return repo, true
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
