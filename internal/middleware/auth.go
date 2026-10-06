package middleware

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
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
	// Scoped marks a token (an OAuth-app token or a PAT) limited to Scopes.
	// Sessions are unscoped and act with the user's full access.
	Scoped bool
	Scopes []string
	// SessionVersion is the user's session version when a session JWT was issued.
	SessionVersion int
	// PAT marks a personal access token, named TokenName. A token with Targets
	// is limited to those repositories and organizations; see TargetAllows.
	PAT       bool
	TokenName string
	Targets   []string
}

// SessionStates reports a user's current session state; bumping the version
// ends every session JWT issued before. Implemented by UserService.
type SessionStates interface {
	SessionState(ctx context.Context, userID int64) (model.SessionState, error)
}

type authOptions struct {
	sessions SessionStates
}

type AuthOption func(*authOptions)

// WithSessionStates refuses session JWTs whose version is no longer the user's,
// and those of deleted or suspended users, and reads the role fresh on each
// request. Without it a JWT is good, role and all, until it expires.
func WithSessionStates(v SessionStates) AuthOption {
	return func(o *authOptions) { o.sessions = v }
}

func applyAuthOptions(opts []AuthOption) authOptions {
	var o authOptions
	for _, opt := range opts {
		opt(&o)
	}
	return o
}

// sessionLive returns c with the user's current role, or false once the session is revoked.
func (o authOptions) sessionLive(ctx context.Context, c Claims) (Claims, bool) {
	if o.sessions == nil {
		return c, true
	}
	st, err := o.sessions.SessionState(ctx, c.UserID)
	if err != nil || st.Version != c.SessionVersion {
		return Claims{}, false
	}
	c.IsSuperadmin = st.IsSuperadmin
	return c, true
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

// SignedRequestVerifier is implemented by AccessTokenService: it checks the
// signature a token bound to a key needs on every request.
type SignedRequestVerifier interface {
	VerifySignedRequest(ctx context.Context, token *model.AccessToken, req model.SignedRequest) error
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

			if serveOAuth(w, r, next, oauthResolver, tokenStr) || servePAT(w, r, next, patValidator, tokenStr) {
				return
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
			if ok {
				claims, ok = o.sessionLive(r.Context(), claims)
			}
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
func OptionalAuth(secret, cookieName string, patValidator PATValidator, oauthResolver OAuthTokenResolver, opts ...AuthOption) func(http.Handler) http.Handler {
	o := applyAuthOptions(opts)
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if alreadyAuthenticated(w, r, next) {
				return
			}
			tokenStr := extractToken(r, cookieName)
			if tokenStr != "" {
				if serveOAuth(w, r, next, oauthResolver, tokenStr) || servePAT(w, r, next, patValidator, tokenStr) {
					return
				}
				token, err := jwt.Parse(tokenStr, func(t *jwt.Token) (any, error) {
					if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
						return nil, jwt.ErrSignatureInvalid
					}
					return []byte(secret), nil
				})
				if err == nil && token.Valid {
					if mapClaims, ok := token.Claims.(jwt.MapClaims); ok {
						// A revoked session reads as signed out here, not as an error.
						if claims, ok := claimsFromMap(mapClaims); ok {
							if claims, ok = o.sessionLive(r.Context(), claims); ok {
								r = r.WithContext(context.WithValue(r.Context(), claimsKey, claims))
							}
						}
					}
				}
			}
			next.ServeHTTP(w, r)
		})
	}
}

// serveOAuth handles r when tokenStr is a live OAuth-app token, reporting whether it did.
// IsSuperadmin stays false: instance-admin power is never delegated to an app.
func serveOAuth(w http.ResponseWriter, r *http.Request, next http.Handler, resolver OAuthTokenResolver, tokenStr string) bool {
	if resolver == nil || strings.HasPrefix(tokenStr, "czp_") {
		return false
	}
	user, scopes, err := resolver.ResolveOAuthToken(r.Context(), tokenStr)
	if errors.Is(err, model.ErrAccountSuspended) {
		writeAccountSuspended(w)
		return true
	}
	if err != nil {
		return false
	}
	serveScoped(w, r, next, Claims{UserID: user.ID, Username: user.Username, Scoped: true, Scopes: scopes})
	return true
}

// servePAT handles r when tokenStr is a valid personal access token, reporting
// whether it did. A token bound to a key must carry its signature.
func servePAT(w http.ResponseWriter, r *http.Request, next http.Handler, v PATValidator, tokenStr string) bool {
	if v == nil || !strings.HasPrefix(tokenStr, "czp_") {
		return false
	}
	token, user, err := v.Validate(r.Context(), tokenStr)
	if errors.Is(err, model.ErrAccountSuspended) {
		writeAccountSuspended(w)
		return true
	}
	if err != nil {
		return false
	}
	if token.SigningKey != "" {
		var signed bool
		if r, signed = signedRequest(r, v, token); !signed {
			writeSignatureRequired(w)
			return true
		}
	}
	touchLastUsed(v, token.ID)
	serveScoped(w, r, next, PATClaims(token, user))
	return true
}

// PATClaims returns the claims a personal access token carries: its owner's
// identity, limited to the token's scopes. IsSuperadmin stays false, as for
// OAuth-app tokens: instance administration is for sessions only.
func PATClaims(t *model.AccessToken, u *model.User) Claims {
	return Claims{UserID: u.ID, Username: u.Username, Scoped: true, Scopes: t.Scopes, PAT: true, TokenName: t.Name, Targets: t.Targets}
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

// writeAccountSuspended answers a suspended user's token, which must never
// fall through to anonymous access on optional-auth routes.
func writeAccountSuspended(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusForbidden)
	_, _ = w.Write([]byte(`{"error":"account_suspended"}`))
}

func writeSignatureRequired(w http.ResponseWriter) {
	w.Header().Set("WWW-Authenticate", `Bearer error="invalid_token"`)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusUnauthorized)
	_, _ = w.Write([]byte(`{"error":"this token needs each request signed with its key"}`))
}

// serveScoped passes r on with claims, or refuses it when their scopes don't
// admit r. A refused token is never downgraded to anonymous on optional-auth routes.
func serveScoped(w http.ResponseWriter, r *http.Request, next http.Handler, claims Claims) {
	if !ScopeAllows(claims, r) {
		WriteInsufficientScope(w, RequiredScope(r))
		return
	}
	if len(claims.Targets) > 0 && !TargetAllows(claims.Targets, r) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"error":"this token isn't allowed for that repository or organization"}`))
		return
	}
	next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), claimsKey, claims)))
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
