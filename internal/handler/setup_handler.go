package handler

import (
	"net/http"
	"time"

	"github.com/mkappworks/cloudzilla/internal/view"
	"github.com/mkappworks/cloudzilla/internal/view/pages"
)

func (h *Handler) PageSetup(w http.ResponseWriter, r *http.Request) {
	if h.Services.SiteSetting.IsSetupComplete(r.Context()) {
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}
	h.render(w, r, pages.Setup(view.SetupData{}))
}

func (h *Handler) PageSetupSubmit(w http.ResponseWriter, r *http.Request) {
	if h.Services.SiteSetting.IsSetupComplete(r.Context()) {
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}

	username := r.FormValue("username")
	email := r.FormValue("email")
	password := r.FormValue("password")

	if username == "" || email == "" || password == "" {
		h.render(w, r, pages.Setup(view.SetupData{Error: "All fields are required"}))
		return
	}

	_, err := h.Services.User.CreateSuperadmin(r.Context(), username, email, password)
	if err != nil {
		h.render(w, r, pages.Setup(view.SetupData{Error: "Failed to create admin account: " + err.Error()}))
		return
	}

	_, token, err := h.Services.User.Authenticate(r.Context(), email, password)
	if err != nil {
		h.render(w, r, pages.Setup(view.SetupData{Error: "Account created but login failed"}))
		return
	}

	http.SetCookie(w, &http.Cookie{
		Name:     h.Cfg.Auth.CookieName,
		Value:    token,
		HttpOnly: true,
		Path:     "/",
		Expires:  time.Now().Add(h.Cfg.Auth.JWTExpiry),
		SameSite: http.SameSiteLaxMode,
	})

	http.Redirect(w, r, "/", http.StatusSeeOther)
}
