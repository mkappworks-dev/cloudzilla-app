package middleware

import (
	"context"
	"log/slog"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/mkappworks-dev/cloudzilla-app/internal/concurrency"
	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
)

type contextKey string

const claimsKey contextKey = "claims"

// Claims holds the authenticated user's identity extracted from a JWT, PAT or OAuth-app token.
type Claims struct {
	UserID       int64
	Username     string
	IsSuperadmin bool
	// Scoped marks a delegated credential (an OAuth-app token) limited to Scopes.
	// First-party sessions and PATs are unscoped and act with the user's full access.
	Scoped bool
	Scopes []string
}

// HasScope reports whether the credential grants scope. Unscoped credentials grant every scope.
func (c Claims) HasScope(scope string) bool {
	return !c.Scoped || slices.Contains(c.Scopes, scope)
}

// PATValidator is implemented by AccessTokenService. Defined here to avoid import cycle.
// PATValidator validates a raw personal access token and returns the associated user ID.
type PATValidator interface {
	Validate(ctx context.Context, rawToken string) (*model.AccessToken, *model.User, error)
	UpdateLastUsed(ctx context.Context, tokenID int64) error
}

// OAuthTokenResolver resolves a raw OAuth-app bearer token to its user and granted scopes.
// Implemented by OAuthAppService; defined here to avoid import cycle.
type OAuthTokenResolver interface {
	ResolveOAuthToken(ctx context.Context, rawToken string) (*model.User, []string, error)
}

// ClaimsFromContext extracts the authenticated user claims from a request context.
func ClaimsFromContext(ctx context.Context) (Claims, bool) {
	c, ok := ctx.Value(claimsKey).(Claims)
	return c, ok
}

// Auth returns middleware that requires a valid JWT cookie, Bearer token, or PAT.
// onUnauthorized handles unauthenticated requests (redirect to /login for HTML, JSON 401 for API).
func Auth(secret, cookieName string, patValidator PATValidator, oauthResolver OAuthTokenResolver, onUnauthorized http.HandlerFunc) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			tokenStr := extractToken(r, cookieName)
			if tokenStr == "" {
				onUnauthorized(w, r)
				return
			}

			if claims, ok := resolveOAuthClaims(r, oauthResolver, tokenStr); ok {
				if !scopeAllows(claims, r) {
					writeInsufficientScope(w, r)
					return
				}
				ctx := context.WithValue(r.Context(), claimsKey, claims)
				next.ServeHTTP(w, r.WithContext(ctx))
				return
			}

			if strings.HasPrefix(tokenStr, "czp_") && patValidator != nil {
				token, user, err := patValidator.Validate(r.Context(), tokenStr)
				if err == nil {
					touchLastUsed(patValidator, token.ID)
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
func OptionalAuth(secret, cookieName string, patValidator PATValidator, oauthResolver OAuthTokenResolver) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			tokenStr := extractToken(r, cookieName)
			if tokenStr != "" {
				if claims, ok := resolveOAuthClaims(r, oauthResolver, tokenStr); ok {
					if !scopeAllows(claims, r) {
						writeInsufficientScope(w, r)
						return
					}
					ctx := context.WithValue(r.Context(), claimsKey, claims)
					next.ServeHTTP(w, r.WithContext(ctx))
					return
				}
				if strings.HasPrefix(tokenStr, "czp_") && patValidator != nil {
					pat, user, err := patValidator.Validate(r.Context(), tokenStr)
					if err == nil {
						touchLastUsed(patValidator, pat.ID)
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

// resolveOAuthClaims returns scoped claims when tokenStr is a live OAuth-app token.
// IsSuperadmin stays false: instance-admin power is never delegated to an app.
func resolveOAuthClaims(r *http.Request, resolver OAuthTokenResolver, tokenStr string) (Claims, bool) {
	if resolver == nil || strings.HasPrefix(tokenStr, "czp_") {
		return Claims{}, false
	}
	user, scopes, err := resolver.ResolveOAuthToken(r.Context(), tokenStr)
	if err != nil {
		return Claims{}, false
	}
	return Claims{UserID: user.ID, Username: user.Username, Scoped: true, Scopes: scopes}, true
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

func touchLastUsed(v PATValidator, tokenID int64) {
	concurrency.Go("access_token.update_last_used", func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := v.UpdateLastUsed(ctx, tokenID); err != nil {
			slog.Warn("access token last_used update failed", "token_id", tokenID, "error", err)
		}
	})
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
