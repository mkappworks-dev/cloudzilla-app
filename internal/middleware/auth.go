package middleware

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
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
	// SessionVersion is the user's session version when a session JWT was issued.
	SessionVersion int
	// PAT marks a personal access token. One created with scopes is held to
	// them (Scoped); one created without keeps the user's full access.
	PAT       bool
	TokenName string
}

// patClaims are the claims of a personal access token.
func patClaims(token *model.AccessToken, user *model.User) Claims {
	return Claims{
		UserID: user.ID, Username: user.Username, IsSuperadmin: user.IsSuperadmin,
		PAT: true, TokenName: token.Name, Scoped: len(token.Scopes) > 0, Scopes: token.Scopes,
	}
}

// refuseScope answers a scoped token that r's route doesn't admit.
func refuseScope(w http.ResponseWriter, r *http.Request) {
	var hint string
	if accepted := acceptedScopes(r); len(accepted) > 0 {
		hint = accepted[0]
	}
	WriteInsufficientScope(w, hint)
}

// SessionVersions reports a user's current session version; bumping it ends
// every session JWT issued before. Implemented by UserService.
type SessionVersions interface {
	SessionVersion(ctx context.Context, userID int64) (int, error)
}

type authOptions struct {
	sessions SessionVersions
}

type AuthOption func(*authOptions)

// WithSessionVersions refuses session JWTs whose version is no longer the user's,
// and those of deleted users. Without it a JWT is good until it expires.
func WithSessionVersions(v SessionVersions) AuthOption {
	return func(o *authOptions) { o.sessions = v }
}

func applyAuthOptions(opts []AuthOption) authOptions {
	var o authOptions
	for _, opt := range opts {
		opt(&o)
	}
	return o
}

func (o authOptions) sessionLive(ctx context.Context, c Claims) bool {
	if o.sessions == nil {
		return true
	}
	v, err := o.sessions.SessionVersion(ctx, c.UserID)
	return err == nil && v == c.SessionVersion
}

// HasScope reports whether the credential grants scope. Unscoped credentials grant every scope.
func (c Claims) HasScope(scope string) bool {
	return !c.Scoped || slices.Contains(c.Scopes, scope)
}

// SignedRequestVerifier is implemented by AccessTokenService: it checks the
// signature a token bound to a key needs on every request.
type SignedRequestVerifier interface {
	VerifySignedRequest(ctx context.Context, token *model.AccessToken, req model.SignedRequest) error
}

const maxSignedBody = 1 << 20

// signedRequest checks the signature of a request made with token, a token
// bound to a key; see AccessTokenService.VerifySignedRequest. It hashes the
// body, so it returns r with the body restored. It fails closed: a validator
// that can't verify signatures refuses every key-bound token.
func signedRequest(r *http.Request, v PATValidator, token *model.AccessToken) (*http.Request, bool) {
	sv, ok := v.(SignedRequestVerifier)
	if !ok {
		return r, false
	}
	var body []byte
	if r.Body != nil {
		var err error
		if body, err = io.ReadAll(io.LimitReader(r.Body, maxSignedBody+1)); err != nil || len(body) > maxSignedBody {
			return r, false
		}
		r.Body = io.NopCloser(bytes.NewReader(body))
	}
	sum := sha256.Sum256(body)
	err := sv.VerifySignedRequest(r.Context(), token, model.SignedRequest{
		Method:     r.Method,
		Target:     r.URL.RequestURI(),
		Timestamp:  r.Header.Get("X-Cloudzilla-Timestamp"),
		Nonce:      r.Header.Get("X-Cloudzilla-Nonce"),
		BodySHA256: hex.EncodeToString(sum[:]),
		Signature:  r.Header.Get("X-Cloudzilla-Signature"),
	})
	if err != nil {
		slog.Warn("signed request refused", "token_id", token.ID, "target", r.URL.RequestURI(), "error", err)
	}
	return r, err == nil
}

func writeSignatureRequired(w http.ResponseWriter) {
	w.Header().Set("WWW-Authenticate", `Bearer error="invalid_token"`)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusUnauthorized)
	_, _ = w.Write([]byte(`{"error":"this token needs each request signed with its key"}`))
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

// alreadyAuthenticated passes r on when an outer auth middleware already set
// its claims, as optAuthMW does for route groups whose routes add authMW.
// Checking a signed token's request again would find its nonce spent.
func alreadyAuthenticated(w http.ResponseWriter, r *http.Request, next http.Handler) bool {
	if _, ok := ClaimsFromContext(r.Context()); !ok {
		return false
	}
	next.ServeHTTP(w, r)
	return true
}

// Auth returns middleware that requires a valid JWT cookie, Bearer token, or PAT.
// onUnauthorized handles unauthenticated requests (redirect to /login for HTML, JSON 401 for API).
func Auth(secret, cookieName string, patValidator PATValidator, oauthResolver OAuthTokenResolver, onUnauthorized http.HandlerFunc, opts ...AuthOption) func(http.Handler) http.Handler {
	o := applyAuthOptions(opts)
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if alreadyAuthenticated(w, r, next) {
				return
			}
			tokenStr := extractToken(r, cookieName)
			if tokenStr == "" {
				onUnauthorized(w, r)
				return
			}

			if serveOAuth(w, r, next, oauthResolver, tokenStr) {
				return
			}

			if strings.HasPrefix(tokenStr, "czp_") && patValidator != nil {
				token, user, err := patValidator.Validate(r.Context(), tokenStr)
				if err == nil {
					if token.SigningKey != "" {
						var signed bool
						if r, signed = signedRequest(r, patValidator, token); !signed {
							writeSignatureRequired(w)
							return
						}
					}
					touchLastUsed(patValidator, token.ID)
					claims := patClaims(token, user)
					if !scopeAllows(claims, r) {
						refuseScope(w, r)
						return
					}
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
			if !ok || !o.sessionLive(r.Context(), claims) {
				onUnauthorized(w, r)
				return
			}
			ctx := context.WithValue(r.Context(), claimsKey, claims)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// OptionalAuth returns middleware that reads auth credentials if present but allows unauthenticated requests.
func OptionalAuth(secret, cookieName string, patValidator PATValidator, oauthResolver OAuthTokenResolver, opts ...AuthOption) func(http.Handler) http.Handler {
	o := applyAuthOptions(opts)
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if alreadyAuthenticated(w, r, next) {
				return
			}
			tokenStr := extractToken(r, cookieName)
			if tokenStr != "" {
				if serveOAuth(w, r, next, oauthResolver, tokenStr) {
					return
				}
				if strings.HasPrefix(tokenStr, "czp_") && patValidator != nil {
					pat, user, err := patValidator.Validate(r.Context(), tokenStr)
					if err == nil {
						if pat.SigningKey != "" {
							var signed bool
							if r, signed = signedRequest(r, patValidator, pat); !signed {
								writeSignatureRequired(w)
								return
							}
						}
						touchLastUsed(patValidator, pat.ID)
						claims := patClaims(pat, user)
						if !scopeAllows(claims, r) {
							refuseScope(w, r)
							return
						}
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
						if claims, ok := claimsFromMap(mapClaims); ok && o.sessionLive(r.Context(), claims) {
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

// serveOAuth handles r when tokenStr is a live OAuth-app token, reporting whether it did.
// A token lacking scope for r is refused rather than passed on as anonymous.
// IsSuperadmin stays false: instance-admin power is never delegated to an app.
func serveOAuth(w http.ResponseWriter, r *http.Request, next http.Handler, resolver OAuthTokenResolver, tokenStr string) bool {
	if resolver == nil || strings.HasPrefix(tokenStr, "czp_") {
		return false
	}
	user, scopes, err := resolver.ResolveOAuthToken(r.Context(), tokenStr)
	if err != nil {
		return false
	}
	claims := Claims{UserID: user.ID, Username: user.Username, Scoped: true, Scopes: scopes}
	if !scopeAllows(claims, r) {
		refuseScope(w, r)
		return true
	}
	next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), claimsKey, claims)))
	return true
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
	// Tokens from before session versions existed read as version 0.
	if v, ok := m["sv"].(float64); ok {
		c.SessionVersion = int(v)
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
