package handler

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"time"

	"github.com/mkappworks-dev/cloudzilla-app/internal/service"
	"github.com/mkappworks-dev/cloudzilla-app/internal/view"
	"github.com/mkappworks-dev/cloudzilla-app/internal/view/pages"
	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
)

const (
	oauthStateCookie     = "oauth_state"
	oauthNextCookie      = "oauth_next"
	oauthLinkStateCookie = "oauth_link_state"
	googleCallbackPath   = "/auth/google/callback"
	googleProvider       = "google"
)

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
	next := r.URL.Query().Get("next")
	nextCookie := &http.Cookie{
		Name:     oauthNextCookie,
		Value:    url.QueryEscape(next),
		HttpOnly: true,
		Secure:   h.Cfg.Auth.CookieSecure,
		Path:     "/",
		Expires:  time.Now().Add(5 * time.Minute),
		SameSite: http.SameSiteLaxMode,
	}
	if next == "" {
		// Clear any return path left by an abandoned earlier attempt.
		nextCookie.MaxAge = -1
	}
	http.SetCookie(w, nextCookie)
	http.Redirect(w, r, h.googleOAuthConfig().AuthCodeURL(state), http.StatusTemporaryRedirect)
}

func (h *Handler) GoogleOAuthCallback(w http.ResponseWriter, r *http.Request) {
	state := r.URL.Query().Get("state")
	// The cookie ConnectGoogle set picks link mode, not the state alone, so only the
	// browser that re-authenticated can finish a link.
	if c, err := r.Cookie(oauthLinkStateCookie); err == nil && c.Value != "" && c.Value == state {
		h.googleLinkCallback(w, r, state)
		return
	}
	stateCookie, err := r.Cookie(oauthStateCookie)
	if err != nil || stateCookie.Value != state {
		http.Error(w, "invalid OAuth state", http.StatusBadRequest)
		return
	}
	http.SetCookie(w, &http.Cookie{Name: oauthStateCookie, MaxAge: -1, Path: "/", Secure: h.Cfg.Auth.CookieSecure})
	var next string
	if c, err := r.Cookie(oauthNextCookie); err == nil {
		next, _ = url.QueryUnescape(c.Value)
		http.SetCookie(w, &http.Cookie{Name: oauthNextCookie, MaxAge: -1, Path: "/", Secure: h.Cfg.Auth.CookieSecure})
	}

	identity, status, err := h.googleIdentity(r.Context(), r.URL.Query().Get("code"))
	if err != nil {
		http.Error(w, err.Error(), status)
		return
	}

	allowReg := h.Services.SiteSetting.AllowRegistration(r.Context())
	allowLogin := h.Services.SiteSetting.AllowLogin(r.Context())

	fail := func(err error) {
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
			msg := "An account with this Google account's email address already exists. Sign in with your password, then connect Google under Account settings → Security."
			if h.Services.EmailVerifier.Available() {
				msg += " Verifying the address there also lets Google sign you in directly."
			}
			loginError(http.StatusConflict, msg)
		case service.ErrOAuthAlreadyLinked:
			loginError(http.StatusConflict, "The account with this email address is linked to a different Google account. Sign in with that Google account or with your password.")
		default:
			http.Error(w, "authentication failed", http.StatusInternalServerError)
		}
	}

	login, err := h.Services.User.AuthenticateOAuth(r.Context(), identity, allowReg, allowLogin)
	if err != nil {
		fail(err)
		return
	}
	if err := h.signInLinking(w, r, login.User, login.Token, next, login.Link); err != nil {
		fail(err)
	}
}

// googleIdentity redeems code at Google and reads the account it grants. On
// failure the error text is safe to show, and status is the code to send.
func (h *Handler) googleIdentity(ctx context.Context, code string) (service.OAuthIdentity, int, error) {
	cfg := h.googleOAuthConfig()
	token, err := cfg.Exchange(ctx, code)
	if err != nil {
		return service.OAuthIdentity{}, http.StatusBadRequest, errors.New("failed to exchange token")
	}

	resp, err := cfg.Client(ctx, token).Get(googleUserinfoURL)
	if err != nil {
		return service.OAuthIdentity{}, http.StatusBadGateway, errors.New("failed to fetch user info")
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return service.OAuthIdentity{}, http.StatusBadGateway, errors.New("failed to fetch user info")
	}

	var info struct {
		ID            string `json:"id"`
		Email         string `json:"email"`
		VerifiedEmail bool   `json:"verified_email"`
		Name          string `json:"name"`
		Picture       string `json:"picture"`
	}
	// An empty ID would be stored as a link that every later ID-less response signs in to.
	if err := json.NewDecoder(resp.Body).Decode(&info); err != nil || info.ID == "" {
		return service.OAuthIdentity{}, http.StatusInternalServerError, errors.New("failed to parse user info")
	}
	return service.OAuthIdentity{
		Provider:      googleProvider,
		ID:            info.ID,
		Email:         info.Email,
		EmailVerified: info.VerifiedEmail,
		Name:          info.Name,
		AvatarURL:     info.Picture,
	}, 0, nil
}
