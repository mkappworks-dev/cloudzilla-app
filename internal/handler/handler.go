package handler

import (
	"encoding/json"
	"html/template"
	"net/http"

	"github.com/mkappworks/cloudzilla/internal/config"
	"github.com/mkappworks/cloudzilla/internal/service"
)

type Handler struct {
	Services *service.Services
	Cfg      *config.Config
	Pages    map[string]*template.Template
	Frags    *template.Template
}

func New(services *service.Services, cfg *config.Config,
	pages map[string]*template.Template, frags *template.Template) *Handler {
	return &Handler{Services: services, Cfg: cfg, Pages: pages, Frags: frags}
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

func (h *Handler) render(w http.ResponseWriter, page string, data any) {
	tmpl, ok := h.Pages[page]
	if !ok {
		http.Error(w, "unknown page: "+page, http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := tmpl.ExecuteTemplate(w, "layout", data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func (h *Handler) renderFragment(w http.ResponseWriter, name string, data any) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := h.Frags.ExecuteTemplate(w, name, data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}
