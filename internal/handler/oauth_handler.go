package handler

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"time"

	"github.com/mkappworks/cloudzilla/internal/service"
	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
)

const oauthStateCookie = "oauth_state"

func (h *Handler) googleOAuthConfig() *oauth2.Config {
	return &oauth2.Config{
		ClientID:     h.Cfg.OAuth.GoogleClientID,
		ClientSecret: h.Cfg.OAuth.GoogleClientSecret,
		RedirectURL:  h.Cfg.OAuth.GoogleRedirectURL,
		Scopes:       []string{"openid", "email", "profile"},
		Endpoint:     google.Endpoint,
	}
}

func (h *Handler) GoogleOAuthBegin(w http.ResponseWriter, r *http.Request) {
	if h.Cfg.OAuth.GoogleClientID == "" {
		http.Error(w, "Google OAuth not configured", http.StatusNotImplemented)
		return
	}
	b := make([]byte, 16)
	rand.Read(b)
	state := hex.EncodeToString(b)

	http.SetCookie(w, &http.Cookie{
		Name:     oauthStateCookie,
		Value:    state,
		HttpOnly: true,
		Path:     "/",
		Expires:  time.Now().Add(5 * time.Minute),
		SameSite: http.SameSiteLaxMode,
	})
	http.Redirect(w, r, h.googleOAuthConfig().AuthCodeURL(state), http.StatusTemporaryRedirect)
}

func (h *Handler) GoogleOAuthCallback(w http.ResponseWriter, r *http.Request) {
	stateCookie, err := r.Cookie(oauthStateCookie)
	if err != nil || stateCookie.Value != r.URL.Query().Get("state") {
		http.Error(w, "invalid OAuth state", http.StatusBadRequest)
		return
	}
	http.SetCookie(w, &http.Cookie{Name: oauthStateCookie, MaxAge: -1, Path: "/"})

	cfg := h.googleOAuthConfig()
	token, err := cfg.Exchange(context.Background(), r.URL.Query().Get("code"))
	if err != nil {
		http.Error(w, "failed to exchange token", http.StatusBadRequest)
		return
	}

	client := cfg.Client(context.Background(), token)
	resp, err := client.Get("https://www.googleapis.com/oauth2/v2/userinfo")
	if err != nil || resp.StatusCode != http.StatusOK {
		http.Error(w, "failed to fetch user info", http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	var info struct {
		ID      string `json:"id"`
		Email   string `json:"email"`
		Name    string `json:"name"`
		Picture string `json:"picture"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&info); err != nil {
		http.Error(w, "failed to parse user info", http.StatusInternalServerError)
		return
	}

	allowReg := h.Services.SiteSetting.AllowRegistration(r.Context())
	allowLogin := h.Services.SiteSetting.AllowLogin(r.Context())

	_, jwtToken, err := h.Services.User.AuthenticateOAuth(r.Context(), "google", info.ID, info.Email, info.Name, info.Picture, allowReg, allowLogin)
	if err != nil {
		switch err {
		case service.ErrRegistrationDisabled:
			http.Error(w, "Registration is currently disabled", http.StatusForbidden)
		case service.ErrLoginDisabled:
			http.Error(w, "Login is currently disabled", http.StatusForbidden)
		default:
			http.Error(w, "authentication failed", http.StatusInternalServerError)
		}
		return
	}

	http.SetCookie(w, &http.Cookie{
		Name:     h.Cfg.Auth.CookieName,
		Value:    jwtToken,
		HttpOnly: true,
		Path:     "/",
		Expires:  time.Now().Add(h.Cfg.Auth.JWTExpiry),
		SameSite: http.SameSiteLaxMode,
	})
	http.Redirect(w, r, "/", http.StatusSeeOther)
}
