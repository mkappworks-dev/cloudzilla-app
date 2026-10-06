package middleware

import (
	"context"
	"net/http"

	"github.com/golang-jwt/jwt/v5"
	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
)

const credentialKey contextKey = "credential"

type credentialKind int

const (
	credentialSession credentialKind = iota + 1
	credentialPAT
	credentialOAuth
)

// resolvedCredential is APIRateLimit's lookup of raw, failures included, so auth needn't repeat it.
type resolvedCredential struct {
	raw    string
	kind   credentialKind
	token  *model.AccessToken
	user   *model.User
	scopes []string
	err    error
}

func withCredential(r *http.Request, c resolvedCredential) *http.Request {
	return r.WithContext(context.WithValue(r.Context(), credentialKey, c))
}

// credentialFor returns what is known about raw, matching the exact string so a
// request can't borrow another credential's result.
func credentialFor(ctx context.Context, raw string) (resolvedCredential, bool) {
	c, ok := ctx.Value(credentialKey).(resolvedCredential)
	return c, ok && c.raw == raw
}

// ValidatePAT validates rawToken with v, reusing the result APIRateLimit stored
// for this request when it already looked the token up.
func ValidatePAT(r *http.Request, v PATValidator, rawToken string) (*model.AccessToken, *model.User, error) {
	if c, ok := credentialFor(r.Context(), rawToken); ok && c.kind == credentialPAT {
		return c.token, c.user, c.err
	}
	return v.Validate(r.Context(), rawToken)
}

func resolveOAuth(r *http.Request, resolver OAuthTokenResolver, rawToken string) (*model.User, []string, error) {
	if c, ok := credentialFor(r.Context(), rawToken); ok && c.kind == credentialOAuth {
		return c.user, c.scopes, c.err
	}
	return resolver.ResolveOAuthToken(r.Context(), rawToken)
}

// isSessionJWT reports whether APIRateLimit already found rawToken to be a session JWT.
func isSessionJWT(r *http.Request, rawToken string) bool {
	c, ok := credentialFor(r.Context(), rawToken)
	return ok && c.kind == credentialSession
}

// parseSessionJWT checks tokenStr's HMAC signature and expiry with secret. It
// doesn't check the session version.
func parseSessionJWT(secret, tokenStr string) (Claims, bool) {
	token, err := jwt.Parse(tokenStr, func(t *jwt.Token) (any, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, jwt.ErrSignatureInvalid
		}
		return []byte(secret), nil
	})
	if err != nil || !token.Valid {
		return Claims{}, false
	}
	mapClaims, ok := token.Claims.(jwt.MapClaims)
	if !ok {
		return Claims{}, false
	}
	return claimsFromMap(mapClaims)
}
