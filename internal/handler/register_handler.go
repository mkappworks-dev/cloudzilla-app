package handler

import (
	"net/http"
	"strings"
	"time"

	"github.com/mkappworks-dev/cloudzilla-app/internal/middleware"
	"github.com/mkappworks-dev/cloudzilla-app/internal/view"
	"github.com/mkappworks-dev/cloudzilla-app/internal/view/pages"
)

// PageRegister renders the public registration form when allow_registration is enabled.
func (h *Handler) PageRegister(w http.ResponseWriter, r *http.Request) {
	if _, ok := middleware.ClaimsFromContext(r.Context()); ok {
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}
	if !h.Services.SiteSetting.AllowRegistration(r.Context()) {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}
	h.render(w, r, pages.Register(view.RegisterData{BasePage: basePage(r, h.Services)}))
}

// PageRegisterSubmit creates a new user account, sets the auth cookie, and redirects on success.
func (h *Handler) PageRegisterSubmit(w http.ResponseWriter, r *http.Request) {
	if !h.Services.SiteSetting.AllowRegistration(r.Context()) {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}

	username := strings.TrimSpace(r.FormValue("username"))
	email := strings.TrimSpace(r.FormValue("email"))
	password := r.FormValue("password")

	renderError := func(msg string) {
		h.render(w, r, pages.Register(view.RegisterData{
			BasePage: basePage(r, h.Services),
			Username: username,
			Email:    email,
			Error:    msg,
		}))
	}

	if username == "" || email == "" || password == "" {
		renderError("All fields are required")
		return
	}
	if len(password) < 8 {
		renderError("Password must be at least 8 characters")
		return
	}

	if _, err := h.Services.User.Create(r.Context(), username, email, password); err != nil {
		renderError("Failed to create account: " + err.Error())
		return
	}

	_, jwtToken, err := h.Services.User.Authenticate(r.Context(), email, password)
	if err != nil {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}

	http.SetCookie(w, &http.Cookie{
		Name:     h.Cfg.Auth.CookieName,
		Value:    jwtToken,
		HttpOnly: true,
		Secure:   h.Cfg.Auth.CookieSecure,
		Path:     "/",
		Expires:  time.Now().Add(h.Cfg.Auth.JWTExpiry),
		SameSite: http.SameSiteLaxMode,
	})

	http.Redirect(w, r, "/", http.StatusSeeOther)
}
