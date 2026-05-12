package middleware

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
)

// --- helpers ---

const testSecret = "test-jwt-secret-32-bytes-minimum!"

// testUnauthorized writes a 401 — mimics the old http.Error behavior so tests
// that asserted on the 401 status still pass.
var testUnauthorized http.HandlerFunc = func(w http.ResponseWriter, _ *http.Request) {
	http.Error(w, "unauthorized", http.StatusUnauthorized)
}

// testForbidden writes a 403 — same idea for RequireSuperadmin tests.
var testForbidden http.HandlerFunc = func(w http.ResponseWriter, _ *http.Request) {
	http.Error(w, "forbidden", http.StatusForbidden)
}

// makeValidJWT creates a signed HS256 JWT with the given user fields and a 1-hour expiry.
func makeValidJWT(t *testing.T, userID int64, username string, superadmin bool) string {
	t.Helper()
	claims := jwt.MapClaims{
		"sub":           float64(userID),
		"username":      username,
		"is_superadmin": superadmin,
		"exp":           float64(time.Now().Add(time.Hour).Unix()),
	}
	tok := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	signed, err := tok.SignedString([]byte(testSecret))
	if err != nil {
		t.Fatalf("makeValidJWT: %v", err)
	}
	return signed
}

// makeExpiredJWT creates a signed HS256 JWT with an expiry 1 hour in the past.
func makeExpiredJWT(t *testing.T) string {
	t.Helper()
	claims := jwt.MapClaims{
		"sub":      float64(1),
		"username": "alice",
		"exp":      float64(time.Now().Add(-time.Hour).Unix()),
	}
	tok := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	signed, _ := tok.SignedString([]byte(testSecret))
	return signed
}

// stubPAT is a test implementation of PATValidator.
type stubPAT struct {
	token *model.AccessToken
	user  *model.User
	err   error
}

func (s *stubPAT) Validate(_ context.Context, _ string) (*model.AccessToken, *model.User, error) {
	return s.token, s.user, s.err
}
func (s *stubPAT) UpdateLastUsed(_ context.Context, _ int64) error { return nil }

// stubOAuth is a test implementation of OAuthUserIDResolver.
type stubOAuth struct {
	uid int64
	err error
}

func (s *stubOAuth) ResolveOAuthUserID(_ context.Context, _ string) (int64, error) {
	return s.uid, s.err
}

// okHandler is a trivial 200 handler used as the wrapped next handler in middleware tests.
func okHandler(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) }

// runAuth applies mw to okHandler and serves req, returning the recorded response.
func runAuth(mw func(http.Handler) http.Handler, req *http.Request) *httptest.ResponseRecorder {
	rr := httptest.NewRecorder()
	mw(http.HandlerFunc(okHandler)).ServeHTTP(rr, req)
	return rr
}

// --- Auth middleware ---

// TestAuth_ValidJWTInHeader verifies that a valid JWT in the Authorization header is accepted (200).
func TestAuth_ValidJWTInHeader(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer "+makeValidJWT(t, 7, "alice", false))
	if rr := runAuth(Auth(testSecret, "cz_token", nil, nil, testUnauthorized), req); rr.Code != http.StatusOK {
		t.Errorf("want 200, got %d", rr.Code)
	}
}

// TestAuth_ValidJWTInCookie verifies that a valid JWT in the auth cookie is accepted (200).
func TestAuth_ValidJWTInCookie(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.AddCookie(&http.Cookie{Name: "cz_token", Value: makeValidJWT(t, 7, "alice", false)})
	if rr := runAuth(Auth(testSecret, "cz_token", nil, nil, testUnauthorized), req); rr.Code != http.StatusOK {
		t.Errorf("want 200, got %d", rr.Code)
	}
}

// TestAuth_ExpiredJWT_Unauthorized verifies that an expired JWT is rejected with 401.
func TestAuth_ExpiredJWT_Unauthorized(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer "+makeExpiredJWT(t))
	if rr := runAuth(Auth(testSecret, "cz_token", nil, nil, testUnauthorized), req); rr.Code != http.StatusUnauthorized {
		t.Errorf("want 401, got %d", rr.Code)
	}
}

// TestAuth_WrongAlgorithm_Unauthorized verifies that a JWT signed with the "none" algorithm
// is rejected — the middleware only trusts HMAC-SHA256.
func TestAuth_WrongAlgorithm_Unauthorized(t *testing.T) {
	claims := jwt.MapClaims{
		"sub": float64(1), "username": "alice",
		"exp": float64(time.Now().Add(time.Hour).Unix()),
	}
	tok := jwt.NewWithClaims(jwt.SigningMethodNone, claims)
	signed, _ := tok.SignedString(jwt.UnsafeAllowNoneSignatureType)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer "+signed)
	if rr := runAuth(Auth(testSecret, "cz_token", nil, nil, testUnauthorized), req); rr.Code != http.StatusUnauthorized {
		t.Errorf("want 401, got %d", rr.Code)
	}
}

// TestAuth_NoToken_Unauthorized verifies that a request with no token at all is rejected with 401.
func TestAuth_NoToken_Unauthorized(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	if rr := runAuth(Auth(testSecret, "cz_token", nil, nil, testUnauthorized), req); rr.Code != http.StatusUnauthorized {
		t.Errorf("want 401, got %d", rr.Code)
	}
}

// TestAuth_ValidPAT_InjectsUserClaims verifies that a valid czp_ PAT is accepted and
// the resolved user's ID and username are injected into the request context.
func TestAuth_ValidPAT_InjectsUserClaims(t *testing.T) {
	pat := &stubPAT{
		token: &model.AccessToken{ID: 1},
		user:  &model.User{ID: 42, Username: "bob"},
	}
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer czp_validtoken")

	var got Claims
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got, _ = ClaimsFromContext(r.Context())
		w.WriteHeader(http.StatusOK)
	})
	rr := httptest.NewRecorder()
	Auth(testSecret, "cz_token", pat, nil, testUnauthorized)(handler).ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("want 200, got %d", rr.Code)
	}
	if got.UserID != 42 || got.Username != "bob" {
		t.Errorf("claims mismatch: %+v", got)
	}
}

// TestAuth_InvalidPAT_Unauthorized verifies that a czp_ token that fails PAT validation
// does not fall through to grant access — the request is rejected with 401.
func TestAuth_InvalidPAT_Unauthorized(t *testing.T) {
	pat := &stubPAT{err: errors.New("invalid")}
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer czp_bad")
	// PAT fails, falls through to JWT parse which also fails → 401
	if rr := runAuth(Auth(testSecret, "cz_token", pat, nil, testUnauthorized), req); rr.Code != http.StatusUnauthorized {
		t.Errorf("want 401, got %d", rr.Code)
	}
}

// TestAuth_ClaimsInContext verifies that JWT claims (userID, username, superadmin flag)
// are correctly injected into the request context for downstream handlers.
func TestAuth_ClaimsInContext(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer "+makeValidJWT(t, 99, "charlie", true))

	var got Claims
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got, _ = ClaimsFromContext(r.Context())
		w.WriteHeader(http.StatusOK)
	})
	rr := httptest.NewRecorder()
	Auth(testSecret, "cz_token", nil, nil, testUnauthorized)(handler).ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("want 200, got %d", rr.Code)
	}
	if got.UserID != 99 || got.Username != "charlie" || !got.IsSuperadmin {
		t.Errorf("claims mismatch: %+v", got)
	}
}

// --- OptionalAuth middleware ---

// TestOptionalAuth_NoToken_Passes verifies that a request with no token is allowed through
// and no claims are injected into the context.
func TestOptionalAuth_NoToken_Passes(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	var hasClaims bool
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, hasClaims = ClaimsFromContext(r.Context())
		w.WriteHeader(http.StatusOK)
	})
	rr := httptest.NewRecorder()
	OptionalAuth(testSecret, "cz_token", nil, nil)(handler).ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Errorf("want 200, got %d", rr.Code)
	}
	if hasClaims {
		t.Error("expected no claims for unauthenticated request")
	}
}

// TestOptionalAuth_ValidToken_InjectsContext verifies that a valid JWT causes claims to
// be injected into context even in OptionalAuth mode.
func TestOptionalAuth_ValidToken_InjectsContext(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer "+makeValidJWT(t, 5, "dana", false))
	var got Claims
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got, _ = ClaimsFromContext(r.Context())
		w.WriteHeader(http.StatusOK)
	})
	rr := httptest.NewRecorder()
	OptionalAuth(testSecret, "cz_token", nil, nil)(handler).ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Errorf("want 200, got %d", rr.Code)
	}
	if got.UserID != 5 || got.Username != "dana" {
		t.Errorf("claims mismatch: %+v", got)
	}
}

// TestOptionalAuth_ExpiredToken_Passes verifies that an expired token in OptionalAuth mode
// does not block the request — it passes through without claims.
func TestOptionalAuth_ExpiredToken_Passes(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer "+makeExpiredJWT(t))
	var hasClaims bool
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, hasClaims = ClaimsFromContext(r.Context())
		w.WriteHeader(http.StatusOK)
	})
	rr := httptest.NewRecorder()
	OptionalAuth(testSecret, "cz_token", nil, nil)(handler).ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Errorf("want 200, got %d", rr.Code)
	}
	if hasClaims {
		t.Error("expired token should not inject claims in OptionalAuth")
	}
}

// --- RequireSuperadmin middleware ---

// TestRequireSuperadmin_Superadmin_Passes verifies that a user with IsSuperadmin=true
// is allowed through RequireSuperadmin (200).
func TestRequireSuperadmin_Superadmin_Passes(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/admin", nil)
	ctx := context.WithValue(req.Context(), claimsKey, Claims{UserID: 1, IsSuperadmin: true})
	rr := httptest.NewRecorder()
	RequireSuperadmin(testForbidden)(http.HandlerFunc(okHandler)).ServeHTTP(rr, req.WithContext(ctx))
	if rr.Code != http.StatusOK {
		t.Errorf("want 200, got %d", rr.Code)
	}
}

// TestRequireSuperadmin_RegularUser_Forbidden verifies that a non-superadmin user
// is rejected by RequireSuperadmin (403).
func TestRequireSuperadmin_RegularUser_Forbidden(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/admin", nil)
	ctx := context.WithValue(req.Context(), claimsKey, Claims{UserID: 2, IsSuperadmin: false})
	rr := httptest.NewRecorder()
	RequireSuperadmin(testForbidden)(http.HandlerFunc(okHandler)).ServeHTTP(rr, req.WithContext(ctx))
	if rr.Code != http.StatusForbidden {
		t.Errorf("want 403, got %d", rr.Code)
	}
}

// TestRequireSuperadmin_NoClaims_Forbidden verifies that an unauthenticated request
// (no claims in context) is rejected by RequireSuperadmin (403).
func TestRequireSuperadmin_NoClaims_Forbidden(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/admin", nil)
	rr := httptest.NewRecorder()
	RequireSuperadmin(testForbidden)(http.HandlerFunc(okHandler)).ServeHTTP(rr, req)
	if rr.Code != http.StatusForbidden {
		t.Errorf("want 403, got %d", rr.Code)
	}
}

// --- extractToken ---

// TestExtractToken_HeaderWins verifies that the Authorization header takes precedence
// over a cookie when both are present.
func TestExtractToken_HeaderWins(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer headertoken")
	req.AddCookie(&http.Cookie{Name: "cz_token", Value: "cookietoken"})
	if got := extractToken(req, "cz_token"); got != "headertoken" {
		t.Errorf("want header to win, got %q", got)
	}
}

// TestExtractToken_Cookie verifies that the cookie value is returned when there is no header.
func TestExtractToken_Cookie(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.AddCookie(&http.Cookie{Name: "cz_token", Value: "cookietoken"})
	if got := extractToken(req, "cz_token"); got != "cookietoken" {
		t.Errorf("want %q, got %q", "cookietoken", got)
	}
}

// TestExtractToken_None verifies that an empty string is returned when neither header nor cookie is present.
func TestExtractToken_None(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	if got := extractToken(req, "cz_token"); got != "" {
		t.Errorf("want empty, got %q", got)
	}
}
