package handler

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strings"

	"github.com/mkappworks-dev/cloudzilla-app/internal/middleware"
	"github.com/mkappworks-dev/cloudzilla-app/internal/service"
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
	if h.Services.Signup.Enabled() {
		h.render(w, r, pages.RegisterEmail(view.RegisterEmailData{BasePage: basePage(r, h.Services)}))
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
	if h.Services.Signup.Enabled() {
		h.requestSignup(w, r)
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
	if msg := passwordLengthMessage(password); msg != "" {
		renderError(msg)
		return
	}

	if _, err := h.Services.User.Create(r.Context(), username, email, password); err != nil {
		logCreateAccountFailure(r.Context(), "register: create user failed", err)
		renderError(createAccountErrorMessage(err))
		return
	}

	_, jwtToken, err := h.Services.User.Authenticate(r.Context(), email, password)
	if err != nil {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}

	h.setAuthCookie(w, jwtToken)

	http.Redirect(w, r, "/", http.StatusSeeOther)
}

// Usernames are public, but an email conflict gets the generic message so the
// form doesn't confirm which addresses have accounts.
func createAccountErrorMessage(err error) string {
	switch {
	case errors.Is(err, service.ErrUsernameTaken):
		return "That username is already taken"
	case errors.Is(err, service.ErrInvalidOwnerName):
		return invalidUsernameMessage
	}
	return "Could not create account. If you already have one, sign in instead."
}

// Invalid or taken usernames and taken emails are user mistakes, so they log below Error.
func logCreateAccountFailure(ctx context.Context, msg string, err error, args ...any) {
	level := slog.LevelError
	if errors.Is(err, service.ErrUsernameTaken) || errors.Is(err, service.ErrEmailTaken) || errors.Is(err, service.ErrInvalidOwnerName) {
		level = slog.LevelInfo
	}
	slog.Log(ctx, level, msg, append(args, "error", err)...)
}
