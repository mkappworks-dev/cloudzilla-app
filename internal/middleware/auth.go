package middleware

import (
	"context"
	"net/http"
	"strings"

	"github.com/golang-jwt/jwt/v5"
	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
)

type contextKey string

const claimsKey contextKey = "claims"

// Claims holds the authenticated user's identity extracted from a JWT or PAT.
type Claims struct {
	UserID       int64
	Username     string
	IsSuperadmin bool
}

// PATValidator is implemented by AccessTokenService. Defined here to avoid import cycle.
// PATValidator validates a raw personal access token and returns the associated user ID.
type PATValidator interface {
	Validate(ctx context.Context, rawToken string) (*model.AccessToken, *model.User, error)
	UpdateLastUsed(ctx context.Context, tokenID int64) error
}

// OAuthUserIDResolver resolves a raw OAuth bearer token to a user ID.
// Implemented by OAuthAppService; defined here to avoid import cycle.
// OAuthUserIDResolver resolves an OAuth access token to an internal user ID.
type OAuthUserIDResolver interface {
	ResolveOAuthUserID(ctx context.Context, rawToken string) (int64, error)
}

// ClaimsFromContext extracts the authenticated user claims from a request context.
func ClaimsFromContext(ctx context.Context) (Claims, bool) {
	c, ok := ctx.Value(claimsKey).(Claims)
	return c, ok
}

// Auth returns middleware that requires a valid JWT cookie, Bearer token, or PAT.
// onUnauthorized handles unauthenticated requests (redirect to /login for HTML, JSON 401 for API).
func Auth(secret, cookieName string, patValidator PATValidator, oauthResolver OAuthUserIDResolver, onUnauthorized http.HandlerFunc) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			tokenStr := extractToken(r, cookieName)
			if tokenStr == "" {
				onUnauthorized(w, r)
				return
			}

			if oauthResolver != nil && !strings.HasPrefix(tokenStr, "czp_") {
				if uid, err := oauthResolver.ResolveOAuthUserID(r.Context(), tokenStr); err == nil {
					claims := Claims{UserID: uid}
					ctx := context.WithValue(r.Context(), claimsKey, claims)
					next.ServeHTTP(w, r.WithContext(ctx))
					return
				}
			}

			if strings.HasPrefix(tokenStr, "czp_") && patValidator != nil {
				token, user, err := patValidator.Validate(r.Context(), tokenStr)
				if err == nil {
					go patValidator.UpdateLastUsed(context.Background(), token.ID)
					claims := Claims{UserID: user.ID, Username: user.Username, IsSuperadmin: user.IsSuperadmin}
					ctx := context.WithValue(r.Context(), claimsKey, claims)
					next.ServeHTTP(w, r.WithContext(ctx))
					return
				}
			}

			token, err := jwt.Parse(tokenStr, func(t *jwt.Token) (any, error) {
				if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
					return nil, jwt.ErrSignatureInvalid
				}
				return []byte(secret), nil
			})
			if err != nil || !token.Valid {
				onUnauthorized(w, r)
				return
			}

			mapClaims, ok := token.Claims.(jwt.MapClaims)
			if !ok {
				onUnauthorized(w, r)
				return
			}

			claims, ok := claimsFromMap(mapClaims)
			if !ok {
				onUnauthorized(w, r)
				return
			}
			ctx := context.WithValue(r.Context(), claimsKey, claims)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// OptionalAuth returns middleware that reads auth credentials if present but allows unauthenticated requests.
func OptionalAuth(secret, cookieName string, patValidator PATValidator, oauthResolver OAuthUserIDResolver) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			tokenStr := extractToken(r, cookieName)
			if tokenStr != "" {
				if oauthResolver != nil && !strings.HasPrefix(tokenStr, "czp_") {
					if uid, err := oauthResolver.ResolveOAuthUserID(r.Context(), tokenStr); err == nil {
						claims := Claims{UserID: uid}
						ctx := context.WithValue(r.Context(), claimsKey, claims)
						r = r.WithContext(ctx)
						next.ServeHTTP(w, r)
						return
					}
				}
				if strings.HasPrefix(tokenStr, "czp_") && patValidator != nil {
					pat, user, err := patValidator.Validate(r.Context(), tokenStr)
					if err == nil {
						go patValidator.UpdateLastUsed(context.Background(), pat.ID)
						claims := Claims{UserID: user.ID, Username: user.Username, IsSuperadmin: user.IsSuperadmin}
						ctx := context.WithValue(r.Context(), claimsKey, claims)
						r = r.WithContext(ctx)
						next.ServeHTTP(w, r)
						return
					}
				}
				token, err := jwt.Parse(tokenStr, func(t *jwt.Token) (any, error) {
					if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
						return nil, jwt.ErrSignatureInvalid
					}
					return []byte(secret), nil
				})
				if err == nil && token.Valid {
					if mapClaims, ok := token.Claims.(jwt.MapClaims); ok {
						if claims, ok := claimsFromMap(mapClaims); ok {
							ctx := context.WithValue(r.Context(), claimsKey, claims)
							r = r.WithContext(ctx)
						}
					}
				}
			}
			next.ServeHTTP(w, r)
		})
	}
}

// RequireSuperadmin returns middleware that calls onForbidden if the authenticated user is not a superadmin.
func RequireSuperadmin(onForbidden http.HandlerFunc) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			claims, ok := ClaimsFromContext(r.Context())
			if !ok || !claims.IsSuperadmin {
				onForbidden(w, r)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

func claimsFromMap(m jwt.MapClaims) (Claims, bool) {
	sub, ok := m["sub"].(float64)
	if !ok {
		return Claims{}, false
	}
	username, ok := m["username"].(string)
	if !ok {
		return Claims{}, false
	}
	c := Claims{
		UserID:   int64(sub),
		Username: username,
	}
	if v, ok := m["is_superadmin"].(bool); ok {
		c.IsSuperadmin = v
	}
	return c, true
}

func extractToken(r *http.Request, cookieName string) string {
	// Check Authorization header first
	if auth := r.Header.Get("Authorization"); strings.HasPrefix(auth, "Bearer ") {
		return strings.TrimPrefix(auth, "Bearer ")
	}
	// Fall back to cookie
	if cookie, err := r.Cookie(cookieName); err == nil {
		return cookie.Value
	}
	return ""
}
