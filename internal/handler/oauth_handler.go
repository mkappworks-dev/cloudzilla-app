package handler

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"time"

	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/service"
	"github.com/mkappworks-dev/cloudzilla-app/internal/view"
	"github.com/mkappworks-dev/cloudzilla-app/internal/view/pages"
	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
)

const oauthStateCookie = "oauth_state"

// Variables so tests can point the flow at a fake Google.
var (
	googleEndpoint    = google.Endpoint
	googleUserinfoURL = "https://www.googleapis.com/oauth2/v2/userinfo"
)

func (h *Handler) googleOAuthConfig() *oauth2.Config {
	return &oauth2.Config{
		ClientID:     h.Cfg.OAuth.GoogleClientID,
		ClientSecret: h.Cfg.OAuth.GoogleClientSecret,
		RedirectURL:  h.Cfg.OAuth.GoogleRedirectURL,
		Scopes:       []string{"openid", "email", "profile"},
		Endpoint:     googleEndpoint,
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
		Secure:   h.Cfg.Auth.CookieSecure,
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
	http.SetCookie(w, &http.Cookie{Name: oauthStateCookie, MaxAge: -1, Path: "/", Secure: h.Cfg.Auth.CookieSecure})

	cfg := h.googleOAuthConfig()
	token, err := cfg.Exchange(context.Background(), r.URL.Query().Get("code"))
	if err != nil {
		http.Error(w, "failed to exchange token", http.StatusBadRequest)
		return
	}

	client := cfg.Client(context.Background(), token)
	resp, err := client.Get(googleUserinfoURL)
	if err != nil {
		http.Error(w, "failed to fetch user info", http.StatusBadGateway)
		return
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		http.Error(w, "failed to fetch user info", http.StatusBadGateway)
		return
	}

	var info struct {
		ID            string `json:"id"`
		Email         string `json:"email"`
		VerifiedEmail bool   `json:"verified_email"`
		Name          string `json:"name"`
		Picture       string `json:"picture"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&info); err != nil {
		http.Error(w, "failed to parse user info", http.StatusInternalServerError)
		return
	}

	allowReg := h.Services.SiteSetting.AllowRegistration(r.Context())
	allowLogin := h.Services.SiteSetting.AllowLogin(r.Context())

	oauthUser, jwtToken, err := h.Services.User.AuthenticateOAuth(r.Context(), service.OAuthIdentity{
		Provider:      "google",
		ID:            info.ID,
		Email:         info.Email,
		EmailVerified: info.VerifiedEmail,
		Name:          info.Name,
		AvatarURL:     info.Picture,
	}, allowReg, allowLogin)
	if err != nil {
		loginError := func(status int, msg string) {
			ldapEnabled, samlEnabled := h.ssoEnabled(r)
			w.WriteHeader(status)
			h.render(w, r, pages.Login(view.LoginData{
				BasePage:          basePage(r, h.Services),
				LDAPEnabled:       ldapEnabled,
				SAMLEnabled:       samlEnabled,
				AllowRegistration: allowReg,
				Error:             msg,
			}))
		}
		switch err {
		case service.ErrRegistrationDisabled:
			http.Error(w, "Registration is currently disabled", http.StatusForbidden)
		case service.ErrLoginDisabled:
			http.Error(w, "Login is currently disabled", http.StatusForbidden)
		case service.ErrOAuthEmailUnverified:
			loginError(http.StatusForbidden, "Google hasn't verified this Google account's email address, so it can't be used to sign in. Verify the address with Google, or sign in with your password.")
		case service.ErrOAuthAccountExists:
			loginError(http.StatusConflict, "An account with this Google account's email address already exists. Sign in with your password.")
		default:
			http.Error(w, "authentication failed", http.StatusInternalServerError)
		}
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
	h.Services.AuditLog.Record(r.Context(), r, oauthUser.ID, oauthUser.Username, model.AuditActionLogin, "user", oauthUser.ID, oauthUser.Username, nil)
	http.Redirect(w, r, "/", http.StatusSeeOther)
}
