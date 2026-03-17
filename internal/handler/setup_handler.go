package handler

import (
	"net/http"
	"time"
)

func (h *Handler) PageSetup(w http.ResponseWriter, r *http.Request) {
	if h.Services.SiteSetting.IsSetupComplete(r.Context()) {
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}
	h.render(w, "setup", SetupData{})
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
		h.render(w, "setup", SetupData{Error: "All fields are required"})
		return
	}

	_, err := h.Services.User.CreateSuperadmin(r.Context(), username, email, password)
	if err != nil {
		h.render(w, "setup", SetupData{Error: "Failed to create admin account: " + err.Error()})
		return
	}

	_, token, err := h.Services.User.Authenticate(r.Context(), email, password)
	if err != nil {
		h.render(w, "setup", SetupData{Error: "Account created but login failed"})
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
