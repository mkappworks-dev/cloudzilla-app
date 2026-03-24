package middleware

import (
	"context"
	"net/http"
	"strings"

	"github.com/golang-jwt/jwt/v5"
	"github.com/mkappworks/cloudzilla/internal/model"
)

type contextKey string

const claimsKey contextKey = "claims"

type Claims struct {
	UserID       int64
	Username     string
	IsSuperadmin bool
}

// PATValidator is implemented by AccessTokenService. Defined here to avoid import cycle.
type PATValidator interface {
	Validate(ctx context.Context, rawToken string) (*model.AccessToken, *model.User, error)
	UpdateLastUsed(ctx context.Context, tokenID int64) error
}

func ClaimsFromContext(ctx context.Context) (Claims, bool) {
	c, ok := ctx.Value(claimsKey).(Claims)
	return c, ok
}

func Auth(secret, cookieName string, patValidator PATValidator) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			tokenStr := extractToken(r, cookieName)
			if tokenStr == "" {
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
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
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}

			mapClaims, ok := token.Claims.(jwt.MapClaims)
			if !ok {
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}

			claims := claimsFromMap(mapClaims)
			ctx := context.WithValue(r.Context(), claimsKey, claims)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

func OptionalAuth(secret, cookieName string, patValidator PATValidator) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			tokenStr := extractToken(r, cookieName)
			if tokenStr != "" {
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
						claims := claimsFromMap(mapClaims)
						ctx := context.WithValue(r.Context(), claimsKey, claims)
						r = r.WithContext(ctx)
					}
				}
			}
			next.ServeHTTP(w, r)
		})
	}
}

func RequireSuperadmin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		claims, ok := ClaimsFromContext(r.Context())
		if !ok || !claims.IsSuperadmin {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func claimsFromMap(m jwt.MapClaims) Claims {
	c := Claims{
		UserID:   int64(m["sub"].(float64)),
		Username: m["username"].(string),
	}
	if v, ok := m["is_superadmin"]; ok {
		c.IsSuperadmin, _ = v.(bool)
	}
	return c
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
